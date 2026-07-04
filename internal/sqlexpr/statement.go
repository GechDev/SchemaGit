package sqlexpr

import (
	"fmt"
	"strings"
)

// Statements splits a SQL script into individual statements, ignoring
// semicolons inside string literals, quoted identifiers, dollar-quoted bodies
// and comments. Statement text is returned verbatim so callers can canonicalise
// expressions with their original spacing.
func Statements(script string) ([]string, error) {
	statements := make([]string, 0, 8)
	runes := []rune(script)
	start := 0
	index := 0
	for index < len(runes) {
		character := runes[index]
		switch {
		case character == '-' && index+1 < len(runes) && runes[index+1] == '-':
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
		case character == '/' && index+1 < len(runes) && runes[index+1] == '*':
			end := index + 2
			for end < len(runes) {
				if runes[end] == '*' && end+1 < len(runes) && runes[end+1] == '/' {
					end += 2
					break
				}
				end++
			}
			if end > len(runes) || end == len(runes) {
				return nil, fmt.Errorf("unterminated block comment at offset %d", index)
			}
			index = end
		case character == '\'' || character == '"':
			end, err := scanQuoted(runes, index)
			if err != nil {
				return nil, err
			}
			index = end
		case character == '$':
			if end, ok := scanDollarQuoted(runes, index); ok {
				index = end
				break
			}
			index++
		case character == ';':
			statement := strings.TrimSpace(string(runes[start:index]))
			if statement != "" {
				statements = append(statements, statement)
			}
			index++
			start = index
		default:
			index++
		}
	}
	if trailing := strings.TrimSpace(string(runes[start:])); trailing != "" {
		statements = append(statements, trailing)
	}
	return statements, nil
}

func scanQuoted(runes []rune, start int) (int, error) {
	quote := runes[start]
	index := start + 1
	for index < len(runes) {
		if runes[index] != quote {
			index++
			continue
		}
		if index+1 < len(runes) && runes[index+1] == quote {
			index += 2
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("unterminated quoted text at offset %d", start)
}

func scanDollarQuoted(runes []rune, start int) (int, bool) {
	end := start + 1
	for end < len(runes) && isIdentifierPart(runes[end]) && runes[end] != '$' {
		end++
	}
	if end >= len(runes) || runes[end] != '$' {
		return 0, false
	}
	tag := string(runes[start : end+1])
	closing := strings.Index(string(runes[end+1:]), tag)
	if closing < 0 {
		return 0, false
	}
	return end + closing + 1 + len(tag), true
}

// CanonicalTokens normalises an already tokenised expression.
func CanonicalTokens(tokens []Token) (string, error) {
	stripped := stripCasts(tokens)
	unqualified := stripDefaultNamespace(stripped)
	reduced := dropRedundantParens(unqualified)
	reduced = stripRelationQualifiers(reduced)
	for len(reduced) > 0 && reduced[len(reduced)-1].IsPunctuation(";") {
		reduced = reduced[:len(reduced)-1]
	}
	return Render(reduced), nil
}

var relationKeywords = map[string]bool{
	"from": true, "join": true, "update": true, "into": true,
}

// Relations returns the relation names a query reads or writes, reduced to
// their final component. Names that cannot be resolved to a schema object are
// still returned so callers can decide how to treat them.
func Relations(statement string) ([]string, error) {
	tokens, err := Tokenize(statement)
	if err != nil {
		return nil, err
	}
	return RelationTokens(tokens), nil
}

// RelationTokens is Relations for an already tokenised statement.
func RelationTokens(body []Token) []string {
	names := []string{}
	seen := map[string]bool{}
	for index := 0; index+1 < len(body); index++ {
		if body[index].Kind != KindIdentifier || !relationKeywords[body[index].Value] {
			continue
		}
		start := index + 1
		if body[start].IsPunctuation("(") {
			continue
		}
		name := readRelationName(body, start)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		index = start
	}
	return names
}

func readRelationName(body []Token, start int) string {
	index := start
	last := ""
	for index < len(body) {
		token := body[index]
		if token.Kind != KindIdentifier && token.Kind != KindQuotedIdentifier {
			break
		}
		last = strings.ToLower(token.Value)
		index++
		if index+1 < len(body) && body[index].IsPunctuation(".") {
			index++
			continue
		}
		break
	}
	return last
}
