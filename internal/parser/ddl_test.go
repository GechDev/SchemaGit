package parser

import (
	"os"
	"testing"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

func TestParseDDLFixture(t *testing.T) {
	sql, err := os.ReadFile("../../testdata/fixtures/simple.sql")
	require.NoError(t, err)
	actual, err := ParseDDL(string(sql))
	require.NoError(t, err)

	expected := &schema.Schema{Namespaces: []schema.Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []schema.Table{{
			ID: "tbl:public.users", Name: "users",
			Columns: []schema.Column{
				{ID: "col:public.users.email", Name: "email", Ordinal: 3, Type: "text", Nullable: false, Default: "'unknown'::text"},
				{ID: "col:public.users.id", Name: "id", Ordinal: 1, Type: "int8", Nullable: false},
				{ID: "col:public.users.role", Name: "role", Ordinal: 2, Type: "user_role", Nullable: false},
			},
			Constraints: []schema.Constraint{
				{ID: "con:public.users.users_email_key", Name: "users_email_key", Type: "unique", Columns: []string{"email"}},
				{ID: "con:public.users.users_id_pk", Name: "users_id_pk", Type: "pk", Columns: []string{"id"}},
			},
			Indexes: []schema.Index{{ID: "idx:public.users.users_email_idx", Name: "users_email_idx", Columns: []string{"email"}, Method: "btree"}},
		}},
		Views:     []schema.View{{ID: "view:public.active_users", Name: "active_users", Definition: "SELECT id, email FROM users WHERE (email <> ''::text)"}},
		Enums:     []schema.Enum{{ID: "enum:public.user_role", Name: "user_role", Values: []string{"admin", "member"}}},
		Sequences: []schema.Sequence{{ID: "seq:public.user_id_seq", Name: "user_id_seq", DataType: "bigint", Start: 1, Increment: 1, MinValue: 1, MaxValue: 9223372036854775807}},
	}}}
	require.Equal(t, schema.Hash(expected), schema.Hash(actual))
}

func TestParseDDLRejectsUnsupportedStatement(t *testing.T) {
	_, err := ParseDDL("DROP TABLE users")
	require.Error(t, err)
}
