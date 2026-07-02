// Package sqlexpr provides a small PostgreSQL text lexer plus the canonical
// expression form that SchemaGit stores for defaults, check constraints,
// partial index predicates and view bodies. Canonicalising both parsed DDL and
// catalog output through the same function is what makes "parse then introspect"
// round-trip byte-for-byte.
package sqlexpr

import (
	"fmt"
	"strings"
	"unicode"
)

// Kind classifies a lexical token.
type Kind int

const (
	// KindEOF marks the end of the token stream.
	KindEOF Kind = iota
	// KindIdentifier is an unquoted identifier or keyword, already lowercased.
	KindIdentifier
	// KindQuotedIdentifier is a double quoted identifier, case preserved.
	KindQuotedIdentifier
	// KindString is a single quoted string literal, escapes preserved.
	KindString
	// KindNumber is a numeric literal.
	KindNumber
	// KindOperator is an operator such as =, <=> or <>.
	KindOperator
	// KindPunctuation is one of ( ) , ; . ::
	KindPunctuation
)

// Token is a single lexical unit.
type Token struct {
	Kind  Kind
	Value string
}

// IsPunctuation reports whether the token is the given punctuation symbol.
func (t Token) IsPunctuation(symbol string) bool {
	return t.Kind == KindPunctuation && t.Value == symbol
}

// IsKeyword reports whether the token is the given (case-insensitive) keyword.
func (t Token) IsKeyword(keyword string) bool {
	return t.Kind == KindIdentifier && strings.EqualFold(t.Value, keyword)
}

// IsOperator reports whether the token is the given operator.
func (t Token) IsOperator(operator string) bool {
	return t.Kind == KindOperator && t.Value == operator
}

func isIdentifierStart(character rune) bool {
	return character == '_' || unicode.IsLetter(character)
}

func isIdentifierPart(character rune) bool {
	return isIdentifierStart(character) || unicode.IsDigit(character) || character == '$'
}

func isOperatorCharacter(character rune) bool {
	switch character {
	case '+', '-', '*', '/', '<', '>', '=', '~', '!', '@', '#', '%', '^', '&', '|', '`', '?':
		return true
	}
	return false
}

// Tokenize splits SQL text into tokens. Comments and dollar-quoted bodies are
// skipped; the returned error names the offending offset when the text cannot be
// tokenized.
func Tokenize(input string) ([]Token, error) {
	tokens := make([]Token, 0, len(input)/4)
	runes := []rune(input)
	index := 0
	for index < len(runes) {
		character := runes[index]
		switch {
		case unicode.IsSpace(character):
			index++
		case character == '-' && index+1 < len(runes) && runes[index+1] == '-':
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
		case character == '/' && index+1 < len(runes) && runes[index+1] == '*':
			end := index + 2
			nested := 1
			for end < len(runes) && nested > 0 {
				if runes[end] == '/' && end+1 < len(runes) && runes[end+1] == '*' {
					nested++
					end += 2
					continue
				}
				if runes[end] == '*' && end+1 < len(runes) && runes[end+1] == '/' {
					nested--
					end += 2
					continue
				}
				end++
			}
			if nested != 0 {
				return nil, fmt.Errorf("unterminated block comment at offset %d", index)
			}
			index = end
		case character == '\'':
			end := index + 1
			for end < len(runes) {
				if runes[end] != '\'' {
					end++
					continue
				}
				if end+1 < len(runes) && runes[end+1] == '\'' {
					end += 2
					continue
				}
				break
			}
			if end >= len(runes) {
				return nil, fmt.Errorf("unterminated string literal at offset %d", index)
			}
			tokens = append(tokens, Token{Kind: KindString, Value: string(runes[index : end+1])})
			index = end + 1
		case character == '"':
			end := index + 1
			for end < len(runes) {
				if runes[end] != '"' {
					end++
					continue
				}
				if end+1 < len(runes) && runes[end+1] == '"' {
					end += 2
					continue
				}
				break
			}
			if end >= len(runes) {
				return nil, fmt.Errorf("unterminated quoted identifier at offset %d", index)
			}
			tokens = append(tokens, Token{Kind: KindQuotedIdentifier, Value: string(runes[index+1 : end])})
			index = end + 1
		case character == '$':
			end := index + 1
			for end < len(runes) && isIdentifierPart(runes[end]) && runes[end] != '$' {
				end++
			}
			if end < len(runes) && runes[end] == '$' {
				tag := string(runes[index : end+1])
				closing := strings.Index(string(runes[end+1:]), tag)
				if closing < 0 {
					return nil, fmt.Errorf("unterminated dollar-quoted string at offset %d", index)
				}
				tokens = append(tokens, Token{Kind: KindString, Value: string(runes[index : end+1+closing+len(tag)])})
				index = end + closing + 1 + len(tag)
				continue
			}
			return nil, fmt.Errorf("unexpected dollar sign at offset %d", index)
		case unicode.IsDigit(character):
			end := index
			for end < len(runes) && (unicode.IsDigit(runes[end]) || runes[end] == '.') {
				end++
			}
			if end < len(runes) && (runes[end] == 'e' || runes[end] == 'E') {
				probe := end + 1
				if probe < len(runes) && (runes[probe] == '+' || runes[probe] == '-') {
					probe++
				}
				if probe < len(runes) && unicode.IsDigit(runes[probe]) {
					end = probe
					for end < len(runes) && unicode.IsDigit(runes[end]) {
						end++
					}
				}
			}
			tokens = append(tokens, Token{Kind: KindNumber, Value: string(runes[index:end])})
			index = end
		case isIdentifierStart(character):
			end := index
			for end < len(runes) && isIdentifierPart(runes[end]) {
				end++
			}
			tokens = append(tokens, Token{Kind: KindIdentifier, Value: strings.ToLower(string(runes[index:end]))})
			index = end
		case isOperatorCharacter(character):
			end := index
			for end < len(runes) && isOperatorCharacter(runes[end]) {
				end++
			}
			tokens = append(tokens, Token{Kind: KindOperator, Value: string(runes[index:end])})
			index = end
		case strings.ContainsRune("(),;.[]:", character):
			if character == ':' && index+1 < len(runes) && runes[index+1] == ':' {
				tokens = append(tokens, Token{Kind: KindPunctuation, Value: "::"})
				index += 2
				continue
			}
			tokens = append(tokens, Token{Kind: KindPunctuation, Value: string(character)})
			index++
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", string(character), index)
		}
	}
	return tokens, nil
}
