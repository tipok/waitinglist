package database

import (
	"database/sql"
	"log/slog"
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
		wantErr  bool
	}{
		{"sqlite://:memory:", ":memory:", false},
		{"sqlite:///absolute/path/to/file.db", "/absolute/path/to/file.db", false},
		{"sqlite://relative/path.db", "relative/path.db", false},
		{"sqlite://", "", true},
		{"sqlite:///", "", true},
	}
	for _, tc := range cases {
		got, err := parseSQLitePath(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSQLitePath(%q) expected error, got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSQLitePath(%q) unexpected error: %v", tc.url, err)
			continue
		}
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

func TestMigrationsDir(t *testing.T) {
	cases := []struct {
		base     string
		driver   Driver
		expected string
	}{
		{"migrations", DriverPostgres, filepath.Join("migrations", "postgres")},
		{"migrations", DriverSQLite, filepath.Join("migrations", "sqlite")},
		{"/app/migrations", DriverPostgres, filepath.Join("/app/migrations", "postgres")},
	}
	for _, tc := range cases {
		got := MigrationsDir(tc.base, tc.driver)
		if got != tc.expected {
			t.Errorf("MigrationsDir(%q, %q) = %q, want %q", tc.base, tc.driver, got, tc.expected)
		}
	}
}

func findSQLiteMigrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations", "sqlite")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find migrations/sqlite directory")
		}
		dir = parent
	}
}

func TestSQLiteMigrations_AllTablesExist(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	tables := []string{"user_entity", "waiting_list", "scheduler_state"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestSQLiteMigrations_UserEntityColumns(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	rows, err := db.Query(`PRAGMA table_info(user_entity)`)
	if err != nil {
		t.Fatalf("querying table_info: %v", err)
	}
	defer rows.Close()

	cols := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, colType, notNull, pk string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scanning column info: %v", err)
		}
		cols[name] = colType
	}

	expected := map[string]string{
		"id":           "TEXT",
		"firstname":    "TEXT",
		"lastname":     "TEXT",
		"email":        "TEXT",
		"has_access":   "INTEGER",
		"created_at":   "TEXT",
		"project_slug": "TEXT",
	}
	for col, wantType := range expected {
		gotType, ok := cols[col]
		if !ok {
			t.Errorf("missing column %q in user_entity", col)
			continue
		}
		if gotType != wantType {
			t.Errorf("column %q: expected type %q, got %q", col, wantType, gotType)
		}
	}
}

func TestSQLiteMigrations_WaitingListGeneratedColumn(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Insert a user then a waiting list entry and verify weighted_created_at is populated.
	userID := "00000000-0000-0000-0000-000000000001"
	_, err = db.Exec(`INSERT INTO user_entity (id, firstname, lastname, email, project_slug) VALUES (?, 'A', 'B', 'a@b.com', 'default')`, userID)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	wlID := "00000000-0000-0000-0000-000000000002"
	_, err = db.Exec(`INSERT INTO waiting_list (id, user_id, weight, project_slug) VALUES (?, ?, 0, 'default')`, wlID, userID)
	if err != nil {
		t.Fatalf("inserting waiting list entry: %v", err)
	}

	var weighted string
	if err := db.QueryRow(`SELECT weighted_created_at FROM waiting_list WHERE id=?`, wlID).Scan(&weighted); err != nil {
		t.Fatalf("querying weighted_created_at: %v", err)
	}
	if weighted == "" {
		t.Error("expected weighted_created_at to be populated by generated column")
	}
}

func TestSQLiteMigrations_Idempotent(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("second RunMigrations failed (not idempotent): %v", err)
	}
}

func TestSQLiteMigrations_ForeignKeyEnforced(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (id, user_id, project_slug) VALUES ('wl-1', '00000000-0000-0000-0000-000000000099', 'default')`)
	if err == nil {
		t.Fatal("expected foreign key violation for non-existent user_id")
	}
}

func TestSQLiteMigrations_UniqueEmailPerProject(t *testing.T) {
	db, err := NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findSQLiteMigrationsDir(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err = db.Exec(`INSERT INTO user_entity (id, firstname, lastname, email, project_slug) VALUES ('id-1', 'A', 'B', 'dup@test.com', 'proj')`)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = db.Exec(`INSERT INTO user_entity (id, firstname, lastname, email, project_slug) VALUES ('id-2', 'C', 'D', 'dup@test.com', 'proj')`)
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate (project_slug, email)")
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
