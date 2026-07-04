package parser

import (
	"fmt"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

func (b *builder) createView(cursor *reader) error {
	if err := cursor.expectKeyword("view"); err != nil {
		return err
	}
	namespace, name, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	if cursor.peek().IsPunctuation("(") {
		if _, err := cursor.skipBalancedParens(); err != nil {
			return err
		}
	}
	if cursor.peek().IsKeyword("with") {
		return fmt.Errorf("view storage parameters are not supported (%s)", viewID(namespace, name))
	}
	if err := cursor.expectKeyword("as"); err != nil {
		return err
	}
	if cursor.done() {
		return fmt.Errorf("CREATE VIEW %s has no query", viewID(namespace, name))
	}
	body, err := queryBody(cursor.tokens[cursor.position:])
	if err != nil {
		return err
	}
	definition, err := sqlexpr.CanonicalTokens(body)
	if err != nil {
		return err
	}
	if definition == "" {
		return fmt.Errorf("CREATE VIEW %s has an empty query", viewID(namespace, name))
	}
	target := b.namespace(namespace)
	if findView(target, name) != nil {
		return fmt.Errorf("view %s is defined twice", viewID(namespace, name))
	}
	target.Views = append(target.Views, schema.View{
		ID:         viewID(namespace, name),
		Name:       name,
		Definition: definition,
		DependsOn:  []string{},
	})
	if b.viewRefs == nil {
		b.viewRefs = map[string][]string{}
	}
	b.viewRefs[viewID(namespace, name)] = referencedRelations(body)
	return nil
}

// queryBody validates the view body and returns its tokens with any trailing
// statement separator removed.
func queryBody(body []sqlexpr.Token) ([]sqlexpr.Token, error) {
	end := len(body)
	for end > 0 {
		token := body[end-1]
		if token.IsPunctuation(";") {
			end--
			continue
		}
		break
	}
	if end == 0 {
		return nil, fmt.Errorf("view query is empty")
	}
	return body[:end], nil
}

// referencedRelations collects the relation names a view body reads. Names that
// do not resolve to a known table or view are dropped: they are most likely CTE
// aliases, which are not part of the schema model.
func referencedRelations(body []sqlexpr.Token) []string {
	return sqlexpr.RelationTokens(body)
}

// resolveViewDependencies fills in View.DependsOn once every table and view is
// known. Dependencies are stored as IDs and sorted for deterministic hashing.
func (b *builder) resolveViewDependencies() {
	if len(b.viewRefs) == 0 {
		return
	}
	for _, namespace := range b.namespaces {
		for index := range namespace.Views {
			view := &namespace.Views[index]
			dependencies := []string{}
			for _, name := range b.viewRefs[view.ID] {
				if table := findTable(namespace, name); table != nil {
					dependencies = append(dependencies, table.ID)
					continue
				}
				if referenced := findView(namespace, name); referenced != nil {
					dependencies = append(dependencies, referenced.ID)
				}
			}
			if len(dependencies) > 0 {
				view.DependsOn = uniqueSorted(dependencies)
			}
		}
	}
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	sortStrings(unique)
	return unique
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}
