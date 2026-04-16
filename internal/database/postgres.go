package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/lib/pq"
)

// NewPostgresDB opens a connection to the PostgreSQL database using the given
// connection URL and verifies connectivity with a ping.
func NewPostgresDB(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening database connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		err := db.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return db, nil
}

// RunMigrations reads all .sql files from the given directory in alphabetical
// order and executes them against the database.
func RunMigrations(db *sql.DB, migrationsDir string, logger *slog.Logger) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	var sqlFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".sql" {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}

	sort.Strings(sqlFiles)

	for _, filename := range sqlFiles {
		path := filepath.Join(migrationsDir, filename)

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", filename, err)
		}

		logger.Info("Running migration", "file", filename)

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("executing migration %s: %w", filename, err)
		}
	}

	return nil
}
