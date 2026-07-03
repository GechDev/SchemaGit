package parser

import (
	"fmt"
	"strings"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// unquoteLabel turns a SQL string literal into the label PostgreSQL stores in
// pg_enum.enumlabel.
func unquoteLabel(literal string) (string, error) {
	if len(literal) < 2 || !strings.HasPrefix(literal, "'") || !strings.HasSuffix(literal, "'") {
		return "", fmt.Errorf("invalid enum label %s", literal)
	}
	return strings.ReplaceAll(literal[1:len(literal)-1], "''", "'"), nil
}

func (b *builder) createType(cursor *reader) error {
	if err := cursor.expectKeyword("type"); err != nil {
		return err
	}
	namespace, name, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	if err := cursor.expectKeyword("as"); err != nil {
		return err
	}
	if !cursor.acceptWords("enum") {
		return fmt.Errorf("only CREATE TYPE ... AS ENUM is supported (%s)", enumID(namespace, name))
	}
	start := cursor.position
	if _, err := cursor.skipBalancedParens(); err != nil {
		return err
	}
	values := []string{}
	for index := start + 1; index < cursor.position-1; index++ {
		token := cursor.tokens[index]
		if token.IsPunctuation(",") {
			continue
		}
		if token.Kind != sqlexpr.KindString {
			return fmt.Errorf("enum %s must use string labels", enumID(namespace, name))
		}
		label, err := unquoteLabel(token.Value)
		if err != nil {
			return fmt.Errorf("enum %s: %w", enumID(namespace, name), err)
		}
		values = append(values, label)
	}
	if len(values) == 0 {
		return fmt.Errorf("enum %s has no values", enumID(namespace, name))
	}
	target := b.namespace(namespace)
	for _, existing := range target.Enums {
		if existing.Name == name {
			return fmt.Errorf("enum %s is defined twice", enumID(namespace, name))
		}
	}
	target.Enums = append(target.Enums, schema.Enum{
		ID:     enumID(namespace, name),
		Name:   name,
		Values: values,
	})
	return nil
}
