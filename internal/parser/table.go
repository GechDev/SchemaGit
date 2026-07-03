package parser

import (
	"fmt"
	"strings"

	"github.com/schemagit/schemagit/internal/pgtype"
	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

var typeContinuationWords = map[string]bool{
	"varying": true, "precision": true, "without": true, "with": true, "time": true, "zone": true,
}

var reservedColumnConstraints = map[string]bool{
	"not": true, "null": true, "default": true, "constraint": true, "primary": true,
	"unique": true, "check": true, "references": true, "generated": true, "collate": true,
}

func (b *builder) createTable(cursor *reader) error {
	if err := cursor.expectKeyword("table"); err != nil {
		return err
	}
	namespace, name, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	target := b.namespace(namespace)
	if findTable(target, name) != nil {
		return fmt.Errorf("table %s is defined twice", tableID(namespace, name))
	}
	table := schema.Table{ID: tableID(namespace, name), Name: name, Columns: []schema.Column{}}
	if err := cursor.expectPunctuation("("); err != nil {
		return err
	}
	for {
		if err := b.tableElement(cursor, namespace, name, &table); err != nil {
			return err
		}
		if cursor.accept(sqlexpr.KindPunctuation, ",") {
			continue
		}
		break
	}
	if err := cursor.expectPunctuation(")"); err != nil {
		return err
	}
	if err := validateTable(&table); err != nil {
		return err
	}
	if !cursor.done() {
		return fmt.Errorf("unsupported CREATE TABLE option %q for %s", truncate(cursor.statement, 80), table.ID)
	}
	sortColumns(&table)
	target.Tables = append(target.Tables, table)
	return nil
}

// validateTable rejects definitions PostgreSQL itself would reject, so a schema
// can never be committed that could not be created on a fresh database.
func validateTable(table *schema.Table) error {
	if len(table.Columns) == 0 {
		return fmt.Errorf("CREATE TABLE %s defines no columns", table.ID)
	}
	primaryKeys := 0
	seen := map[string]bool{}
	for _, constraint := range table.Constraints {
		if seen[constraint.ID] {
			return fmt.Errorf("constraint %s is defined twice", constraint.ID)
		}
		seen[constraint.ID] = true
		if constraint.Type == "pk" {
			primaryKeys++
		}
		for _, column := range constraint.Columns {
			if !columnExists(table, column) {
				return fmt.Errorf("constraint %s references unknown column %s", constraint.ID, column)
			}
		}
	}
	if primaryKeys > 1 {
		return fmt.Errorf("table %s defines %d primary keys", table.ID, primaryKeys)
	}
	return nil
}

// tableElement parses either a column definition or a table level constraint.
func (b *builder) tableElement(cursor *reader, namespace, table string, target *schema.Table) error {
	if cursor.done() {
		return fmt.Errorf("unexpected end of CREATE TABLE %s", target.ID)
	}
	constraintName := ""
	if cursor.peek().IsKeyword("constraint") {
		cursor.position++
		name, err := cursor.identifier()
		if err != nil {
			return err
		}
		constraintName = name
	}
	switch {
	case cursor.peek().IsKeyword("like"):
		return fmt.Errorf("CREATE TABLE LIKE is not supported (%s)", target.ID)
	case cursor.peek().IsKeyword("exclude"):
		return fmt.Errorf("EXCLUDE constraints are not supported (%s)", target.ID)
	case cursor.peek().IsKeyword("foreign"):
		cursor.position++
		if err := cursor.expectKeyword("key"); err != nil {
			return err
		}
		return b.tableForeignKey(cursor, namespace, table, constraintName, target)
	case cursor.peek().IsKeyword("primary"),
		cursor.peek().IsKeyword("unique"),
		cursor.peek().IsKeyword("check"):
		return b.tableKeyConstraint(cursor, namespace, table, constraintName, target)
	}
	return b.tableColumn(cursor, namespace, table, target)
}

func (b *builder) tableColumn(cursor *reader, namespace, table string, target *schema.Table) error {
	name, err := cursor.identifier()
	if err != nil {
		return err
	}
	if columnExists(target, name) {
		return fmt.Errorf("column %s is defined twice", columnID(namespace, table, name))
	}
	columnType, err := readType(cursor)
	if err != nil {
		return fmt.Errorf("column %s: %w", columnID(namespace, table, name), err)
	}
	column := schema.Column{
		ID:       columnID(namespace, table, name),
		Name:     name,
		Type:     columnType,
		Nullable: true,
	}
	for !cursor.done() {
		peek := cursor.peek()
		if peek.IsPunctuation(",") || peek.IsPunctuation(")") {
			break
		}
		if err := b.columnConstraint(cursor, namespace, table, &column, target); err != nil {
			return fmt.Errorf("column %s: %w", column.ID, err)
		}
	}
	target.Columns = append(target.Columns, column)
	return nil
}

// readType reads a PostgreSQL type name including multi word types such as
// `double precision`, `timestamp with time zone` and `character varying(20)`.
func readType(cursor *reader) (string, error) {
	name, err := cursor.identifier()
	if err != nil {
		return "", err
	}
	if cursor.accept(sqlexpr.KindPunctuation, ".") {
		local, localErr := cursor.identifier()
		if localErr != nil {
			return "", localErr
		}
		name = local
	}
	words := []string{name}
	for cursor.peek().Kind == sqlexpr.KindIdentifier && typeContinuationWords[cursor.peek().Value] {
		words = append(words, cursor.take().Value)
	}
	modifiers := ""
	if cursor.peek().IsPunctuation("(") {
		start := cursor.position
		if _, balanceErr := cursor.skipBalancedParens(); balanceErr != nil {
			return "", balanceErr
		}
		modifiers = sqlexpr.Render(cursor.tokens[start:cursor.position])
	}
	for cursor.peek().IsPunctuation("[") && cursor.peekAt(1).IsPunctuation("]") {
		cursor.position += 2
		modifiers += "[]"
	}
	canonical, err := pgtype.FromDDL(strings.Join(words, " ") + modifiers)
	if err != nil {
		return "", err
	}
	switch canonical {
	case "serial", "bigserial", "smallserial", "serial4", "serial8", "serial2":
		return "", fmt.Errorf("pseudo-types are not supported: declare CREATE SEQUENCE explicitly")
	}
	return canonical, nil
}

func (b *builder) columnConstraint(cursor *reader, namespace, table string, column *schema.Column, target *schema.Table) error {
	name := ""
	if cursor.peek().IsKeyword("constraint") {
		cursor.position++
		explicit, err := cursor.identifier()
		if err != nil {
			return err
		}
		name = explicit
	}
	switch {
	case cursor.acceptWords("not", "null"):
		column.Nullable = false
		return nil
	case cursor.acceptWords("not", "deferrable"):
		return nil
	case cursor.acceptWords("not", "valid"):
		return nil
	case cursor.acceptWords("null"):
		column.Nullable = true
		return nil
	case cursor.acceptWords("deferrable"), cursor.acceptWords("initially"):
		return nil
	case cursor.acceptWords("default"):
		tokens, err := readExpression(cursor)
		if err != nil {
			return err
		}
		canonical, err := sqlexpr.CanonicalTokens(tokens)
		if err != nil {
			return err
		}
		defaultValue, err := sqlexpr.CanonicalDefault(canonical, column.Type)
		if err != nil {
			return err
		}
		column.Default = defaultValue
		return nil
	case cursor.peek().IsKeyword("collate"):
		return fmt.Errorf("COLLATE is not supported")
	case cursor.acceptWords("generated"):
		return readGenerated(cursor, column)
	case cursor.acceptWords("primary", "key"):
		target.Constraints = append(target.Constraints,
			primaryKey(namespace, table, name, []string{column.Name}))
		return nil
	case cursor.acceptWords("nulls", "not", "distinct"):
		return fmt.Errorf("NULLS NOT DISTINCT is not supported")
	case cursor.acceptWords("unique"):
		if err := skipOptionalNullsDistinct(cursor); err != nil {
			return err
		}
		target.Constraints = append(target.Constraints,
			uniqueConstraint(namespace, table, name, []string{column.Name}))
		return nil
	case cursor.acceptWords("check"):
		expression, err := cursor.skipBalancedParens()
		if err != nil {
			return err
		}
		canonical, err := sqlexpr.CanonicalTokens(expression)
		if err != nil {
			return err
		}
		target.Constraints = append(target.Constraints,
			b.checkConstraint(namespace, table, name, canonical, target))
		return nil
	case cursor.peek().IsKeyword("references"):
		return b.foreignKey(cursor, namespace, table, name, []string{column.Name}, target)
	}
	return fmt.Errorf("unsupported column constraint near %q", truncate(cursor.statement, 60))
}

func skipOptionalNullsDistinct(cursor *reader) error {
	if cursor.acceptWords("nulls", "not", "distinct") {
		return fmt.Errorf("NULLS NOT DISTINCT is not supported")
	}
	return nil
}

func readGenerated(cursor *reader, column *schema.Column) error {
	if cursor.acceptWords("always", "as", "identity") || cursor.acceptWords("by", "default", "as", "identity") {
		column.IsIdentity = true
		if cursor.peek().IsPunctuation("(") {
			if _, err := cursor.skipBalancedParens(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := cursor.expectKeyword("always"); err != nil {
		return err
	}
	if err := cursor.expectKeyword("as"); err != nil {
		return err
	}
	expression, err := cursor.skipBalancedParens()
	if err != nil {
		return err
	}
	if err := cursor.expectKeyword("stored"); err != nil {
		return err
	}
	canonical, err := sqlexpr.CanonicalTokens(expression)
	if err != nil {
		return err
	}
	column.Generated = canonical
	return nil
}

// readExpression collects tokens up to the next column constraint or the end of
// the column definition, keeping parenthesised groups intact.
func readExpression(cursor *reader) ([]sqlexpr.Token, error) {
	collected := make([]sqlexpr.Token, 0, 4)
	depth := 0
	for !cursor.done() {
		token := cursor.peek()
		if depth == 0 {
			if token.IsPunctuation(",") || token.IsPunctuation(")") || token.IsPunctuation(";") {
				break
			}
			if token.Kind == sqlexpr.KindIdentifier && reservedColumnConstraints[token.Value] {
				break
			}
		}
		switch {
		case token.IsPunctuation("("):
			depth++
		case token.IsPunctuation(")"):
			depth--
		}
		collected = append(collected, cursor.take())
	}
	if len(collected) == 0 {
		return nil, fmt.Errorf("missing expression %s", cursor.offset())
	}
	return collected, nil
}

// foreignKey parses a REFERENCES clause. A nil columns slice means the
// constraint was declared at column level, so the single column is used.
func (b *builder) foreignKey(cursor *reader, namespace, table, name string, columns []string, target *schema.Table) error {
	if err := cursor.expectKeyword("references"); err != nil {
		return err
	}
	refNamespace, refTable, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	referenced := []string{}
	if cursor.peek().IsPunctuation("(") {
		names, err := readColumnList(cursor)
		if err != nil {
			return err
		}
		referenced = names
		if len(referenced) != len(columns) {
			return fmt.Errorf("FOREIGN KEY on %s maps %d columns onto %d referenced columns",
				target.ID, len(columns), len(referenced))
		}
	}
	if len(columns) == 0 {
		return fmt.Errorf("FOREIGN KEY on %s needs an explicit column list", target.ID)
	}
	if err := consumeForeignKeyActions(cursor); err != nil {
		return err
	}
	constraintName := foreignKeyName(name, table, columns)
	target.Constraints = append(target.Constraints, schema.Constraint{
		ID:              constraintID(namespace, table, constraintName),
		Name:            constraintName,
		Type:            "fk",
		Columns:         columns,
		ReferencedTable: qualified(refNamespace, refTable),
		ReferencedCols:  referenced,
	})
	return nil
}

func (b *builder) tableForeignKey(cursor *reader, namespace, table, name string, target *schema.Table) error {
	columns, err := readColumnList(cursor)
	if err != nil {
		return err
	}
	return b.foreignKey(cursor, namespace, table, name, columns, target)
}

func (b *builder) tableKeyConstraint(cursor *reader, namespace, table, name string, target *schema.Table) error {
	switch {
	case cursor.acceptWords("primary", "key"):
		columns, err := readColumnList(cursor)
		if err != nil {
			return err
		}
		if err := rejectIndexOptions(cursor, target.ID); err != nil {
			return err
		}
		target.Constraints = append(target.Constraints, primaryKey(namespace, table, name, columns))
		return nil
	case cursor.acceptWords("unique"):
		columns, err := readColumnList(cursor)
		if err != nil {
			return err
		}
		if err := rejectIndexOptions(cursor, target.ID); err != nil {
			return err
		}
		target.Constraints = append(target.Constraints, uniqueConstraint(namespace, table, name, columns))
		return nil
	case cursor.acceptWords("check"):
		expression, err := cursor.skipBalancedParens()
		if err != nil {
			return err
		}
		canonical, err := sqlexpr.CanonicalTokens(expression)
		if err != nil {
			return err
		}
		if !cursor.acceptWords("no", "inherit") {
			if err := consumeConstraintState(cursor); err != nil {
				return err
			}
		}
		target.Constraints = append(target.Constraints,
			b.checkConstraint(namespace, table, name, canonical, target))
		return nil
	}
	return fmt.Errorf("unsupported table constraint near %q", truncate(cursor.statement, 60))
}

func readColumnList(cursor *reader) ([]string, error) {
	start := cursor.position
	if _, err := cursor.skipBalancedParens(); err != nil {
		return nil, err
	}
	names := []string{}
	for index := start + 1; index < cursor.position-1; index++ {
		token := cursor.tokens[index]
		if token.Kind != sqlexpr.KindIdentifier && token.Kind != sqlexpr.KindQuotedIdentifier {
			return nil, fmt.Errorf("only plain column names are supported in constraint column lists %s", cursor.offset())
		}
		names = append(names, token.Value)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("empty column list %s", cursor.offset())
	}
	return names, nil
}

func rejectIndexOptions(cursor *reader, table string) error {
	for {
		switch {
		case cursor.acceptWords("include"),
			cursor.acceptWords("with"),
			cursor.acceptWords("using", "index"),
			cursor.acceptWords("deferrable"),
			cursor.acceptWords("not", "deferrable"),
			cursor.acceptWords("initially"):
			return fmt.Errorf("constraint options are not supported (%s)", table)
		case cursor.peek().IsKeyword("deferrable"),
			cursor.peek().IsKeyword("initially"):
			return consumeConstraintState(cursor)
		default:
			return nil
		}
	}
}

func consumeConstraintState(cursor *reader) error {
	for {
		switch {
		case cursor.acceptWords("deferrable"),
			cursor.acceptWords("not", "deferrable"),
			cursor.acceptWords("initially", "deferred"),
			cursor.acceptWords("initially", "immediate"),
			cursor.acceptWords("no", "inherit"):
			continue
		}
		return nil
	}
}

func consumeForeignKeyActions(cursor *reader) error {
	for {
		switch {
		case cursor.acceptWords("match", "full"), cursor.acceptWords("match", "simple"),
			cursor.acceptWords("match", "partial"):
			continue
		case cursor.acceptWords("on", "delete"), cursor.acceptWords("on", "update"):
			for {
				switch {
				case cursor.acceptWords("no", "action"),
					cursor.acceptWords("restrict"),
					cursor.acceptWords("cascade"),
					cursor.acceptWords("set", "null"),
					cursor.acceptWords("set", "default"):
					break
				default:
					return fmt.Errorf("unsupported referential action %s", cursor.offset())
				}
				break
			}
			continue
		case cursor.acceptWords("deferrable"),
			cursor.acceptWords("not", "deferrable"),
			cursor.acceptWords("initially", "deferred"),
			cursor.acceptWords("initially", "immediate"),
			cursor.acceptWords("not", "valid"),
			cursor.acceptWords("not", "enforced"),
			cursor.acceptWords("enforced"):
			continue
		}
		return nil
	}
}

func primaryKey(namespace, table, name string, columns []string) schema.Constraint {
	constraintName := name
	if constraintName == "" {
		constraintName = table + "_pkey"
	}
	return schema.Constraint{
		ID:      constraintID(namespace, table, constraintName),
		Name:    constraintName,
		Type:    "pk",
		Columns: columns,
	}
}

func uniqueConstraint(namespace, table, name string, columns []string) schema.Constraint {
	constraintName := name
	if constraintName == "" {
		suffix := "key"
		if len(columns) > 0 {
			suffix = strings.Join(columns, "_") + "_key"
		}
		constraintName = table + "_" + suffix
	}
	return schema.Constraint{
		ID:      constraintID(namespace, table, constraintName),
		Name:    constraintName,
		Type:    "unique",
		Columns: columns,
	}
}

// checkConstraint builds a CHECK constraint, generating the same
// `{table}_check`, `{table}_check1`, ... names PostgreSQL assigns to unnamed
// checks so that parsed names match the catalog.
func (b *builder) checkConstraint(namespace, table, name, expression string, target *schema.Table) schema.Constraint {
	if name == "" {
		name = b.nextCheckName(namespace, table, target)
	}
	return schema.Constraint{
		ID:         constraintID(namespace, table, name),
		Name:       name,
		Type:       "check",
		Columns:    []string{},
		Expression: expression,
	}
}

func (b *builder) nextCheckName(namespace, table string, target *schema.Table) string {
	identifier := tableID(namespace, table)
	b.checkCounts[identifier]++
	for {
		candidate := table + "_check"
		if count := b.checkCounts[identifier]; count > 1 {
			candidate = fmt.Sprintf("%s_check%d", table, count-1)
		}
		taken := false
		for _, existing := range target.Constraints {
			if existing.Name == candidate {
				taken = true
				break
			}
		}
		if !taken {
			return candidate
		}
		b.checkCounts[identifier]++
	}
}

func foreignKeyName(name, table string, columns []string) string {
	if name != "" {
		return name
	}
	if len(columns) == 0 {
		return table + "_fkey"
	}
	return table + "_" + strings.Join(columns, "_") + "_fkey"
}

func tableNamespace(tableID string) string {
	_, rest, found := strings.Cut(tableID, ":")
	if !found {
		return "public"
	}
	index := strings.LastIndex(rest, ".")
	if index < 0 {
		return "public"
	}
	return rest[:index]
}
