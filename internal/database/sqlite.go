package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// NewSQLiteDB opens a SQLite database at the given path (or ":memory:" for in-memory),
// enables WAL journal mode, foreign keys, and sets connection limits appropriate for SQLite.
func NewSQLiteDB(path string) (*sql.DB, error) {
	// Embed PRAGMAs in the DSN so they are applied to every new connection the
	// pool creates, not just the initial one. This matters because database/sql
	// may discard and recreate a connection after certain errors, and per-connection
	// settings like foreign_keys would be lost without DSN-level configuration.
	dsn := buildSQLiteDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite does not support concurrent writers; limit to one open connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	return db, nil
}

// buildSQLiteDSN appends _pragma DSN parameters so that foreign_keys, WAL mode,
// and busy_timeout are applied on every new driver connection.
func buildSQLiteDSN(path string) string {
	if path == ":memory:" {
		// In-memory databases use a plain path; WAL is not applicable.
		return path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	// For file paths that already use file: URI syntax, append pragmas using &
	// if a query string is already present, or ? otherwise.
	if strings.HasPrefix(path, "file:") {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return path + sep + "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	// Plain file path — wrap in file: URI so driver processes the query parameters.
	return "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
}
