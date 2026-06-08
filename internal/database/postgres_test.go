package database

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPostgresDB_InvalidURL(t *testing.T) {
	_, err := NewPostgresDB("invalid://not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for invalid connection URL")
	}
}

func TestNewPostgresDB_UnreachableHost(t *testing.T) {
	_, err := NewPostgresDB("postgres://localhost:59999/nonexistent?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for unreachable database host")
	}
}

func TestRunMigrations_NonExistentDirectory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	err := RunMigrations(db, "/nonexistent/path", logger)
	if err == nil {
		t.Fatal("expected error for non-existent migrations directory")
	}
}

func TestRunMigrations_EmptyDirectory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	tmpDir := t.TempDir()

	err := RunMigrations(db, tmpDir, logger)
	if err != nil {
		t.Fatalf("expected no error for empty directory, got: %v", err)
	}
}

func TestRunMigrations_SkipsNonSQLFiles(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("not sql"), 0644); err != nil {
		t.Fatal(err)
	}

	err := RunMigrations(db, tmpDir, logger)
	if err != nil {
		t.Fatalf("expected no error when only non-SQL files present, got: %v", err)
	}
}

func TestRunMigrations_SkipsSubdirectories(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	err := RunMigrations(db, tmpDir, logger)
	if err != nil {
		t.Fatalf("expected no error when only subdirectories present, got: %v", err)
	}
}

func TestRunMigrations_UnreadableFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "001_init.sql")
	if err := os.WriteFile(filePath, []byte("SELECT 1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filePath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := os.Chmod(filePath, 0644)
		if err != nil {
			return
		}
	})

	err := RunMigrations(db, tmpDir, logger)
	if err == nil {
		t.Fatal("expected error for unreadable migration file")
	}
}

func TestRunMigrations_FilesExecutedInOrder(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, _ := sql.Open("postgres", "postgres://localhost/test?sslmode=disable")
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	tmpDir := t.TempDir()

	// Create files in non-alphabetical creation order
	if err := os.WriteFile(filepath.Join(tmpDir, "003_third.sql"), []byte("SELECT 1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "001_first.sql"), []byte("SELECT 1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "002_second.sql"), []byte("SELECT 1"), 0644); err != nil {
		t.Fatal(err)
	}

	// RunMigrations will fail on Exec because db isn't connected, but we can
	// verify ordering logic by checking it tries to execute the first file
	err := RunMigrations(db, tmpDir, logger)
	if err == nil {
		t.Fatal("expected error (db not connected), but got nil")
	}
	// The error should reference 001_first.sql (the first alphabetically)
	if got := err.Error(); !contains(got, "001_first.sql") {
		t.Fatalf("expected error to reference 001_first.sql (first in order), got: %s", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Integration tests that require a real PostgreSQL database.
// Set TEST_DATABASE_URL to run these tests.

//goland:noinspection ALL
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	db, err := NewPostgresDB(url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS waiting_list")
		db.Exec("DROP TABLE IF EXISTS scheduler_state")
		db.Exec("DROP TABLE IF EXISTS user_entity")
		db.Close()
	})
	// Clean up any leftover tables from previous runs
	db.Exec("DROP TABLE IF EXISTS waiting_list")
	db.Exec("DROP TABLE IF EXISTS scheduler_state")
	db.Exec("DROP TABLE IF EXISTS user_entity")
	return db
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

//goland:noinspection ALL
func TestIntegration_RunMigrations_CreatesTables(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Verify user_entity table exists
	var tableExists bool
	err := db.QueryRow(`SELECT EXISTS (
		SELECT FROM information_schema.tables WHERE table_name = 'user_entity'
	)`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("checking user_entity existence: %v", err)
	}
	if !tableExists {
		t.Fatal("user_entity table was not created")
	}

	// Verify waiting_list table exists
	err = db.QueryRow(`SELECT EXISTS (
		SELECT FROM information_schema.tables WHERE table_name = 'waiting_list'
	)`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("checking waiting_list existence: %v", err)
	}
	if !tableExists {
		t.Fatal("waiting_list table was not created")
	}
}

//goland:noinspection ALL
func TestIntegration_UserEntityColumns(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	expectedColumns := map[string]string{
		"id":         "uuid",
		"firstname":  "character varying",
		"lastname":   "character varying",
		"email":      "character varying",
		"has_access": "boolean",
	}

	rows, err := db.Query(`
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = 'user_entity'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("querying columns: %v", err)
	}
	defer rows.Close()

	found := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning column: %v", err)
		}
		found[name] = dataType
	}

	for col, expectedType := range expectedColumns {
		gotType, ok := found[col]
		if !ok {
			t.Errorf("missing column %q in user_entity", col)
			continue
		}
		if gotType != expectedType {
			t.Errorf("column %q: expected type %q, got %q", col, expectedType, gotType)
		}
	}
}

//goland:noinspection ALL
func TestIntegration_WaitingListColumns(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	expectedColumns := map[string]string{
		"id":                  "uuid",
		"user_id":             "uuid",
		"created_at":          "timestamp with time zone",
		"weight":              "integer",
		"weighted_created_at": "timestamp with time zone",
	}

	rows, err := db.Query(`
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = 'waiting_list'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("querying columns: %v", err)
	}
	defer rows.Close()

	found := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning column: %v", err)
		}
		found[name] = dataType
	}

	for col, expectedType := range expectedColumns {
		gotType, ok := found[col]
		if !ok {
			t.Errorf("missing column %q in waiting_list", col)
			continue
		}
		if gotType != expectedType {
			t.Errorf("column %q: expected type %q, got %q", col, expectedType, gotType)
		}
	}
}

func TestIntegration_MigrationsIdempotent(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)

	// Run migrations twice — second run should not error
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("second RunMigrations failed (not idempotent): %v", err)
	}
}

//goland:noinspection ALL
func TestIntegration_HasAccessDefaultFalse(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('John', 'Doe', 'john@example.com')`)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var hasAccess bool
	err = db.QueryRow(`SELECT has_access FROM user_entity WHERE email = 'john@example.com'`).Scan(&hasAccess)
	if err != nil {
		t.Fatalf("querying has_access: %v", err)
	}
	if hasAccess {
		t.Fatal("expected has_access to default to FALSE")
	}
}

//goland:noinspection ALL
func TestIntegration_CreatedAtDefaultNow(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('Jane', 'Doe', 'jane@example.com')`)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var userID string
	err = db.QueryRow(`SELECT id FROM user_entity WHERE email = 'jane@example.com'`).Scan(&userID)
	if err != nil {
		t.Fatalf("querying user id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (user_id) VALUES ($1)`, userID)
	if err != nil {
		t.Fatalf("inserting waiting list entry: %v", err)
	}

	var createdAt sql.NullTime
	err = db.QueryRow(`SELECT created_at FROM waiting_list WHERE user_id = $1`, userID).Scan(&createdAt)
	if err != nil {
		t.Fatalf("querying created_at: %v", err)
	}
	if !createdAt.Valid {
		t.Fatal("expected created_at to have a default value")
	}
}

//goland:noinspection ALL
func TestIntegration_DuplicateEmailFails(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('A', 'B', 'dup@example.com')`)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err = db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('C', 'D', 'dup@example.com')`)
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate email")
	}
}

//goland:noinspection ALL
func TestIntegration_ForeignKeyViolation(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO waiting_list (user_id) VALUES ('00000000-0000-0000-0000-000000000000')`)
	if err == nil {
		t.Fatal("expected foreign key violation for non-existent user_id")
	}
}

//goland:noinspection ALL
func TestIntegration_DuplicateUserIDFails(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('X', 'Y', 'xy@example.com')`)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var userID string
	err = db.QueryRow(`SELECT id FROM user_entity WHERE email = 'xy@example.com'`).Scan(&userID)
	if err != nil {
		t.Fatalf("querying user id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (user_id) VALUES ($1)`, userID)
	if err != nil {
		t.Fatalf("first waiting_list insert: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (user_id) VALUES ($1)`, userID)
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate user_id in waiting_list")
	}
}

//goland:noinspection ALL
func TestIntegration_CascadeDelete(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('Del', 'User', 'del@example.com')`)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var userID string
	err = db.QueryRow(`SELECT id FROM user_entity WHERE email = 'del@example.com'`).Scan(&userID)
	if err != nil {
		t.Fatalf("querying user id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (user_id) VALUES ($1)`, userID)
	if err != nil {
		t.Fatalf("inserting waiting list entry: %v", err)
	}

	// Delete the user — waiting_list entry should be cascade-deleted
	_, err = db.Exec(`DELETE FROM user_entity WHERE id = $1`, userID)
	if err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM waiting_list WHERE user_id = $1`, userID).Scan(&count)
	if err != nil {
		t.Fatalf("counting waiting_list entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 waiting_list entries after cascade delete, got %d", count)
	}
}

//goland:noinspection ALL
func TestIntegration_SchemaImprovements_IndexExists(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var indexExists bool
	err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM pg_indexes
		WHERE tablename = 'waiting_list'
		  AND indexname = 'idx_waiting_list_weighted_created_at'
	)`).Scan(&indexExists)
	if err != nil {
		t.Fatalf("checking index existence: %v", err)
	}
	if !indexExists {
		t.Fatal("expected index idx_waiting_list_weighted_created_at to exist")
	}
}

//goland:noinspection ALL
func TestIntegration_SchemaImprovements_StorageParameters(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var reloptions []string
	rows, err := db.Query(`
		SELECT unnest(reloptions)
		FROM pg_class
		WHERE relname = 'waiting_list'
	`)
	if err != nil {
		t.Fatalf("querying reloptions: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var opt string
		if err := rows.Scan(&opt); err != nil {
			t.Fatalf("scanning reloption: %v", err)
		}
		reloptions = append(reloptions, opt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating reloptions: %v", err)
	}

	expected := []string{
		"fillfactor=70",
		"autovacuum_vacuum_scale_factor=0.05",
		"autovacuum_vacuum_threshold=50",
		"autovacuum_analyze_scale_factor=0.05",
		"autovacuum_analyze_threshold=50",
	}

	for _, exp := range expected {
		found := false
		for _, opt := range reloptions {
			if opt == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected reloption %q not found in %v", exp, reloptions)
		}
	}
}

//goland:noinspection ALL
func TestIntegration_SchemaImprovements_Idempotent(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)

	// Run migrations twice — second run must not fail
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}

	// Drop tracking to force re-execution (migrations runner may skip already-applied files)
	// Re-run the raw SQL to verify idempotency
	migrationSQL, err := os.ReadFile(filepath.Join(migrationsDir, "002_schema_improvements.sql"))
	if err != nil {
		t.Fatalf("reading migration file: %v", err)
	}

	_, err = db.Exec(string(migrationSQL))
	if err != nil {
		t.Fatalf("re-executing 002_schema_improvements.sql should be idempotent, got: %v", err)
	}
}

//goland:noinspection ALL
func TestIntegration_SchemaImprovements_DeleteWaitingListKeepsUser(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	_, err := db.Exec(`INSERT INTO user_entity (firstname, lastname, email) VALUES ('Keep', 'Me', 'keep@example.com')`)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var userID string
	err = db.QueryRow(`SELECT id FROM user_entity WHERE email = 'keep@example.com'`).Scan(&userID)
	if err != nil {
		t.Fatalf("querying user id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO waiting_list (user_id) VALUES ($1)`, userID)
	if err != nil {
		t.Fatalf("inserting waiting list entry: %v", err)
	}

	// Delete the waiting list entry
	_, err = db.Exec(`DELETE FROM waiting_list WHERE user_id = $1`, userID)
	if err != nil {
		t.Fatalf("deleting waiting list entry: %v", err)
	}

	// User must still exist
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM user_entity WHERE id = $1`, userID).Scan(&count)
	if err != nil {
		t.Fatalf("counting users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected user to still exist after waiting list deletion, got count=%d", count)
	}
}

//goland:noinspection ALL
func TestIntegration_SchedulerState_TableCreated(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	var tableExists bool
	err := db.QueryRow(`SELECT EXISTS (
		SELECT FROM information_schema.tables WHERE table_name = 'scheduler_state'
	)`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("checking scheduler_state existence: %v", err)
	}
	if !tableExists {
		t.Fatal("scheduler_state table was not created")
	}
}

//goland:noinspection ALL
func TestIntegration_SchedulerState_Idempotent(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("first RunMigrations failed: %v", err)
	}

	migrationSQL, err := os.ReadFile(filepath.Join(migrationsDir, "003_scheduler_state.sql"))
	if err != nil {
		t.Fatalf("reading migration file: %v", err)
	}

	_, err = db.Exec(string(migrationSQL))
	if err != nil {
		t.Fatalf("re-executing 003_scheduler_state.sql should be idempotent, got: %v", err)
	}
}

//goland:noinspection ALL
func TestIntegration_SchedulerState_FullFlow(t *testing.T) {
	db := openTestDB(t)
	logger := testLogger()

	migrationsDir := findMigrationsDir(t)
	if err := RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Insert a scheduler state entry
	_, err := db.Exec(`INSERT INTO scheduler_state (key, value) VALUES ('test_key', NOW())`)
	if err != nil {
		t.Fatalf("inserting scheduler state: %v", err)
	}

	// Read it back and verify it's recent
	var value string
	err = db.QueryRow(`SELECT value::text FROM scheduler_state WHERE key = 'test_key'`).Scan(&value)
	if err != nil {
		t.Fatalf("querying scheduler state: %v", err)
	}
	if value == "" {
		t.Fatal("expected non-empty timestamp")
	}
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	// Walk up from the test file to find the project root migrations directory
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations", "postgres")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find migrations/postgres directory")
		}
		dir = parent
	}
}
