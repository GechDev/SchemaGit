// Package postgres implements the SchemaGit database adapter for PostgreSQL by
// reading pg_catalog. It is the only package in the repository that imports a
// PostgreSQL driver.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/schemagit/schemagit/internal/introspect"
	"github.com/schemagit/schemagit/internal/schema"
)

// Adapter introspects and migrates a single PostgreSQL database.
type Adapter struct {
	// connectTimeout bounds a single connection attempt.
	connectTimeout time.Duration
}

// New returns a PostgreSQL adapter with default timeouts.
func New() introspect.Adapter {
	return &Adapter{connectTimeout: 15 * time.Second}
}

// connect opens a single connection to url.
func (a *Adapter) connect(ctx context.Context, url string) (*pgx.Conn, error) {
	timeout := a.connectTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connection, err := pgx.Connect(dialCtx, url)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return connection, nil
}

// Introspect reads the live schema from pg_catalog.
func (a *Adapter) Introspect(ctx context.Context, url string) (*schema.Schema, error) {
	connection, err := a.connect(ctx, url)
	if err != nil {
		return nil, err
	}
	defer connection.Close(ctx)

	if err := connection.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	version := connection.Config().RuntimeParams["server_version"]
	result := &schema.Schema{Namespaces: []schema.Namespace{}}
	namespaces := map[string]*schema.Namespace{}
	namespace := func(name string) *schema.Namespace {
		if name == "" {
			name = "public"
		}
		if existing, ok := namespaces[name]; ok {
			return existing
		}
		created := &schema.Namespace{ID: "ns:" + name, Name: name}
		namespaces[name] = created
		return created
	}

	tables, err := a.readTables(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("read tables: %w (server %s)", err, version)
	}
	if err := a.readEnums(ctx, connection, namespace); err != nil {
		return nil, fmt.Errorf("read enums: %w", err)
	}
	if err := a.readSequences(ctx, connection, namespace); err != nil {
		return nil, fmt.Errorf("read sequences: %w", err)
	}
	if err := a.readViews(ctx, connection, namespace); err != nil {
		return nil, fmt.Errorf("read views: %w", err)
	}
	if err := a.readFunctions(ctx, connection, namespace); err != nil {
		return nil, fmt.Errorf("read functions: %w", err)
	}
	tables.attach(namespace)

	for _, value := range namespaces {
		result.Namespaces = append(result.Namespaces, *value)
	}
	sortNamespaces(result)
	for index := range result.Namespaces {
		sortNamespaceContents(&result.Namespaces[index])
	}
	if err := resolveViewDependencies(result); err != nil {
		return nil, err
	}
	if err := schema.Validate(result); err != nil {
		return nil, fmt.Errorf("introspected schema is invalid: %w", err)
	}
	return result, nil
}

// AppliedMigrations lists the migration ids recorded in the database.
func (a *Adapter) AppliedMigrations(ctx context.Context, url string) ([]string, error) {
	connection, err := a.connect(ctx, url)
	if err != nil {
		return nil, err
	}
	defer connection.Close(ctx)
	ids, err := readApplied(ctx, connection)
	if errors.Is(err, introspect.ErrNoMigrationTable) {
		return nil, introspect.ErrNoMigrationTable
	}
	return ids, err
}

// EnsureMigrationTable creates the bookkeeping table when it is missing.
func (a *Adapter) EnsureMigrationTable(ctx context.Context, url string) error {
	connection, err := a.connect(ctx, url)
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	if _, err := connection.Exec(ctx, migrationTableSQL); err != nil {
		return fmt.Errorf("create %s: %w", migrationTableName, err)
	}
	return nil
}

// Apply executes SQL against the database in a single transaction.
func (a *Adapter) Apply(ctx context.Context, url string, sql string) error {
	if err := validateSQL(sql); err != nil {
		return err
	}
	connection, err := a.connect(ctx, url)
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	if _, err := connection.Exec(ctx, sql); err != nil {
		return fmt.Errorf("apply SQL: %w", err)
	}
	return nil
}

// validateSQL refuses obviously dangerous or empty statements so the adapter
// never executes a no-op or a client side meta command by accident.
func validateSQL(sql string) error {
	trimmed := trimSQL(sql)
	if trimmed == "" {
		return fmt.Errorf("refusing to apply empty SQL")
	}
	lowered := toLowerASCII(trimmed)
	for _, prefix := range []string{"\\", "copy ", "create database", "drop database"} {
		if hasPrefixWord(lowered, prefix) {
			return fmt.Errorf("refusing to apply a %s statement", trimSQL(prefix))
		}
	}
	return nil
}

func isNoSuchTable(err error) bool {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return pgError.Code == "42P01"
	}
	return false
}
