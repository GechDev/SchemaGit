package schema

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalJSONRoundTrip(t *testing.T) {
	original := &Schema{Namespaces: []Namespace{{
		ID:   "ns:public",
		Name: "public",
		Tables: []Table{{
			ID:   "tbl:public.users",
			Name: "users",
			Columns: []Column{{
				ID: "col:public.users.email", Name: "email", Ordinal: 2,
				Type: "text", Nullable: false,
			}, {
				ID: "col:public.users.id", Name: "id", Ordinal: 1,
				Type: "bigint", Nullable: false,
			}},
		}},
	}}}

	canonical, err := CanonicalJSON(original)
	require.NoError(t, err)
	var decoded Schema
	require.NoError(t, json.Unmarshal(canonical, &decoded))
	encodedAgain, err := CanonicalJSON(&decoded)
	require.NoError(t, err)
	require.JSONEq(t, string(canonical), string(encodedAgain))
	require.Equal(t, "col:public.users.email", original.Namespaces[0].Tables[0].Columns[0].ID)
}

func TestHashStableAcrossNodeOrdering(t *testing.T) {
	first := &Schema{Namespaces: []Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []Table{{ID: "tbl:public.z", Name: "z"}, {ID: "tbl:public.a", Name: "a"}},
	}}}
	second := &Schema{Namespaces: []Namespace{{
		ID: "ns:public", Name: "public",
		Tables: []Table{{ID: "tbl:public.a", Name: "a"}, {ID: "tbl:public.z", Name: "z"}},
	}}}

	require.NotEmpty(t, Hash(first))
	require.Equal(t, Hash(first), Hash(second))
}

func TestValidateModel(t *testing.T) {
	valid := &Schema{Namespaces: []Namespace{{ID: "ns:public", Name: "public"}}}
	require.NoError(t, Validate(valid))

	invalid := &Schema{Namespaces: []Namespace{{ID: "ns:public", Name: "public", Tables: []Table{{
		ID: "tbl:public.users", Name: "users", Columns: []Column{{
			ID: "col:public.users.id", Name: "id", Ordinal: 1,
		}},
	}}}}}
	require.Error(t, Validate(invalid))
}

func TestPublishedSchemaMatchesEmbeddedSchema(t *testing.T) {
	data, err := os.ReadFile("../../schemagit.schema.json")
	require.NoError(t, err)
	require.JSONEq(t, modelSchema, string(data))
}
