package postgres

import (
	"testing"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

func TestQualified(t *testing.T) {
	require.Equal(t, "public.users", qualified("", "users"))
	require.Equal(t, "app.users", qualified("app", "users"))
}

func TestValidateSQLRefusesUnsafeInput(t *testing.T) {
	require.Error(t, validateSQL("   "))
	require.Error(t, validateSQL("\\connect other"))
	require.Error(t, validateSQL("COPY users FROM STDIN"))
	require.Error(t, validateSQL("create database other"))
	require.Error(t, validateSQL("drop database other"))
	require.NoError(t, validateSQL("CREATE TABLE public.users (id bigint);"))
	require.NoError(t, validateSQL("drop table if exists public.users;"))
}

func TestHasPrefixWord(t *testing.T) {
	require.True(t, hasPrefixWord("copy users from stdin", "copy "))
	require.False(t, hasPrefixWord("copy_user from stdin", "copy "))
	require.True(t, hasPrefixWord("drop database x", "drop database"))
	require.False(t, hasPrefixWord("drop database_x", "drop database"))
	require.True(t, hasPrefixWord("create database;", "create database"))
}

func TestSplitArguments(t *testing.T) {
	require.Equal(t, []string{}, splitArguments(""))
	require.Equal(t, []string{"integer"}, splitArguments("integer"))
	require.Equal(t, []string{"integer", "text"}, splitArguments("integer, text"))
	require.Equal(t, []string{"numeric(10, 2)", "text[]"}, splitArguments("numeric(10, 2), text[]"))
}

func TestNonNil(t *testing.T) {
	require.Equal(t, []string{}, nonNil(nil))
	require.Equal(t, []string{"a"}, nonNil([]string{"a"}))
}

func TestUniqueSortedStrings(t *testing.T) {
	require.Equal(t, []string{"a", "b", "c"}, uniqueSortedStrings([]string{"c", "b", "a", "b"}))
	require.Equal(t, []string{}, uniqueSortedStrings(nil))
}

func TestResolveViewDependencies(t *testing.T) {
	result := &schema.Schema{Namespaces: []schema.Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []schema.Table{{ID: "tbl:public.users", Name: "users"}},
		Views: []schema.View{{
			ID: "view:public.active", Name: "active",
			Definition: "select id from users",
		}, {
			ID: "view:public.all", Name: "all",
			Definition: "select id from active",
		}, {
			ID: "view:public.constant", Name: "constant",
			Definition: "select 1",
		}},
	}}}
	require.NoError(t, resolveViewDependencies(result))
	views := result.Namespaces[0].Views
	require.Equal(t, []string{"tbl:public.users"}, views[0].DependsOn)
	require.Equal(t, []string{"view:public.active"}, views[1].DependsOn)
	require.Equal(t, []string{}, views[2].DependsOn)
}

func TestSortNamespaceContentsDensifiesOrdinals(t *testing.T) {
	result := &schema.Schema{Namespaces: []schema.Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []schema.Table{{
			ID: "tbl:public.users", Name: "users",
			Columns: []schema.Column{
				{ID: "col:public.users.id", Name: "id", Ordinal: 3, Type: "int8"},
				{ID: "col:public.users.email", Name: "email", Ordinal: 9, Type: "text"},
			},
			Constraints: []schema.Constraint{
				{ID: "con:public.users.b", Name: "b", Type: "pk"},
				{ID: "con:public.users.a", Name: "a", Type: "unique"},
			},
			Indexes: []schema.Index{
				{ID: "idx:public.users.b", Name: "b"},
				{ID: "idx:public.users.a", Name: "a"},
			},
		}},
	}}}
	sortNamespaces(result)
	sortNamespaceContents(&result.Namespaces[0])
	table := result.Namespaces[0].Tables[0]
	require.Equal(t, "col:public.users.email", table.Columns[0].ID)
	require.Equal(t, 1, table.Columns[0].Ordinal)
	require.Equal(t, 2, table.Columns[1].Ordinal)
	require.Equal(t, "con:public.users.a", table.Constraints[0].ID)
	require.Equal(t, "idx:public.users.a", table.Indexes[0].ID)
}

func TestNewReturnsAdapter(t *testing.T) {
	adapter := New()
	require.NotNil(t, adapter)
	_, err := adapter.Introspect(t.Context(), "postgres://127.0.0.1:1/none")
	require.Error(t, err)
	require.Contains(t, err.Error(), "connect to PostgreSQL")
}
