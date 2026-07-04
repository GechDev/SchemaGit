package sqlexpr

import (
	"fmt"
	"strings"

	"github.com/schemagit/schemagit/internal/pgtype"
)

var continuationWords = map[string]bool{
	"varying":   true,
	"precision": true,
	"without":   true,
	"with":      true,
	"time":      true,
	"zone":      true,
}

var keywords = map[string]bool{
	"all": true, "alter": true, "and": true, "as": true, "asc": true, "between": true,
	"both": true, "by": true, "case": true, "cast": true, "check": true, "collate": true,
	"column": true, "constraint": true, "create": true, "cross": true, "current_date": true,
	"current_time": true, "current_timestamp": true, "default": true, "deferrable": true,
	"desc": true, "distinct": true, "do": true, "else": true, "end": true, "except": true,
	"exists": true, "false": true, "fetch": true, "first": true, "for": true, "foreign": true,
	"from": true, "full": true, "grant": true, "greatest": true, "group": true, "having": true,
	"ilike": true, "in": true, "initially": true, "inner": true, "insert": true, "intersect": true,
	"into": true, "is": true, "join": true, "key": true, "last": true, "lateral": true,
	"leading": true, "least": true, "left": true, "like": true, "limit": true, "localtime": true,
	"localtimestamp": true, "natural": true, "not": true, "null": true, "nulls": true,
	"offset": true, "on": true, "only": true, "or": true, "order": true, "outer": true,
	"over": true, "overlaps": true, "primary": true, "references": true, "returning": true,
	"right": true, "select": true, "set": true, "similar": true, "some": true, "symmetric": true,
	"table": true, "then": true, "to": true, "trailing": true, "true": true, "union": true,
	"unique": true, "unbounded": true, "update": true, "using": true, "values": true,
	"when": true, "where": true, "window": true, "with": true, "within": true,
}

var connectiveKeywords = map[string]bool{
	"and": true, "or": true, "like": true, "ilike": true, "in": true, "is": true,
	"between": true, "not": true, "collate": true, "overlaps": true, "similar": true,
	"at": true, "operator": true,
}

var expressionStartKeywords = map[string]bool{
	"select": true, "where": true, "having": true, "on": true, "when": true,
	"then": true, "else": true, "check": true, "default": true, "values": true,
	"set": true, "by": true, "returning": true, "case": true, "exists": true,
	"as": true, "constraint": true, "do": true, "into": true, "using": true,
	"all": true, "any": true, "some": true, "array": true, "row": true,
	"distinct": true, "from": true, "group": true, "order": true, "union": true,
	"intersect": true, "except": true, "with": true, "primary": true,
	"foreign": true, "unique": true, "key": true, "references": true,
	"index": true, "table": true, "create": true, "alter": true, "limit": true,
	"offset": true, "true": true, "false": true, "null": true,
}

var expressionEndKeywords = map[string]bool{
	"where": true, "group": true, "having": true, "order": true, "limit": true,
	"offset": true, "then": true, "else": true, "end": true, "when": true,
	"from": true, "set": true, "values": true, "returning": true, "default": true,
	"check": true, "into": true, "as": true, "do": true, "on": true, "using": true,
	"for": true, "to": true, "constraint": true, "references": true,
	"primary": true, "foreign": true, "unique": true, "key": true,
}

// Canonical normalises a SQL expression to SchemaGit's stored form: unquoted
// identifiers are lowercased, `public.` qualification and redundant parentheses
// are removed, explicit `::casts` are dropped, and token spacing is fixed.
// Both parsed DDL text and pg_catalog output pass through this function.
func Canonical(expression string) (string, error) {
	tokens, err := Tokenize(expression)
	if err != nil {
		return "", err
	}
	return CanonicalTokens(tokens)
}

// CanonicalDefault normalises a column default expression. PostgreSQL stores
// defaults with an explicit cast for literal and NULL values, so the canonical
// form re-adds it; this keeps parsed DDL and catalog output identical.
func CanonicalDefault(expression, columnType string) (string, error) {
	canonical, err := Canonical(expression)
	if err != nil {
		return "", err
	}
	if canonical == "" {
		return "", nil
	}
	if sequence, ok := canonicalNextval(canonical); ok {
		return sequence, nil
	}
	tokens, err := Tokenize(canonical)
	if err != nil {
		return "", err
	}
	if !isSingleLiteralOrNull(tokens) {
		return canonical, nil
	}
	canonicalType, err := pgtype.FromCatalog(pgtype.ToCatalog(columnType))
	if err != nil {
		return "", fmt.Errorf("default expression: %w", err)
	}
	return canonical + "::" + pgtype.ToCatalog(canonicalType), nil
}

func isSingleLiteralOrNull(tokens []Token) bool {
	if len(tokens) != 1 {
		return false
	}
	return tokens[0].Kind == KindString || tokens[0].IsKeyword("null")
}

func canonicalNextval(canonical string) (string, bool) {
	const prefix = "nextval("
	if !strings.HasPrefix(canonical, prefix) || !strings.HasSuffix(canonical, ")") {
		return "", false
	}
	argument := strings.TrimSpace(canonical[len(prefix) : len(canonical)-1])
	if len(argument) < 3 || !strings.HasPrefix(argument, "'") || !strings.HasSuffix(argument, "'") {
		return "", false
	}
	literal := argument[1 : len(argument)-1]
	name := pgtype.Unqualify(literal)
	if name == "" {
		return "", false
	}
	return "nextval('" + name + "'::regclass)", true
}

func stripCasts(tokens []Token) []Token {
	result := make([]Token, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		if !tokens[index].IsPunctuation("::") {
			result = append(result, tokens[index])
			continue
		}
		index++
		if index < len(tokens) && tokens[index].Kind == KindIdentifier {
			index++
			for index < len(tokens) && tokens[index].Kind == KindIdentifier && continuationWords[tokens[index].Value] {
				index++
			}
		}
		if index < len(tokens) && tokens[index].IsPunctuation("(") {
			index = skipBalanced(tokens, index)
		}
		for index+1 < len(tokens) && tokens[index].IsPunctuation("[") && tokens[index+1].IsPunctuation("]") {
			index += 2
		}
		index--
	}
	return result
}

func skipBalanced(tokens []Token, index int) int {
	depth := 0
	for index < len(tokens) {
		if tokens[index].IsPunctuation("(") {
			depth++
		}
		if tokens[index].IsPunctuation(")") {
			depth--
			if depth == 0 {
				return index
			}
		}
		index++
	}
	return len(tokens)
}

func stripDefaultNamespace(tokens []Token) []Token {
	result := make([]Token, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		if index+2 < len(tokens) &&
			tokens[index].IsKeyword(pgtype.DefaultNamespace) &&
			tokens[index+1].IsPunctuation(".") &&
			(tokens[index+2].Kind == KindIdentifier || tokens[index+2].Kind == KindQuotedIdentifier) {
			index++
			continue
		}
		result = append(result, tokens[index])
	}
	return result
}

// stripRelationQualifiers removes `<relation>.` prefixes that qualify a column
// with the only relation the statement reads. pg_get_viewdef always qualifies
// columns with their table, while hand written DDL usually does not, so both
// forms have to converge on the same canonical text.
func stripRelationQualifiers(tokens []Token) []Token {
	qualifiers := relationQualifiers(tokens)
	if len(qualifiers) == 0 {
		return tokens
	}
	result := make([]Token, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		if opensQualifier(tokens, index) &&
			qualifiers[strings.ToLower(tokens[index].Value)] &&
			index+2 < len(tokens) && tokens[index+1].IsPunctuation(".") &&
			(tokens[index+2].Kind == KindIdentifier || tokens[index+2].Kind == KindQuotedIdentifier) {
			index++
			continue
		}
		result = append(result, tokens[index])
	}
	return result
}

// relationQualifiers collects the relations a statement reads plus the aliases
// assigned to them, which are the only names allowed to qualify a column. The
// schema part of a qualified relation name is deliberately excluded so that
// `from app.users` keeps its schema prefix.
func relationQualifiers(tokens []Token) map[string]bool {
	qualifiers := map[string]bool{}
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Kind != KindIdentifier || !relationKeywords[tokens[index].Value] {
			continue
		}
		cursor := index + 1
		if cursor < len(tokens) && tokens[cursor].IsPunctuation("(") {
			continue
		}
		last := ""
		for cursor < len(tokens) {
			token := tokens[cursor]
			if token.Kind != KindIdentifier && token.Kind != KindQuotedIdentifier {
				break
			}
			last = strings.ToLower(token.Value)
			cursor++
			if cursor+1 < len(tokens) && tokens[cursor].IsPunctuation(".") &&
				(tokens[cursor+1].Kind == KindIdentifier || tokens[cursor+1].Kind == KindQuotedIdentifier) {
				cursor++
				continue
			}
			break
		}
		if last == "" {
			continue
		}
		qualifiers[last] = true
		if cursor < len(tokens) {
			next := tokens[cursor]
			if next.Kind == KindQuotedIdentifier ||
				(next.Kind == KindIdentifier && !keywords[next.Value]) {
				qualifiers[strings.ToLower(next.Value)] = true
				cursor++
			}
		}
		index = cursor - 1
	}
	return qualifiers
}

// opensQualifier reports whether the token at index can be the first part of a
// qualified reference such as `users.id` or `u.email`.
func opensQualifier(tokens []Token, index int) bool {
	if index == 0 {
		return true
	}
	previous := tokens[index-1]
	switch previous.Kind {
	case KindPunctuation:
		return previous.Value == "(" || previous.Value == ","
	case KindIdentifier:
		return keywords[previous.Value]
	}
	return false
}

// dropRedundantParens removes parentheses that wrap a whole expression term.
// A group is only dropped when it occupies a complete operand position: it must
// not be the operand of a surrounding operator and it must not contain a
// top-level comma, so `count(*)`, `(a + b) * c` and `x in (1, 2)` all survive.
func dropRedundantParens(tokens []Token) []Token {
	for {
		changed := false
		for index := 0; index < len(tokens); index++ {
			if !tokens[index].IsPunctuation("(") {
				continue
			}
			end := matchParen(tokens, index)
			if end < 0 || end == index+1 {
				break
			}
			if !opensOperand(tokens, index) || !closesOperand(tokens, end) {
				continue
			}
			if hasTopLevelComma(tokens, index+1, end) {
				continue
			}
			if topLevelOperatorCount(tokens, index+1, end) > 1 {
				continue
			}
			inner := dropRedundantParens(append([]Token{}, tokens[index+1:end]...))
			prefix := append([]Token{}, tokens[:index]...)
			suffix := append([]Token{}, tokens[end+1:]...)
			tokens = append(append(prefix, inner...), suffix...)
			changed = true
			break
		}
		if !changed {
			return tokens
		}
	}
}

func opensOperand(tokens []Token, index int) bool {
	if index == 0 {
		return true
	}
	previous := tokens[index-1]
	switch {
	case previous.IsPunctuation("("), previous.IsPunctuation(","):
		return true
	case previous.Kind == KindIdentifier:
		return expressionStartKeywords[previous.Value] || connectiveKeywords[previous.Value]
	}
	return false
}

func closesOperand(tokens []Token, index int) bool {
	if index == len(tokens)-1 {
		return true
	}
	next := tokens[index+1]
	switch {
	case next.IsPunctuation(")"), next.IsPunctuation(","), next.IsPunctuation(";"):
		return true
	case next.Kind == KindIdentifier:
		return expressionEndKeywords[next.Value]
	}
	return false
}

func hasTopLevelComma(tokens []Token, start, end int) bool {
	depth := 0
	for index := start; index < end; index++ {
		switch {
		case tokens[index].IsPunctuation("("):
			depth++
		case tokens[index].IsPunctuation(")"):
			depth--
		case depth == 0 && tokens[index].IsPunctuation(","):
			return true
		}
	}
	return false
}

func matchParen(tokens []Token, start int) int {
	depth := 0
	for index := start; index < len(tokens); index++ {
		if tokens[index].IsPunctuation("(") {
			depth++
		}
		if tokens[index].IsPunctuation(")") {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func topLevelOperatorCount(tokens []Token, start, end int) int {
	count := 0
	depth := 0
	for index := start; index < end; index++ {
		switch {
		case tokens[index].IsPunctuation("("):
			depth++
		case tokens[index].IsPunctuation(")"):
			depth--
		case depth != 0:
		case tokens[index].Kind == KindOperator:
			count++
		case tokens[index].Kind == KindIdentifier && connectiveKeywords[tokens[index].Value]:
			if tokens[index].Value == "not" || tokens[index].Value == "is" || tokens[index].Value == "collate" {
				continue
			}
			count++
		}
	}
	return count
}

// Render joins tokens with SchemaGit's fixed spacing rules.
func Render(tokens []Token) string {
	var builder strings.Builder
	previous := Token{Kind: KindEOF}
	for index, token := range tokens {
		if index > 0 && needsSpace(previous, token) {
			builder.WriteByte(' ')
		}
		switch token.Kind {
		case KindQuotedIdentifier:
			builder.WriteByte('"')
			builder.WriteString(token.Value)
			builder.WriteByte('"')
		default:
			builder.WriteString(token.Value)
		}
		previous = token
	}
	return builder.String()
}

func needsSpace(previous, current Token) bool {
	switch {
	case current.IsPunctuation(","), current.IsPunctuation(";"),
		current.IsPunctuation(")"), current.IsPunctuation("]"), current.IsPunctuation("."):
		return false
	case previous.IsPunctuation("("), previous.IsPunctuation("["), previous.IsPunctuation("."):
		return false
	case previous.IsPunctuation(","):
		return true
	case current.IsPunctuation("("):
		if previous.Kind == KindIdentifier && !keywords[previous.Value] {
			return false
		}
		return true
	}
	return true
}
