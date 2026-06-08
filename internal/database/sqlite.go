package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// NewSQLiteDB opens a SQLite database at the given path (or ":memory:" for in-memory),
// enables WAL journal mode, foreign keys, and sets connection limits appropriate for SQLite.
func NewSQLiteDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite does not support concurrent writers; limit to one open connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	return db, nil
}
