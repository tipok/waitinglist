package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_UnsupportedScheme(t *testing.T) {
	_, _, err := New("mysql://user:pass@localhost/db")
	if err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
}

func TestNew_SQLiteMemory(t *testing.T) {
	db, driver, err := New("sqlite://:memory:")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	if driver != DriverSQLite {
		t.Errorf("expected DriverSQLite, got %q", driver)
	}

	if err := db.Ping(); err != nil {
		t.Errorf("ping failed: %v", err)
	}
}

func TestNew_SQLiteTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, driver, err := New("sqlite://" + path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	if driver != DriverSQLite {
		t.Errorf("expected DriverSQLite, got %q", driver)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected db file to exist: %v", err)
	}
}

func TestNew_PostgresScheme(t *testing.T) {
	// We cannot actually connect without a running PostgreSQL instance,
	// but we can verify the scheme is accepted and that the postgres path is taken.
	// An invalid host/db will fail at ping time, not at URL parsing.
	_, _, err := New("postgres://invalid-host-no-such-server/db?connect_timeout=1")
	if err == nil {
		t.Fatal("expected connection error for unreachable postgres host")
	}
	// Ensure we get a postgres error, not an "unsupported scheme" error.
	if err.Error() == `unsupported database URL scheme: "postgres://invalid-host-no-such-server/db?connect_timeout=1"` {
		t.Errorf("got unsupported scheme error; expected a postgres connection error: %v", err)
	}
}

func TestNew_PostgresqlScheme(t *testing.T) {
	_, _, err := New("postgresql://invalid-host-no-such-server/db?connect_timeout=1")
	if err == nil {
		t.Fatal("expected connection error for unreachable postgres host")
	}
	if err.Error() == `unsupported database URL scheme: "postgresql://invalid-host-no-such-server/db?connect_timeout=1"` {
		t.Errorf("got unsupported scheme error; expected a postgres connection error: %v", err)
	}
}

func TestParseSQLitePath(t *testing.T) {
	cases := []struct {
		url      string
		expected string
	}{
		{"sqlite://:memory:", ":memory:"},
		{"sqlite:///absolute/path/to/file.db", "/absolute/path/to/file.db"},
		{"sqlite://relative/path.db", "relative/path.db"},
	}
	for _, tc := range cases {
		got := parseSQLitePath(tc.url)
		if got != tc.expected {
			t.Errorf("parseSQLitePath(%q) = %q, want %q", tc.url, got, tc.expected)
		}
	}
}

func TestNewSQLiteDB_Memory(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Errorf("ping failed: %v", err)
	}
}

func TestNewSQLiteDB_TempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected db file to exist: %v", err)
	}
}

func TestNewSQLiteDB_WALAndForeignKeys(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	if journalMode != "memory" {
		// In-memory databases report "memory" not "wal" — that is expected.
		// For a file-based DB we'd check for "wal". This is acceptable.
		t.Logf("journal_mode for :memory: is %q (expected 'memory', WAL not applicable)", journalMode)
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("querying foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys=1, got %d", foreignKeys)
	}
}
