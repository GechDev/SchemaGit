// Package introspect defines the database adapter contract used by SchemaGit.
// Core packages depend on this interface only; concrete adapters live under
// internal/adapter and are wired up by the CLI, which keeps the core free of
// any PostgreSQL driver import.
package introspect

import (
	"context"
	"errors"

	"github.com/schemagit/schemagit/internal/schema"
)

// Adapter is the contract every database backend must satisfy.
type Adapter interface {
	// Introspect reads the live database schema at url.
	Introspect(ctx context.Context, url string) (*schema.Schema, error)
	// Apply executes SQL against the database at url.
	Apply(ctx context.Context, url string, sql string) error
	// AppliedMigrations lists the migration ids recorded in the database.
	AppliedMigrations(ctx context.Context, url string) ([]string, error)
}

// ErrNoMigrationTable is returned when the bookkeeping table is missing, which
// means the database has never been migrated by SchemaGit.
var ErrNoMigrationTable = errors.New("migration bookkeeping table is missing")

// Factory builds a concrete adapter. The CLI registers the PostgreSQL adapter
// so that core packages never import a driver.
type Factory func() Adapter

var registered Factory

// Register installs the adapter factory used by New.
func Register(factory Factory) {
	registered = factory
}

// New returns the registered adapter.
func New() (Adapter, error) {
	if registered == nil {
		return nil, errors.New("no database adapter registered")
	}
	return registered(), nil
}
