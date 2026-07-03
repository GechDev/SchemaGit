// Package parser turns PostgreSQL DDL into the canonical SchemaGit schema
// model. The parser is intentionally fail-closed: any construct that cannot be
// represented exactly in the model - and therefore cannot be reproduced by a
// regenerated migration - is rejected instead of being silently dropped.
package parser

import (
	"fmt"
	"sort"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// ParseDDL parses supported PostgreSQL DDL into the canonical schema model.
//
// Supported statements are CREATE TABLE (with columns and PRIMARY KEY, FOREIGN
// KEY, UNIQUE and CHECK constraints), CREATE INDEX, CREATE VIEW, CREATE TYPE ...
// AS ENUM and CREATE SEQUENCE.
func ParseDDL(sql string) (*schema.Schema, error) {
	statements, err := sqlexpr.Statements(sql)
	if err != nil {
		return nil, fmt.Errorf("split DDL into statements: %w", err)
	}
	build := &builder{namespaces: map[string]*schema.Namespace{}, checkCounts: map[string]int{}}
	for index, statement := range statements {
		cursor, err := newReader(statement)
		if err != nil {
			return nil, fmt.Errorf("statement %d: %w", index+1, err)
		}
		if cursor.done() {
			continue
		}
		if err := build.statement(cursor); err != nil {
			return nil, fmt.Errorf("statement %d: %w", index+1, err)
		}
	}
	return build.result()
}

// builder accumulates namespaces while statements are parsed.
type builder struct {
	namespaces  map[string]*schema.Namespace
	viewRefs    map[string][]string
	checkCounts map[string]int
}

func (b *builder) namespace(name string) *schema.Namespace {
	if name == "" {
		name = "public"
	}
	if existing, ok := b.namespaces[name]; ok {
		return existing
	}
	created := &schema.Namespace{ID: "ns:" + name, Name: name}
	b.namespaces[name] = created
	return created
}

func (b *builder) statement(cursor *reader) error {
	trimmed := cursor.statement
	if !cursor.peek().IsKeyword("create") {
		return fmt.Errorf("unsupported statement %q: only CREATE statements are supported", truncate(trimmed, 60))
	}
	cursor.position++
	for {
		switch {
		case cursor.acceptWords("or", "replace"),
			cursor.acceptWords("if", "not", "exists"),
			cursor.acceptWords("temp"), cursor.acceptWords("temporary"),
			cursor.acceptWords("global"), cursor.acceptWords("local"),
			cursor.acceptWords("unlogged"), cursor.acceptWords("recursive"):
			continue
		}
		break
	}
	if cursor.peek().IsKeyword("materialized") {
		return fmt.Errorf("materialized views are not supported")
	}
	if cursor.peek().IsKeyword("unique") || cursor.peek().IsKeyword("index") {
		return b.createIndex(cursor)
	}
	switch {
	case cursor.peek().IsKeyword("table"):
		return b.createTable(cursor)
	case cursor.peek().IsKeyword("view"):
		return b.createView(cursor)
	case cursor.peek().IsKeyword("type"):
		return b.createType(cursor)
	case cursor.peek().IsKeyword("sequence"):
		return b.createSequence(cursor)
	}
	return fmt.Errorf("unsupported CREATE statement %q", truncate(trimmed, 60))
}

// result sorts every slice by ID so that the hash is deterministic.
func (b *builder) result() (*schema.Schema, error) {
	b.resolveViewDependencies()
	result := &schema.Schema{Namespaces: []schema.Namespace{}}
	for _, namespace := range b.namespaces {
		sortNamespace(namespace)
		result.Namespaces = append(result.Namespaces, *namespace)
	}
	sort.Slice(result.Namespaces, func(i, j int) bool {
		return result.Namespaces[i].ID < result.Namespaces[j].ID
	})
	if err := schema.Validate(result); err != nil {
		return nil, fmt.Errorf("parsed schema is invalid: %w", err)
	}
	return result, nil
}
