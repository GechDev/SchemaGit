package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/schemagit/schemagit/internal/pgtype"
	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// migrationTableName is the bookkeeping table SchemaGit maintains in each
// database. It is excluded from introspection so it never becomes a managed
// object.
const migrationTableName = "_schemagit_migrations"

// systemFilter excludes PostgreSQL's own catalogs from every query.
const systemFilter = `n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'`

// tableSet holds fully materialised tables keyed by `schema.table`. Tables are
// built completely before being attached to a namespace so no partially filled
// copy can be captured.
type tableSet struct {
	byKey     map[string]*schema.Table
	namespace map[string]string
	order     []string
}

func newTableSet() *tableSet {
	return &tableSet{byKey: map[string]*schema.Table{}, namespace: map[string]string{}}
}

func (t *tableSet) get(schemaName, tableName string) *schema.Table {
	key := qualified(schemaName, tableName)
	table, ok := t.byKey[key]
	if !ok {
		table = &schema.Table{ID: "tbl:" + key, Name: tableName, Columns: []schema.Column{}}
		t.byKey[key] = table
		t.namespace[key] = schemaName
		t.order = append(t.order, key)
	}
	return table
}

func (t *tableSet) lookup(schemaName, tableName string) *schema.Table {
	return t.byKey[qualified(schemaName, tableName)]
}

// attach copies every completed table into its namespace.
func (t *tableSet) attach(namespace func(string) *schema.Namespace) {
	for _, key := range t.order {
		table := t.byKey[key]
		target := namespace(t.namespace[key])
		target.Tables = append(target.Tables, *table)
	}
}

func (a *Adapter) readTables(ctx context.Context, connection *pgx.Conn) (*tableSet, error) {
	tables := newTableSet()
	rows, err := connection.Query(ctx, `
		SELECT n.nspname,
		       c.relname,
		       a.attname,
		       a.attnum,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       a.attidentity <> '',
		       a.attgenerated,
		       pg_catalog.pg_get_expr(d.adbin, d.adrelid)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
		LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE c.relkind IN ('r', 'p')
		  AND c.relname <> '`+migrationTableName+`'
		  AND `+systemFilter+`
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var schemaName, tableName, columnName, displayType, generatedKind string
		var ordinal int32
		var notNull, isIdentity bool
		var expression *string
		if err := rows.Scan(&schemaName, &tableName, &columnName, &ordinal,
			&displayType, &notNull, &isIdentity, &generatedKind, &expression); err != nil {
			return nil, err
		}
		canonicalType, err := pgtype.FromCatalog(displayType)
		if err != nil {
			return nil, fmt.Errorf("table %s.%s column %s: %w", schemaName, tableName, columnName, err)
		}
		column := schema.Column{
			ID:         "col:" + qualified(schemaName, tableName) + "." + columnName,
			Name:       columnName,
			Ordinal:    int(ordinal),
			Type:       canonicalType,
			Nullable:   !notNull,
			IsIdentity: isIdentity,
		}
		if expression != nil {
			if generatedKind == "s" {
				canonical, err := sqlexpr.Canonical(*expression)
				if err != nil {
					return nil, fmt.Errorf("table %s.%s column %s: %w", schemaName, tableName, columnName, err)
				}
				column.Generated = canonical
			} else {
				defaultValue, err := sqlexpr.CanonicalDefault(*expression, canonicalType)
				if err != nil {
					return nil, fmt.Errorf("table %s.%s column %s: %w", schemaName, tableName, columnName, err)
				}
				column.Default = defaultValue
			}
		}
		table := tables.get(schemaName, tableName)
		table.Columns = append(table.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := a.readConstraints(ctx, connection, tables); err != nil {
		return nil, err
	}
	if err := a.readIndexes(ctx, connection, tables); err != nil {
		return nil, err
	}
	return tables, nil
}

func (a *Adapter) readConstraints(ctx context.Context, connection *pgx.Conn, tables *tableSet) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname,
		       c.relname,
		       con.conname,
		       con.contype,
		       (SELECT coalesce(array_agg(a.attname ORDER BY u.ord), ARRAY[]::text[])
		          FROM unnest(con.conkey) WITH ORDINALITY AS u(attnum, ord)
		          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = u.attnum),
		       (SELECT coalesce(array_agg(a.attname ORDER BY u.ord), ARRAY[]::text[])
		          FROM unnest(con.confkey) WITH ORDINALITY AS u(attnum, ord)
		          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = u.attnum),
		       rn.nspname,
		       rc.relname,
		       CASE WHEN con.contype = 'c'
		            THEN pg_catalog.pg_get_expr(con.conbin, con.conrelid) END
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		LEFT JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE con.contype IN ('p', 'u', 'c', 'f')
		  AND c.relname <> '`+migrationTableName+`'
		  AND `+systemFilter+`
		ORDER BY n.nspname, c.relname, con.conname`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schemaName, tableName, constraintName, constraintType string
		var columns, referencedColumns []string
		var referencedSchema, referencedTable *string
		var expression *string
		if err := rows.Scan(&schemaName, &tableName, &constraintName, &constraintType,
			&columns, &referencedColumns, &referencedSchema, &referencedTable, &expression); err != nil {
			return err
		}
		table := tables.lookup(schemaName, tableName)
		if table == nil {
			continue
		}
		constraint := schema.Constraint{
			ID:             "con:" + qualified(schemaName, tableName) + "." + constraintName,
			Name:           constraintName,
			Type:           constraintType,
			Columns:        nonNil(columns),
			ReferencedCols: nonNil(referencedColumns),
		}
		switch constraintType {
		case "p":
			constraint.Type = "pk"
		case "u":
			constraint.Type = "unique"
		case "c":
			constraint.Type = "check"
			if expression != nil {
				canonical, err := sqlexpr.Canonical(*expression)
				if err != nil {
					return err
				}
				constraint.Expression = canonical
			}
		case "f":
			constraint.Type = "fk"
			if referencedTable == nil {
				return fmt.Errorf("foreign key %s has no referenced table", constraint.ID)
			}
			referenceNamespace := ""
			if referencedSchema != nil {
				referenceNamespace = *referencedSchema
			}
			constraint.ReferencedTable = qualified(referenceNamespace, *referencedTable)
		}
		table.Constraints = append(table.Constraints, constraint)
	}
	return rows.Err()
}

func (a *Adapter) readIndexes(ctx context.Context, connection *pgx.Conn, tables *tableSet) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname,
		       tc.relname,
		       ic.relname,
		       idx.indisunique,
		       am.amname,
		       pg_catalog.pg_get_expr(idx.indpred, idx.indrelid),
		       (SELECT array_agg(a.attname ORDER BY u.ord)
		          FROM unnest(idx.indkey) WITH ORDINALITY AS u(attnum, ord)
		          JOIN pg_catalog.pg_attribute a ON a.attrelid = idx.indrelid AND a.attnum = u.attnum
		         WHERE u.attnum > 0)
		FROM pg_catalog.pg_index idx
		JOIN pg_catalog.pg_class ic ON ic.oid = idx.indexrelid
		JOIN pg_catalog.pg_class tc ON tc.oid = idx.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = tc.relnamespace
		JOIN pg_catalog.pg_am am ON am.oid = ic.relam
		WHERE tc.relname <> '`+migrationTableName+`'
		  AND `+systemFilter+`
		  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_constraint con WHERE con.conindid = idx.indexrelid)
		ORDER BY n.nspname, tc.relname, ic.relname`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schemaName, tableName, indexName, method string
		var unique bool
		var predicate *string
		var columns []string
		if err := rows.Scan(&schemaName, &tableName, &indexName, &unique, &method, &predicate, &columns); err != nil {
			return err
		}
		table := tables.lookup(schemaName, tableName)
		if table == nil {
			continue
		}
		index := schema.Index{
			ID:      "idx:" + qualified(schemaName, tableName) + "." + indexName,
			Name:    indexName,
			Columns: nonNil(columns),
			Method:  method,
			Unique:  unique,
		}
		if predicate != nil {
			canonical, err := sqlexpr.Canonical(*predicate)
			if err != nil {
				return err
			}
			index.Predicate = canonical
		}
		table.Indexes = append(table.Indexes, index)
	}
	return rows.Err()
}

func (a *Adapter) readEnums(ctx context.Context, connection *pgx.Conn, namespace func(string) *schema.Namespace) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname, t.typname, e.enumlabel
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		JOIN pg_catalog.pg_enum e ON e.enumtypid = t.oid
		WHERE `+systemFilter+`
		ORDER BY n.nspname, t.typname, e.enumsortorder`)
	if err != nil {
		return err
	}
	defer rows.Close()
	indexes := map[string]*schema.Enum{}
	for rows.Next() {
		var schemaName, typeName, label string
		if err := rows.Scan(&schemaName, &typeName, &label); err != nil {
			return err
		}
		key := qualified(schemaName, typeName)
		enum, ok := indexes[key]
		if !ok {
			enum = &schema.Enum{ID: "enum:" + key, Name: typeName, Values: []string{}}
			namespace(schemaName).Enums = append(namespace(schemaName).Enums, *enum)
			indexes[key] = enum
		}
		enum.Values = append(enum.Values, label)
	}
	return rows.Err()
}

func (a *Adapter) readSequences(ctx context.Context, connection *pgx.Conn, namespace func(string) *schema.Namespace) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname,
		       c.relname,
		       pg_catalog.format_type(s.seqtypid, NULL),
		       s.seqstart,
		       s.seqincrement,
		       s.seqmin,
		       s.seqmax,
		       s.seqcycle
		FROM pg_catalog.pg_sequence s
		JOIN pg_catalog.pg_class c ON c.oid = s.seqrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE `+systemFilter+`
		ORDER BY n.nspname, c.relname`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schemaName, sequenceName, displayType string
		var start, increment, minValue, maxValue int64
		var cycle bool
		if err := rows.Scan(&schemaName, &sequenceName, &displayType,
			&start, &increment, &minValue, &maxValue, &cycle); err != nil {
			return err
		}
		canonicalType, err := pgtype.FromCatalog(displayType)
		if err != nil {
			return err
		}
		namespace(schemaName).Sequences = append(namespace(schemaName).Sequences, schema.Sequence{
			ID:        "seq:" + qualified(schemaName, sequenceName),
			Name:      sequenceName,
			DataType:  canonicalType,
			Start:     start,
			Increment: increment,
			MinValue:  minValue,
			MaxValue:  maxValue,
			Cycle:     cycle,
		})
	}
	return rows.Err()
}

func (a *Adapter) readViews(ctx context.Context, connection *pgx.Conn, namespace func(string) *schema.Namespace) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname, v.relname, pg_catalog.pg_get_viewdef(v.oid, true)
		FROM pg_catalog.pg_class v
		JOIN pg_catalog.pg_namespace n ON n.oid = v.relnamespace
		WHERE v.relkind = 'v' AND `+systemFilter+`
		ORDER BY n.nspname, v.relname`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schemaName, viewName, definition string
		if err := rows.Scan(&schemaName, &viewName, &definition); err != nil {
			return err
		}
		canonical, err := sqlexpr.Canonical(definition)
		if err != nil {
			return fmt.Errorf("view %s.%s: %w", schemaName, viewName, err)
		}
		namespace(schemaName).Views = append(namespace(schemaName).Views, schema.View{
			ID:         "view:" + qualified(schemaName, viewName),
			Name:       viewName,
			Definition: canonical,
			DependsOn:  []string{},
		})
	}
	return rows.Err()
}

func (a *Adapter) readFunctions(ctx context.Context, connection *pgx.Conn, namespace func(string) *schema.Namespace) error {
	rows, err := connection.Query(ctx, `
		SELECT n.nspname,
		       p.proname,
		       pg_catalog.pg_get_function_arguments(p.oid),
		       pg_catalog.pg_get_function_result(p.oid),
		       l.lanname,
		       pg_catalog.pg_get_functiondef(p.oid)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_catalog.pg_language l ON l.oid = p.prolang
		WHERE p.prokind = 'f' AND NOT p.proisagg AND NOT p.proiswindow AND `+systemFilter+`
		ORDER BY n.nspname, p.proname, p.oid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var schemaName, functionName, arguments, returnType, language, definition string
		if err := rows.Scan(&schemaName, &functionName, &arguments, &returnType, &language, &definition); err != nil {
			return err
		}
		id := "fn:" + qualified(schemaName, functionName)
		if seen[id] {
			return fmt.Errorf("function %s is overloaded, which SchemaGit cannot represent", id)
		}
		seen[id] = true
		canonicalReturn, err := pgtype.FromCatalog(returnType)
		if err != nil {
			canonicalReturn = returnType
		}
		namespace(schemaName).Functions = append(namespace(schemaName).Functions, schema.Function{
			ID:         id,
			Name:       functionName,
			Arguments:  splitArguments(arguments),
			ReturnType: canonicalReturn,
			Language:   language,
			Definition: definition,
		})
	}
	return rows.Err()
}

func splitArguments(arguments string) []string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return []string{}
	}
	parts := []string{}
	depth := 0
	current := strings.Builder{}
	for _, character := range trimmed {
		switch character {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		}
		if character == ',' && depth == 0 {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(character)
	}
	if strings.TrimSpace(current.String()) != "" {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

// resolveViewDependencies records the table and view ids each view reads from so
// that dangling view references can be detected before a migration is planned.
func resolveViewDependencies(result *schema.Schema) error {
	known := map[string]map[string]bool{}
	for _, namespace := range result.Namespaces {
		names := map[string]bool{}
		for _, table := range namespace.Tables {
			names[table.ID] = true
		}
		for _, view := range namespace.Views {
			names[view.ID] = true
		}
		known[namespace.ID] = names
	}
	for namespaceIndex := range result.Namespaces {
		namespace := &result.Namespaces[namespaceIndex]
		for viewIndex := range namespace.Views {
			view := &namespace.Views[viewIndex]
			names, err := sqlexpr.Relations(view.Definition)
			if err != nil {
				return fmt.Errorf("view %s: %w", view.ID, err)
			}
			dependencies := []string{}
			for _, name := range names {
				qualifiedName := qualified(namespace.Name, name)
				for _, candidate := range []string{
					"tbl:" + qualifiedName, "view:" + qualifiedName,
					"tbl:" + name, "view:" + name,
				} {
					if known[namespace.ID][candidate] {
						dependencies = append(dependencies, candidate)
						break
					}
				}
			}
			view.DependsOn = uniqueSortedStrings(dependencies)
		}
	}
	return nil
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
}

func sortNamespaces(result *schema.Schema) {
	sort.Slice(result.Namespaces, func(i, j int) bool { return result.Namespaces[i].ID < result.Namespaces[j].ID })
}

func sortNamespaceContents(namespace *schema.Namespace) {
	sort.Slice(namespace.Tables, func(i, j int) bool { return namespace.Tables[i].ID < namespace.Tables[j].ID })
	sort.Slice(namespace.Views, func(i, j int) bool { return namespace.Views[i].ID < namespace.Views[j].ID })
	sort.Slice(namespace.Enums, func(i, j int) bool { return namespace.Enums[i].ID < namespace.Enums[j].ID })
	sort.Slice(namespace.Sequences, func(i, j int) bool {
		return namespace.Sequences[i].ID < namespace.Sequences[j].ID
	})
	sort.Slice(namespace.Functions, func(i, j int) bool {
		return namespace.Functions[i].ID < namespace.Functions[j].ID
	})
	for index := range namespace.Tables {
		table := &namespace.Tables[index]
		sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].ID < table.Columns[j].ID })
		for columnIndex := range table.Columns {
			table.Columns[columnIndex].Ordinal = columnIndex + 1
		}
		sort.Slice(table.Constraints, func(i, j int) bool {
			return table.Constraints[i].ID < table.Constraints[j].ID
		})
		sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].ID < table.Indexes[j].ID })
	}
}

func qualified(namespace, name string) string {
	if namespace == "" {
		namespace = "public"
	}
	return namespace + "." + name
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
