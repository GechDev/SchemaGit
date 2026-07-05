package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/schemagit/schemagit/internal/parser"
	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

// databaseURLEnv points at a throwaway PostgreSQL 15 database. The test is
// skipped when it is absent so the suite stays runnable offline.
const databaseURLEnv = "SCHEMAGIT_TEST_DATABASE_URL"

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(databaseURLEnv)
	if url == "" {
		t.Skipf("set %s to run the PostgreSQL integration test", databaseURLEnv)
	}
	return url
}

// TestIntrospectMatchesParser is the Checkpoint 3 acceptance test: DDL is
// applied to a live database, introspected through pg_catalog, and the result
// must hash identically to the same DDL parsed from text.
func TestIntrospectMatchesParser(t *testing.T) {
	url := testDatabaseURL(t)
	ddl, err := os.ReadFile("../../../testdata/fixtures/simple.sql")
	require.NoError(t, err)

	expected, err := parser.ParseDDL(string(ddl))
	require.NoError(t, err)
	expectedHash := schema.Hash(expected)

	admin, err := pgx.Connect(context.Background(), url)
	require.NoError(t, err)
	defer admin.Close(context.Background())
	resetTestSchema(t, admin)
	_, err = admin.Exec(context.Background(), string(ddl))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adapter := &Adapter{}
	actual, err := adapter.Introspect(ctx, url)
	require.NoError(t, err)

	actualHash := schema.Hash(actual)
	if os.Getenv("SCHEMAGIT_TEST_DUMP") != "" {
		canonical, err := schema.CanonicalJSON(actual)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile("../../../testdata/fixtures/catalog_output.json", canonical, 0o644))
		t.Log("wrote testdata/fixtures/catalog_output.json")
	}
	require.Equal(t, expectedHash, actualHash,
		"introspection and DDL parsing disagree; set SCHEMAGIT_TEST_DUMP=1 to write testdata/fixtures/catalog_output.json and compare")
}

// TestAppliedMigrationsLifecycle covers the bookkeeping table used to track
// applied migrations.
func TestAppliedMigrationsLifecycle(t *testing.T) {
	url := testDatabaseURL(t)
	admin, err := pgx.Connect(context.Background(), url)
	require.NoError(t, err)
	defer admin.Close(context.Background())
	resetTestSchema(t, admin)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adapter := &Adapter{}
	_, err = adapter.AppliedMigrations(ctx, url)
	require.Error(t, err, "missing bookkeeping table must be reported as an error")

	require.NoError(t, adapter.EnsureMigrationTable(ctx, url))
	ids, err := adapter.AppliedMigrations(ctx, url)
	require.NoError(t, err)
	require.Equal(t, []string{}, ids)
}

// resetTestSchema drops every non-system object so repeated runs are stable.
func resetTestSchema(t *testing.T, connection *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	_, err := connection.Exec(ctx, `
		DO $$
		DECLARE object record;
		BEGIN
			FOR object IN
				SELECT n.nspname, c.relname, c.relkind
				FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname NOT LIKE 'pg_%'
				  AND n.nspname <> 'information_schema'
			LOOP
				IF object.relkind IN ('r', 'p', 'v', 'm', 'S', 'f') THEN
					EXECUTE format('DROP %s IF EXISTS %I.%I CASCADE',
						CASE object.relkind
							WHEN 'v' THEN 'VIEW'
							WHEN 'm' THEN 'MATERIALIZED VIEW'
							WHEN 'S' THEN 'SEQUENCE'
							WHEN 'f' THEN 'FUNCTION'
							ELSE 'TABLE'
						END, object.nspname, object.relname);
				END IF;
			END LOOP;
			FOR object IN
				SELECT n.nspname, t.typname
				FROM pg_type t
				JOIN pg_namespace n ON n.oid = t.typnamespace
				WHERE t.typtype = 'e'
				  AND n.nspname NOT LIKE 'pg_%'
				  AND n.nspname <> 'information_schema'
			LOOP
				EXECUTE format('DROP TYPE IF EXISTS %I.%I CASCADE', object.nspname, object.typname);
			END LOOP;
		END $$;`)
	require.NoError(t, err)
}
