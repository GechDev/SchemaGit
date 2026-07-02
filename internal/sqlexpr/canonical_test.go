package sqlexpr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalMatchesCatalogOutput(t *testing.T) {
	parsed := "SELECT id, email FROM public.users WHERE email <> ''"
	deparsed := " SELECT id,\n  email\n   FROM users\n  WHERE (email <> ''::text); "
	expected := "select id, email from users where email <> ''"

	first, err := Canonical(parsed)
	require.NoError(t, err)
	second, err := Canonical(deparsed)
	require.NoError(t, err)

	require.Equal(t, expected, first)
	require.Equal(t, expected, second)
}

func TestCanonicalKeepsMeaningfulParentheses(t *testing.T) {
	canonical, err := Canonical("(a + b) * c")
	require.NoError(t, err)
	require.Equal(t, "(a + b) * c", canonical)

	canonical, err = Canonical("x IN (1, 2, 3)")
	require.NoError(t, err)
	require.Equal(t, "x in (1, 2, 3)", canonical)

	canonical, err = Canonical("((a AND b) OR c)")
	require.NoError(t, err)
	require.Equal(t, "(a and b) or c", canonical)

	canonical, err = Canonical("(a AND b) OR c")
	require.NoError(t, err)
	require.Equal(t, "(a and b) or c", canonical)
}

func TestCanonicalStripsCasts(t *testing.T) {
	for _, input := range []string{
		"price::numeric(10,2)",
		"tags::text[]",
		"name::character varying",
		"created::timestamp without time zone",
		"value::double precision",
	} {
		canonical, err := Canonical(input)
		require.NoError(t, err, input)
		require.NotContains(t, canonical, "::", input)
	}

	canonical, err := Canonical("a::int4 + b::int8")
	require.NoError(t, err)
	require.Equal(t, "a + b", canonical)
}

func TestCanonicalDefaultAddsCastForLiterals(t *testing.T) {
	cases := []struct{ expression, columnType, expected string }{
		{"'unknown'", "text", "'unknown'::text"},
		{"'unknown'::text", "text", "'unknown'::text"},
		{"'x'", "varchar(20)", "'x'::character varying(20)"},
		{"NULL", "text", "null::text"},
		{"0", "int4", "0"},
		{"true", "bool", "true"},
		{"now()", "timestamptz", "now()"},
		{"nextval('public.user_id_seq'::regclass)", "int8", "nextval('user_id_seq'::regclass)"},
		{"nextval('user_id_seq')", "int8", "nextval('user_id_seq'::regclass)"},
		{"'admin'", "user_role", "'admin'::user_role"},
		{"'2020-01-01'::date", "date", "'2020-01-01'::date"},
		{"", "text", ""},
	}
	for _, item := range cases {
		actual, err := CanonicalDefault(item.expression, item.columnType)
		require.NoError(t, err, item.expression)
		require.Equal(t, item.expected, actual, item.expression)
	}
}

func TestCanonicalDefaultRejectsBrokenSQL(t *testing.T) {
	_, err := CanonicalDefault("'unterminated", "text")
	require.Error(t, err)
}

func TestTokenizeSkipsComments(t *testing.T) {
	tokens, err := Tokenize("SELECT /* inline */ a -- trailing\nFROM t")
	require.NoError(t, err)
	var identifiers []string
	for _, token := range tokens {
		if token.Kind == KindIdentifier {
			identifiers = append(identifiers, token.Value)
		}
	}
	require.Equal(t, []string{"select", "a", "from", "t"}, identifiers)
}

func TestTokenizeQuotedIdentifiersAndEscapes(t *testing.T) {
	tokens, err := Tokenize(`"Mixed""Case" 'it''s' 1.5e3`)
	require.NoError(t, err)
	require.Equal(t, KindQuotedIdentifier, tokens[0].Kind)
	require.Equal(t, `Mixed""Case`, tokens[0].Value)
	require.Equal(t, KindString, tokens[1].Kind)
	require.Equal(t, `'it''s'`, tokens[1].Value)
	require.Equal(t, KindNumber, tokens[2].Kind)
	require.Equal(t, "1.5e3", tokens[2].Value)
}

func TestTokenizeDollarQuoted(t *testing.T) {
	tokens, err := Tokenize("$tag$ body with $tag$ after")
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	require.Equal(t, "$tag$ body with $tag$", tokens[0].Value)
	require.Equal(t, "after", tokens[1].Value)
}

func TestTokenizeRejectsMalformedInput(t *testing.T) {
	inputs := []string{"'unterminated", "\"unterminated", "$tag$ open", "/* open", "name \x00abc"}
	for _, input := range inputs {
		_, err := Tokenize(input)
		require.Error(t, err, input)
	}
}

func TestTokenPredicates(t *testing.T) {
	comma := Token{Kind: KindPunctuation, Value: ","}
	require.True(t, comma.IsPunctuation(","))
	require.False(t, comma.IsPunctuation("("))
	require.True(t, Token{Kind: KindIdentifier, Value: "Select"}.IsKeyword("select"))
	require.False(t, Token{Kind: KindOperator, Value: "<>="}.IsOperator("<>"))
}
