package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Migration is a pair of SQL files generated from a schema transition.
type Migration struct {
	ID       string `json:"id"`
	UpSQL    string `json:"up_sql"`
	DownSQL  string `json:"down_sql"`
	Checksum string `json:"checksum"`
}

// WriteMigration stores the up and down SQL for a migration. The checksum is the
// SHA-256 of the up SQL and is stored so a later apply can detect tampering.
func (s *Store) WriteMigration(id string, up, down string) (*Migration, error) {
	if err := validMigrationID(id); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(up))
	migration := &Migration{ID: id, UpSQL: up, DownSQL: down, Checksum: hex.EncodeToString(digest[:])}
	if err := atomicWrite(s.migrationPath(id), []byte(up)); err != nil {
		return nil, fmt.Errorf("write migration: %w", err)
	}
	if err := atomicWrite(s.downMigrationPath(id), []byte(down)); err != nil {
		return nil, fmt.Errorf("write down migration: %w", err)
	}
	return migration, nil
}

// ReadMigration loads a stored migration.
func (s *Store) ReadMigration(id string) (*Migration, error) {
	if err := validMigrationID(id); err != nil {
		return nil, err
	}
	up, err := os.ReadFile(s.migrationPath(id))
	if err != nil {
		return nil, fmt.Errorf("read migration: %w", err)
	}
	down, err := os.ReadFile(s.downMigrationPath(id))
	if err != nil {
		return nil, fmt.Errorf("read down migration: %w", err)
	}
	digest := sha256.Sum256(up)
	migration := &Migration{
		ID:       id,
		UpSQL:    string(up),
		DownSQL:  string(down),
		Checksum: hex.EncodeToString(digest[:]),
	}
	return migration, nil
}

// ListMigrations returns every stored migration id in ascending order.
func (s *Store) ListMigrations() ([]string, error) {
	root := filepath.Join(s.root, "migrations")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("list migrations: %w", err)
		}
		for _, file := range files {
			name := file.Name()
			if strings.HasSuffix(name, ".down.sql") {
				continue
			}
			if !strings.HasSuffix(name, ".sql") {
				continue
			}
			ids = append(ids, entry.Name()+strings.TrimSuffix(name, ".sql"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) migrationPath(id string) string {
	return filepath.Join(s.path("migrations", id[:2]), id[2:]+".sql")
}

func (s *Store) downMigrationPath(id string) string {
	return filepath.Join(s.path("migrations", id[:2]), id[2:]+".down.sql")
}

// validMigrationID accepts 64 character hex identifiers, the same shape as
// commit hashes, so migration ids can be derived from a commit.
func validMigrationID(id string) error {
	if !validHash(id) {
		return fmt.Errorf("invalid migration id %q", id)
	}
	return nil
}
