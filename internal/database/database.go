package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// Driver identifies the database backend in use.
type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverSQLite   Driver = "sqlite"
)

// MigrationsDir returns the driver-specific migrations subdirectory rooted at
// baseDir (e.g. "migrations/postgres" or "migrations/sqlite").
func MigrationsDir(baseDir string, driver Driver) string {
	return filepath.Join(baseDir, string(driver))
}

// New opens a database connection by auto-detecting the driver from the URL scheme.
// "postgres://" uses PostgreSQL (github.com/lib/pq).
// "sqlite://" uses SQLite (modernc.org/sqlite).
func New(databaseURL string) (*sql.DB, Driver, error) {
	switch {
	case strings.HasPrefix(databaseURL, "postgres://"), strings.HasPrefix(databaseURL, "postgresql://"):
		db, err := NewPostgresDB(databaseURL)
		if err != nil {
			return nil, "", fmt.Errorf("postgres: %w", err)
		}
		return db, DriverPostgres, nil

	case strings.HasPrefix(databaseURL, "sqlite://"):
		path := parseSQLitePath(databaseURL)
		db, err := NewSQLiteDB(path)
		if err != nil {
			return nil, "", fmt.Errorf("sqlite: %w", err)
		}
		return db, DriverSQLite, nil

	default:
		return nil, "", fmt.Errorf("unsupported database URL scheme: %q", databaseURL)
	}
}

// parseSQLitePath extracts the file path from a sqlite:// URL.
// sqlite:///absolute/path → /absolute/path
// sqlite://relative/path  → relative/path
// sqlite://:memory:       → :memory:
func parseSQLitePath(rawURL string) string {
	// Strip the "sqlite://" prefix.
	rest := strings.TrimPrefix(rawURL, "sqlite://")
	// An absolute path is encoded as sqlite:///path, which after stripping gives /path.
	// A relative path is encoded as sqlite://rel/path, which gives rel/path.
	// :memory: is encoded as sqlite://:memory:, which gives :memory:.
	return rest
}
