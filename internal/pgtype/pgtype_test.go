package pgtype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromDDL(t *testing.T) {
	cases := map[string]string{
		"bigint":                      "int8",
		"integer":                     "int4",
		"int":                         "int4",
		"smallint":                    "int2",
		"boolean":                     "bool",
		"text":                        "text",
		"public.user_role":            "user_role",
		"character varying(255)":      "varchar(255)",
		"numeric(10, 2)":              "numeric(10,2)",
		"double precision":            "float8",
		"timestamp without time zone": "timestamp",
		"timestamp with time zone":    "timestamptz",
		"text[]":                      "text[]",
		"character varying(20)[]":     "varchar(20)[]",
		`"MyType"`:                    "MyType",
	}
	for input, expected := range cases {
		actual, err := FromDDL(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, actual, input)
	}
}

func TestFromDDLRejectsMalformed(t *testing.T) {
	for _, input := range []string{"", "   ", "varchar(255", "public."} {
		_, err := FromDDL(input)
		require.Error(t, err, input)
	}
}

func TestFromCatalogRoundTrip(t *testing.T) {
	cases := map[string]string{
		"bigint":                      "int8",
		"integer":                     "int4",
		"smallint":                    "int2",
		"boolean":                     "bool",
		"character varying(255)":      "varchar(255)",
		"character(10)":               "bpchar(10)",
		"timestamp without time zone": "timestamp",
		"timestamp with time zone":    "timestamptz",
		"time without time zone":      "time",
		"time with time zone":         "timetz",
		"double precision":            "float8",
		"real":                        "float4",
		"text[]":                      "text[]",
	}
	for display, expected := range cases {
		actual, err := FromCatalog(display)
		require.NoError(t, err, display)
		require.Equal(t, expected, actual, display)
		require.Equal(t, display, ToCatalog(actual), display)
	}

	unqualified, err := FromCatalog("public.user_role")
	require.NoError(t, err)
	require.Equal(t, "user_role", unqualified)
}

func TestFromCatalogRejectsMalformed(t *testing.T) {
	for _, input := range []string{"", "varchar(255", "[]"} {
		_, err := FromCatalog(input)
		require.Error(t, err, input)
	}
}

func TestToCatalogUserDefinedType(t *testing.T) {
	require.Equal(t, "user_role", ToCatalog("user_role"))
	require.False(t, IsBuiltin("user_role"))
	require.True(t, IsBuiltin("int8[]"))
	require.Equal(t, "int4", Base("int4(10)"))
	require.Equal(t, "(10,2)", Modifiers("numeric(10,2)"))
	require.Equal(t, "", Modifiers("text"))
}

func TestArrayHelpers(t *testing.T) {
	require.True(t, IsArray("text[]"))
	element, err := Element("text[]")
	require.NoError(t, err)
	require.Equal(t, "text", element)
	require.Equal(t, "text[]", Array("text"))
	_, err = Element("text")
	require.Error(t, err)
	_, err = Element("[]")
	require.Error(t, err)
}
