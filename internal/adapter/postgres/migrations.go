package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/schemagit/schemagit/internal/introspect"
)

// migrationTableSQL creates the bookkeeping table used to record applied
// migration ids. It is idempotent so `init` and `migrate` can both call it.
const migrationTableSQL = `CREATE TABLE IF NOT EXISTS ` + migrationTableName + ` (
    migration_id text PRIMARY KEY,
    applied_at   timestamptz NOT NULL DEFAULT now()
)`

// readApplied lists the applied migration ids in ascending order. A missing
// bookkeeping table is reported as introspect.ErrNoMigrationTable.
func readApplied(ctx context.Context, connection *pgx.Conn) ([]string, error) {
	rows, err := connection.Query(ctx,
		`SELECT migration_id FROM `+migrationTableName+` ORDER BY migration_id`)
	if err != nil {
		if isNoSuchTable(err) {
			return nil, introspect.ErrNoMigrationTable
		}
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return ids, nil
}

func trimSQL(sql string) string {
	return strings.TrimSpace(sql)
}

func toLowerASCII(value string) string {
	out := []byte(value)
	for index, character := range out {
		if character >= 'A' && character <= 'Z' {
			out[index] = character + ('a' - 'A')
		}
	}
	return string(out)
}

// hasPrefixWord reports whether value starts with prefix on a word boundary so
// that `drop database` never matches `drop database_user_view`.
func hasPrefixWord(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	last := prefix[len(prefix)-1]
	if !isWordByte(last) {
		return true
	}
	return len(value) == len(prefix) || !isWordByte(value[len(prefix)])
}

func isWordByte(character byte) bool {
	return character == '_' ||
		(character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}
