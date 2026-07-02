package parser

import (
	"fmt"
	"sort"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v5"
	"github.com/schemagit/schemagit/internal/schema"
)

// ParseDDL parses supported PostgreSQL DDL into the canonical Schema Model.
func ParseDDL(sql string) (*schema.Schema, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DDL: %w", err)
	}
	result := &schema.Schema{}
	namespaces := make(map[string]*schema.Namespace)
	for _, rawStatement := range tree.GetStmts() {
		statement := rawStatement.GetStmt()
		switch {
		case statement.GetCreateStmt() != nil:
			if err := parseTable(namespaces, statement.GetCreateStmt()); err != nil {
				return nil, err
			}
		case statement.GetIndexStmt() != nil:
			if err := parseIndex(namespaces, statement.GetIndexStmt()); err != nil {
				return nil, err
			}
		case statement.GetViewStmt() != nil:
			if err := parseView(namespaces, statement.GetViewStmt()); err != nil {
				return nil, err
			}
		case statement.GetCreateEnumStmt() != nil:
			if err := parseEnum(namespaces, statement.GetCreateEnumStmt()); err != nil {
				return nil, err
			}
		case statement.GetCreateSeqStmt() != nil:
			if err := parseSequence(namespaces, statement.GetCreateSeqStmt()); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported PostgreSQL statement %T", statement.GetNode())
		}
	}
	for _, namespace := range namespaces {
		for tableIndex := range namespace.Tables {
			table := &namespace.Tables[tableIndex]
			sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].ID < table.Columns[j].ID })
			sort.Slice(table.Constraints, func(i, j int) bool { return table.Constraints[i].ID < table.Constraints[j].ID })
			sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].ID < table.Indexes[j].ID })
		}
		sort.Slice(namespace.Tables, func(i, j int) bool { return namespace.Tables[i].ID < namespace.Tables[j].ID })
		sort.Slice(namespace.Views, func(i, j int) bool { return namespace.Views[i].ID < namespace.Views[j].ID })
		sort.Slice(namespace.Enums, func(i, j int) bool { return namespace.Enums[i].ID < namespace.Enums[j].ID })
		sort.Slice(namespace.Sequences, func(i, j int) bool { return namespace.Sequences[i].ID < namespace.Sequences[j].ID })
	}
	for _, namespace := range namespaces {
		result.Namespaces = append(result.Namespaces, *namespace)
	}
	sort.Slice(result.Namespaces, func(i, j int) bool { return result.Namespaces[i].ID < result.Namespaces[j].ID })
	if err := schema.Validate(result); err != nil {
		return nil, fmt.Errorf("parsed schema is invalid: %w", err)
	}
	return result, nil
}

func parseTable(namespaces map[string]*schema.Namespace, statement *pg_query.CreateStmt) error {
	if statement.GetRelation() == nil || statement.GetRelation().GetRelname() == "" {
		return fmt.Errorf("CREATE TABLE has no relation name")
	}
	nameSpace, tableName := relationParts(statement.GetRelation())
	namespace := getNamespace(namespaces, nameSpace)
	table := schema.Table{
		ID: "tbl:" + nameSpace + "." + tableName, Name: tableName,
		Columns: []schema.Column{},
	}
	for _, element := range statement.GetTableElts() {
		if columnDefinition := element.GetColumnDef(); columnDefinition != nil {
			columnType, err := formatType(columnDefinition.GetTypeName())
			if err != nil {
				return fmt.Errorf("column %s.%s.%s: %w", nameSpace, tableName, columnDefinition.GetColname(), err)
			}
			columnName := columnDefinition.GetColname()
			column := schema.Column{
				ID:   "col:" + nameSpace + "." + tableName + "." + columnName,
				Name: columnName, Ordinal: len(table.Columns) + 1, Type: columnType,
				Nullable: !columnDefinition.GetIsNotNull(), IsIdentity: columnDefinition.GetIdentity() != "",
				Generated: columnDefinition.GetGenerated(),
			}
			if columnDefinition.GetRawDefault() != nil {
				column.Default, err = deparseExpression(columnDefinition.GetRawDefault())
				if err != nil {
					return fmt.Errorf("column default %s.%s.%s: %w", nameSpace, tableName, columnName, err)
				}
			}
			table.Columns = append(table.Columns, column)
			for _, constraintNode := range columnDefinition.GetConstraints() {
				constraint := constraintNode.GetConstraint()
				if constraint == nil {
					return fmt.Errorf("unsupported column constraint node %T", constraintNode.GetNode())
				}
				parsed, err := parseConstraint(constraint, nameSpace, tableName, columnName)
				if err != nil {
					return err
				}
				table.Constraints = append(table.Constraints, parsed)
			}
			continue
		}
		constraint := element.GetConstraint()
		if constraint == nil {
			return fmt.Errorf("unsupported CREATE TABLE element %T", element.GetNode())
		}
		parsed, err := parseConstraint(constraint, nameSpace, tableName, "")
		if err != nil {
			return err
		}
		table.Constraints = append(table.Constraints, parsed)
	}
	if len(table.Columns) == 0 {
		return fmt.Errorf("CREATE TABLE %s.%s has no supported columns", nameSpace, tableName)
	}
	namespace.Tables = append(namespace.Tables, table)
	return nil
}

func parseConstraint(value *pg_query.Constraint, namespace, table, column string) (schema.Constraint, error) {
	typeName := ""
	switch value.GetContype().String() {
	case "CONSTR_PRIMARY":
		typeName = "pk"
	case "CONSTR_UNIQUE":
		typeName = "unique"
	case "CONSTR_CHECK":
		typeName = "check"
	case "CONSTR_FOREIGN":
		typeName = "fk"
	default:
		return schema.Constraint{}, fmt.Errorf("unsupported constraint type %s", value.GetContype())
	}
	columns, err := nodeStrings(value.GetKeys())
	if err != nil {
		return schema.Constraint{}, err
	}
	if len(columns) == 0 && column != "" {
		columns = []string{column}
	}
	name := value.GetConname()
	if name == "" {
		name = table + "_" + strings.Join(columns, "_") + "_" + typeName
	}
	parsed := schema.Constraint{
		ID:   "con:" + namespace + "." + table + "." + name,
		Name: name, Type: typeName, Columns: columns,
	}
	if value.GetRawExpr() != nil {
		parsed.Expression, err = deparseExpression(value.GetRawExpr())
		if err != nil {
			return schema.Constraint{}, fmt.Errorf("constraint %s expression: %w", name, err)
		}
	}
	if value.GetPktable() != nil {
		refNamespace, refTable := relationParts(value.GetPktable())
		parsed.ReferencedTable = refNamespace + "." + refTable
	}
	parsed.ReferencedCols, err = nodeStrings(value.GetPkAttrs())
	if err != nil {
		return schema.Constraint{}, err
	}
	return parsed, nil
}

func parseIndex(namespaces map[string]*schema.Namespace, statement *pg_query.IndexStmt) error {
	if statement.GetRelation() == nil || statement.GetIdxname() == "" {
		return fmt.Errorf("CREATE INDEX requires an index and table name")
	}
	namespaceName, tableName := relationParts(statement.GetRelation())
	namespace := getNamespace(namespaces, namespaceName)
	table := findTable(namespace, tableName)
	if table == nil {
		return fmt.Errorf("CREATE INDEX %s references unknown table %s.%s", statement.GetIdxname(), namespaceName, tableName)
	}
	columns := make([]string, 0, len(statement.GetIndexParams()))
	for _, parameter := range statement.GetIndexParams() {
		indexElement := parameter.GetIndexElem()
		if indexElement == nil || indexElement.GetName() == "" {
			return fmt.Errorf("index %s uses an unsupported expression", statement.GetIdxname())
		}
		columns = append(columns, indexElement.GetName())
	}
	index := schema.Index{
		ID:   "idx:" + namespaceName + "." + tableName + "." + statement.GetIdxname(),
		Name: statement.GetIdxname(), Columns: columns, Method: statement.GetAccessMethod(), Unique: statement.GetUnique(),
	}
	if index.Method == "" {
		index.Method = "btree"
	}
	if statement.GetWhereClause() != nil {
		predicate, err := deparseExpression(statement.GetWhereClause())
		if err != nil {
			return fmt.Errorf("index %s predicate: %w", statement.GetIdxname(), err)
		}
		index.Predicate = predicate
	}
	table.Indexes = append(table.Indexes, index)
	return nil
}

func parseView(namespaces map[string]*schema.Namespace, statement *pg_query.ViewStmt) error {
	if statement.GetView() == nil || statement.GetQuery() == nil {
		return fmt.Errorf("CREATE VIEW is missing its name or query")
	}
	namespaceName, viewName := relationParts(statement.GetView())
	definition, err := deparseStatement(statement.GetQuery())
	if err != nil {
		return fmt.Errorf("view %s.%s: %w", namespaceName, viewName, err)
	}
	view := schema.View{
		ID: "view:" + namespaceName + "." + viewName, Name: viewName, Definition: definition,
	}
	getNamespace(namespaces, namespaceName).Views = append(getNamespace(namespaces, namespaceName).Views, view)
	return nil
}

func parseEnum(namespaces map[string]*schema.Namespace, statement *pg_query.CreateEnumStmt) error {
	parts, err := nodeStrings(statement.GetTypeName())
	if err != nil || len(parts) == 0 {
		return fmt.Errorf("CREATE TYPE AS ENUM has an invalid type name: %w", err)
	}
	nameSpace, typeName := splitQualifiedName(parts)
	values, err := nodeStrings(statement.GetVals())
	if err != nil {
		return fmt.Errorf("enum %s.%s: %w", nameSpace, typeName, err)
	}
	enum := schema.Enum{ID: "enum:" + nameSpace + "." + typeName, Name: typeName, Values: values}
	namespace := getNamespace(namespaces, nameSpace)
	namespace.Enums = append(namespace.Enums, enum)
	return nil
}

func parseSequence(namespaces map[string]*schema.Namespace, statement *pg_query.CreateSeqStmt) error {
	if statement.GetSequence() == nil || statement.GetSequence().GetRelname() == "" {
		return fmt.Errorf("CREATE SEQUENCE has no relation name")
	}
	namespaceName, sequenceName := relationParts(statement.GetSequence())
	sequence := schema.Sequence{
		ID: "seq:" + namespaceName + "." + sequenceName, Name: sequenceName,
		DataType: "bigint", Start: 1, Increment: 1, MinValue: 1, MaxValue: 9223372036854775807,
	}
	getNamespace(namespaces, namespaceName).Sequences = append(getNamespace(namespaces, namespaceName).Sequences, sequence)
	return nil
}

func getNamespace(namespaces map[string]*schema.Namespace, name string) *schema.Namespace {
	if existing := namespaces[name]; existing != nil {
		return existing
	}
	namespace := &schema.Namespace{ID: "ns:" + name, Name: name}
	namespaces[name] = namespace
	return namespace
}

func relationParts(relation *pg_query.RangeVar) (string, string) {
	namespace := relation.GetSchemaname()
	if namespace == "" {
		namespace = "public"
	}
	return namespace, relation.GetRelname()
}

func splitQualifiedName(parts []string) (string, string) {
	if len(parts) == 1 {
		return "public", parts[0]
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

func findTable(namespace *schema.Namespace, name string) *schema.Table {
	for index := range namespace.Tables {
		if namespace.Tables[index].Name == name {
			return &namespace.Tables[index]
		}
	}
	return nil
}

func nodeStrings(nodes []*pg_query.Node) ([]string, error) {
	values := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if value := node.GetString_(); value != nil {
			values = append(values, value.GetSval())
			continue
		}
		return nil, fmt.Errorf("expected identifier node, got %T", node.GetNode())
	}
	return values, nil
}

func formatType(value *pg_query.TypeName) (string, error) {
	if value == nil {
		return "", fmt.Errorf("missing PostgreSQL type")
	}
	parts, err := nodeStrings(value.GetNames())
	if err != nil || len(parts) == 0 {
		return "", fmt.Errorf("invalid PostgreSQL type name: %w", err)
	}
	name := parts[len(parts)-1]
	if len(value.GetTypmods()) > 0 {
		modifiers := make([]string, 0, len(value.GetTypmods()))
		for _, modifier := range value.GetTypmods() {
			constant := modifier.GetAConst()
			if constant == nil || constant.GetIval() == nil {
				return "", fmt.Errorf("unsupported type modifier")
			}
			modifiers = append(modifiers, fmt.Sprint(constant.GetIval().GetIval()))
		}
		name += "(" + strings.Join(modifiers, ",") + ")"
	}
	if len(value.GetArrayBounds()) > 0 {
		name += "[]"
	}
	return name, nil
}

func deparseExpression(expression *pg_query.Node) (string, error) {
	selectStatement := &pg_query.SelectStmt{TargetList: []*pg_query.Node{{
		Node: &pg_query.Node_ResTarget{ResTarget: &pg_query.ResTarget{Val: expression}},
	}}}
	return deparseStatement(&pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: selectStatement}})
}

func deparseStatement(statement *pg_query.Node) (string, error) {
	output, err := pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: statement}}})
	if err != nil {
		return "", fmt.Errorf("deparse PostgreSQL expression: %w", err)
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(output, "SELECT "), ";")), nil
}
