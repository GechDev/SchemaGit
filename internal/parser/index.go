package parser

import (
	"fmt"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// readPredicate collects the remainder of a partial index predicate, where
// keywords such as NULL are part of the expression rather than a terminator.
func readPredicate(cursor *reader) ([]sqlexpr.Token, error) {
	collected := make([]sqlexpr.Token, 0, 8)
	for !cursor.done() {
		if cursor.peek().IsPunctuation(";") {
			break
		}
		collected = append(collected, cursor.take())
	}
	if len(collected) == 0 {
		return nil, fmt.Errorf("missing index predicate")
	}
	return collected, nil
}

func (b *builder) createIndex(cursor *reader) error {
	unique := cursor.acceptWords("unique")
	cursor.acceptWords("concurrently")
	if err := cursor.expectKeyword("index"); err != nil {
		return err
	}
	cursor.acceptWords("if", "not", "exists")
	name, err := cursor.identifier()
	if err != nil {
		return err
	}
	if err := cursor.expectKeyword("on"); err != nil {
		return err
	}
	namespace, tableName, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	target := b.namespace(namespace)
	table := findTable(target, tableName)
	if table == nil {
		return fmt.Errorf("CREATE INDEX %s references unknown table %s", name, tableID(namespace, tableName))
	}
	method := "btree"
	if cursor.acceptWords("using") {
		method, err = cursor.identifier()
		if err != nil {
			return err
		}
	}
	columns, err := readColumnList(cursor)
	if err != nil {
		return fmt.Errorf("index %s: %w", name, err)
	}
	for _, column := range columns {
		if !columnExists(table, column) {
			return fmt.Errorf("index %s references unknown column %s", name, columnID(namespace, tableName, column))
		}
	}
	if cursor.acceptWords("include") {
		return fmt.Errorf("INCLUDE columns are not supported (index %s)", name)
	}
	predicate := ""
	if cursor.acceptWords("where") {
		tokens, err := readPredicate(cursor)
		if err != nil {
			return err
		}
		canonical, err := sqlexpr.CanonicalTokens(tokens)
		if err != nil {
			return err
		}
		predicate = canonical
	}
	if cursor.acceptWords("with") {
		return fmt.Errorf("index storage parameters are not supported (index %s)", name)
	}
	if !cursor.done() {
		return fmt.Errorf("unsupported CREATE INDEX clause near %q", truncate(cursor.statement, 60))
	}
	for _, existing := range table.Indexes {
		if existing.Name == name {
			return fmt.Errorf("index %s is defined twice", indexID(namespace, tableName, name))
		}
	}
	table.Indexes = append(table.Indexes, schema.Index{
		ID:        indexID(namespace, tableName, name),
		Name:      name,
		Columns:   columns,
		Method:    method,
		Unique:    unique,
		Predicate: predicate,
	})
	return nil
}
