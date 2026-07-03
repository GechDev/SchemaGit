package parser

import (
	"os"
	"testing"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile("../../testdata/fixtures/" + name)
	require.NoError(t, err)
	return string(contents)
}

func TestParseDDLFixture(t *testing.T) {
	actual, err := ParseDDL(readFixture(t, "simple.sql"))
	require.NoError(t, err)

	expected := &schema.Schema{Namespaces: []schema.Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []schema.Table{{
			ID: "tbl:public.users", Name: "users",
			Columns: []schema.Column{
				{
					ID: "col:public.users.email", Name: "email", Ordinal: 1, Type: "text", Nullable: false,
					Default: "'unknown'::text",
				},
				{
					ID: "col:public.users.id", Name: "id", Ordinal: 2, Type: "int8", Nullable: false,
					Default: "nextval('user_id_seq'::regclass)",
				},
				{
					ID: "col:public.users.role", Name: "role", Ordinal: 3, Type: "user_role", Nullable: false,
				},
			},
			Constraints: []schema.Constraint{
				{
					ID: "con:public.users.users_email_key", Name: "users_email_key", Type: "unique",
					Columns: []string{"email"},
				},
				{
					ID: "con:public.users.users_id_pk", Name: "users_id_pk", Type: "pk",
					Columns: []string{"id"},
				},
			},
			Indexes: []schema.Index{{
				ID: "idx:public.users.users_email_idx", Name: "users_email_idx",
				Columns: []string{"email"}, Method: "btree",
			}},
		}},
		Views: []schema.View{{
			ID: "view:public.active_users", Name: "active_users",
			Definition: "select id, email from users where email <> ''",
			DependsOn:  []string{"tbl:public.users"},
		}},
		Enums: []schema.Enum{{ID: "enum:public.user_role", Name: "user_role", Values: []string{"admin", "member"}}},
		Sequences: []schema.Sequence{{
			ID: "seq:public.user_id_seq", Name: "user_id_seq", DataType: "int8",
			Start: 1, Increment: 1, MinValue: 1, MaxValue: 9223372036854775807,
		}},
	}}}
	require.Equal(t, schema.Hash(expected), schema.Hash(actual))
}

func TestParseDDLIsDeterministic(t *testing.T) {
	fixture := readFixture(t, "simple.sql")
	first, err := ParseDDL(fixture)
	require.NoError(t, err)
	second, err := ParseDDL(fixture)
	require.NoError(t, err)
	require.Equal(t, schema.Hash(first), schema.Hash(second))
}

func TestParseDDLFixtureCatalogOutput(t *testing.T) {
	fixture := readFixture(t, "catalog_output.sql")
	actual, err := ParseDDL(fixture)
	require.NoError(t, err)

	expected, err := ParseDDL(readFixture(t, "simple.sql"))
	require.NoError(t, err)
	require.Equal(t, schema.Hash(expected), schema.Hash(actual))
}

func TestParseDDLRejectsUnsupportedStatements(t *testing.T) {
	cases := map[string]string{
		"drop table":            "DROP TABLE users",
		"alter table":           "ALTER TABLE users ADD COLUMN x int",
		"insert":                "INSERT INTO users VALUES (1)",
		"materialized view":     "CREATE MATERIALIZED VIEW mv AS SELECT 1",
		"composite type":        "CREATE TYPE public.composite AS (a int)",
		"function":              "CREATE FUNCTION public.f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql",
		"serial pseudo type":    "CREATE TABLE t (id serial PRIMARY KEY)",
		"create table like":     "CREATE TABLE t2 (LIKE t1)",
		"exclude constraint":    "CREATE TABLE t (a int, EXCLUDE USING gist (a WITH =))",
		"missing name":          "CREATE TABLE (a int)",
		"duplicate table":       "CREATE TABLE t (a int); CREATE TABLE t (b int)",
		"duplicate column":      "CREATE TABLE t (a int, a int)",
		"unknown table index":   "CREATE INDEX i ON missing (a)",
		"unknown column index":  "CREATE INDEX i ON t (missing)",
		"unbalanced parens":     "CREATE TABLE t (a int",
		"collate":               "CREATE TABLE t (a text COLLATE \"C\")",
		"include columns":       "CREATE TABLE t (a int); CREATE INDEX i ON t (a) INCLUDE (a)",
		"empty table":           "CREATE TABLE t ()",
		"unsupported option":    "CREATE TABLE t (a int) INHERITS (parent)",
		"check without parens":  "CREATE TABLE t (a int, CHECK a > 0)",
		"duplicate index":       "CREATE TABLE t (a int); CREATE INDEX i ON t (a); CREATE INDEX i ON t (a)",
		"unterminated literal":  "CREATE TABLE t (a text DEFAULT 'x)",
		"bad sequence type":     "CREATE SEQUENCE s AS real",
		"zero increment":        "CREATE SEQUENCE s INCREMENT 0",
		"expression index":      "CREATE TABLE t (a int); CREATE INDEX i ON t ((a + 1))",
		"generated without":     "CREATE TABLE t (a int GENERATED ALWAYS AS id)",
		"unsupported check":     "CREATE TABLE t (a int, CHECK (a > 0) NO INHERIT EXTRA)",
		"nulls not distinct":    "CREATE TABLE t (a int UNIQUE NULLS NOT DISTINCT)",
		"double constraint":     "CREATE TABLE t (a int, PRIMARY KEY (a), PRIMARY KEY (a))",
		"unknown statement":     "TRUNCATE t",
		"unbalanced identifier": `CREATE TABLE "t (a int`,
	}
	for name, statement := range cases {
		_, err := ParseDDL(statement)
		require.Error(t, err, name)
	}
}

func TestParseDDLSupportedConstructs(t *testing.T) {
	actual, err := ParseDDL(`
		CREATE TYPE mood AS ENUM ('sad', 'ok', 'happy');
		CREATE SEQUENCE counter START WITH 5 INCREMENT BY 2 MINVALUE 5 MAXVALUE 100 CYCLE;
		CREATE TABLE public.people (
			id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
			name character varying(120) NOT NULL,
			nickname varchar(30) UNIQUE,
			mood public.mood NOT NULL DEFAULT 'ok'::mood,
			bio text,
			score numeric(5,2) CHECK (score >= 0),
			created_at timestamp without time zone DEFAULT now(),
			updated_at timestamptz,
			total double precision,
			tags text[],
			CONSTRAINT people_pkey PRIMARY KEY (id),
			CONSTRAINT people_name_fkey FOREIGN KEY (name) REFERENCES public.people (name) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX people_nickname_idx ON public.people USING btree (nickname) WHERE nickname IS NOT NULL;
	`)
	require.NoError(t, err)

	people := namespaceOf(t, actual, "public")
	require.Len(t, people.Tables, 1)
	peopleTable := people.Tables[0]
	require.Equal(t, "tbl:public.people", peopleTable.ID)

	types := map[string]string{}
	for _, column := range peopleTable.Columns {
		types[column.Name] = column.Type
	}
	require.Equal(t, "int8", types["id"])
	require.Equal(t, "varchar(120)", types["name"])
	require.Equal(t, "numeric(5,2)", types["score"])
	require.Equal(t, "timestamp", types["created_at"])
	require.Equal(t, "timestamptz", types["updated_at"])
	require.Equal(t, "float8", types["total"])
	require.Equal(t, "text[]", types["tags"])
	require.Equal(t, "mood", types["mood"])

	identity := findColumn(peopleTable, "id")
	require.True(t, identity.IsIdentity)
	require.Equal(t, "'ok'::mood", findColumn(peopleTable, "mood").Default)
	require.Equal(t, "now()", findColumn(peopleTable, "created_at").Default)
	require.True(t, findColumn(peopleTable, "bio").Nullable)

	constraintNames := map[string]schema.Constraint{}
	for _, constraint := range peopleTable.Constraints {
		constraintNames[constraint.Name] = constraint
	}
	require.Contains(t, constraintNames, "people_pkey")
	require.Contains(t, constraintNames, "people_nickname_key")
	require.Contains(t, constraintNames, "people_name_fkey")
	require.Contains(t, constraintNames, "people_check")
	require.Equal(t, []string{"name"}, constraintNames["people_name_fkey"].Columns)
	require.Equal(t, "public.people", constraintNames["people_name_fkey"].ReferencedTable)
	require.Equal(t, []string{"name"}, constraintNames["people_name_fkey"].ReferencedCols)

	require.Len(t, peopleTable.Indexes, 1)
	require.Equal(t, "people_nickname_idx", peopleTable.Indexes[0].Name)
	require.True(t, peopleTable.Indexes[0].Unique)
	require.Equal(t, "nickname is not null", peopleTable.Indexes[0].Predicate)

	require.Equal(t, []string{"sad", "ok", "happy"}, people.Enums[0].Values)
	require.Equal(t, int64(5), people.Sequences[0].Start)
	require.Equal(t, int64(2), people.Sequences[0].Increment)
	require.True(t, people.Sequences[0].Cycle)
}

func TestParseDDLGeneratesConstraintNames(t *testing.T) {
	actual, err := ParseDDL(`
		CREATE TABLE public.metrics (
			id int,
			total int,
			CONSTRAINT metrics_total_check CHECK (total > 0),
			CHECK (total < 100),
			CHECK (total <> 42)
		);
	`)
	require.NoError(t, err)

	constraints := namespaceOf(t, actual, "public").Tables[0].Constraints
	names := []string{}
	for _, constraint := range constraints {
		names = append(names, constraint.Name)
	}
	require.Equal(t, []string{"metrics_check", "metrics_check1", "metrics_total_check"}, names)
}

func TestParseDDLEmptyScript(t *testing.T) {
	actual, err := ParseDDL("  -- only a comment\n/* and a block comment */\n")
	require.NoError(t, err)
	require.Empty(t, actual.Namespaces)
}

func namespaceOf(t *testing.T, value *schema.Schema, name string) schema.Namespace {
	t.Helper()
	for _, namespace := range value.Namespaces {
		if namespace.Name == name {
			return namespace
		}
	}
	t.Fatalf("namespace %s not found", name)
	return schema.Namespace{}
}

func findColumn(table schema.Table, name string) schema.Column {
	for _, column := range table.Columns {
		if column.Name == name {
			return column
		}
	}
	return schema.Column{}
}
