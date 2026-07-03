package parser

import (
	"fmt"
	"strings"

	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// reader is a forward-only cursor over the tokens of a single SQL statement.
type reader struct {
	statement string
	tokens    []sqlexpr.Token
	position  int
}

func newReader(statement string) (*reader, error) {
	tokens, err := sqlexpr.Tokenize(statement)
	if err != nil {
		return nil, fmt.Errorf("parse statement %q: %w", truncate(statement, 60), err)
	}
	return &reader{statement: statement, tokens: tokens}, nil
}

func truncate(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

// position returns a human readable offset used in error messages.
func (r *reader) offset() string {
	return fmt.Sprintf("at %q", truncate(r.statement, 80))
}

func (r *reader) done() bool {
	return r.position >= len(r.tokens)
}

func (r *reader) peek() sqlexpr.Token {
	if r.done() {
		return sqlexpr.Token{Kind: sqlexpr.KindEOF}
	}
	return r.tokens[r.position]
}

func (r *reader) peekAt(offset int) sqlexpr.Token {
	index := r.position + offset
	if index >= len(r.tokens) {
		return sqlexpr.Token{Kind: sqlexpr.KindEOF}
	}
	return r.tokens[index]
}

func (r *reader) take() sqlexpr.Token {
	token := r.peek()
	if !r.done() {
		r.position++
	}
	return token
}

// accept consumes the next token when it matches the kind and value.
func (r *reader) accept(kind sqlexpr.Kind, value string) bool {
	if r.peek().Kind == kind && r.peek().Value == value {
		r.position++
		return true
	}
	return false
}

// acceptWords consumes the given keyword sequence when it matches.
func (r *reader) acceptWords(words ...string) bool {
	for offset, word := range words {
		if !r.peekAt(offset).IsKeyword(word) {
			return false
		}
	}
	r.position += len(words)
	return true
}

func (r *reader) expectKeyword(word string) error {
	if !r.acceptWords(word) {
		return fmt.Errorf("expected %s %s, found %s", strings.ToUpper(word), r.offset(), r.describe(r.peek()))
	}
	return nil
}

func (r *reader) expectPunctuation(symbol string) error {
	if !r.accept(sqlexpr.KindPunctuation, symbol) {
		return fmt.Errorf("expected %q %s, found %s", symbol, r.offset(), r.describe(r.peek()))
	}
	return nil
}

func (r *reader) describe(token sqlexpr.Token) string {
	switch token.Kind {
	case sqlexpr.KindEOF:
		return "end of statement"
	case sqlexpr.KindIdentifier, sqlexpr.KindQuotedIdentifier:
		return fmt.Sprintf("%q", token.Value)
	default:
		return fmt.Sprintf("%q", token.Value)
	}
}

// identifier reads a plain or quoted identifier and returns its value.
func (r *reader) identifier() (string, error) {
	token := r.peek()
	switch token.Kind {
	case sqlexpr.KindIdentifier:
		r.position++
		return token.Value, nil
	case sqlexpr.KindQuotedIdentifier:
		r.position++
		return token.Value, nil
	}
	return "", fmt.Errorf("expected an identifier %s, found %s", r.offset(), r.describe(token))
}

// qualifiedName reads `name` or `schema.name`.
func (r *reader) qualifiedName() (string, string, error) {
	name, err := r.identifier()
	if err != nil {
		return "", "", err
	}
	if r.accept(sqlexpr.KindPunctuation, ".") {
		local, err := r.identifier()
		if err != nil {
			return "", "", err
		}
		return name, local, nil
	}
	return "", name, nil
}

// skipBalancedParens consumes a parenthesised group including both parentheses.
func (r *reader) skipBalancedParens() ([]sqlexpr.Token, error) {
	if err := r.expectPunctuation("("); err != nil {
		return nil, err
	}
	depth := 1
	start := r.position
	for !r.done() {
		token := r.take()
		switch {
		case token.IsPunctuation("("):
			depth++
		case token.IsPunctuation(")"):
			depth--
			if depth == 0 {
				return r.tokens[start : r.position-1], nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced parentheses %s", r.offset())
}
