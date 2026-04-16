package repository

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/tipok/waitinglist/internal/database"
	"github.com/tipok/waitinglist/internal/model"
)

//goland:noinspection ALL
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("failed to ping database: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := database.RunMigrations(db, "../../migrations", logger); err != nil {
		db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Clean up user_entity table before each test.
	if _, err := db.Exec("DELETE FROM waiting_list"); err != nil {
		db.Close()
		t.Fatalf("failed to clean waiting_list: %v", err)
	}
	if _, err := db.Exec("DELETE FROM user_entity"); err != nil {
		db.Close()
		t.Fatalf("failed to clean user_entity: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestCreate_InsertsUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	user := &model.UserEntity{
		Firstname: "John",
		Lastname:  "Doe",
		Email:     "john@example.com",
	}

	err := repo.Create(t.Context(), user)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.ID == "" {
		t.Fatal("expected ID to be populated")
	}
	if user.HasAccess {
		t.Error("expected has_access to be false")
	}
}

func TestCreate_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	user := &model.UserEntity{
		Firstname: "John",
		Lastname:  "Doe",
		Email:     "dup@example.com",
	}

	if err := repo.Create(t.Context(), user); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	user2 := &model.UserEntity{
		Firstname: "Jane",
		Lastname:  "Smith",
		Email:     "dup@example.com",
	}

	err := repo.Create(t.Context(), user2)
	if !errors.Is(err, model.ErrDuplicateEmail) {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
}

func TestCreate_MaxLengthFields(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	long := strings.Repeat("a", 255)
	user := &model.UserEntity{
		Firstname: long,
		Lastname:  long,
		Email:     long[:245] + "@test.com",
	}

	err := repo.Create(t.Context(), user)
	if err != nil {
		t.Fatalf("expected no error for max-length fields, got %v", err)
	}

	if user.ID == "" {
		t.Fatal("expected ID to be populated")
	}
}

func TestGetByEmail_Found(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	original := &model.UserEntity{
		Firstname: "Alice",
		Lastname:  "Wonder",
		Email:     "alice@example.com",
	}
	if err := repo.Create(ctx, original); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if found.ID != original.ID {
		t.Errorf("expected id %s, got %s", original.ID, found.ID)
	}
	if found.Firstname != "Alice" {
		t.Errorf("expected firstname Alice, got %s", found.Firstname)
	}
	if found.Lastname != "Wonder" {
		t.Errorf("expected lastname Wonder, got %s", found.Lastname)
	}
	if found.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", found.Email)
	}
	if found.HasAccess {
		t.Error("expected has_access to be false")
	}
}

func TestGetByEmail_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	_, err := repo.GetByEmail(t.Context(), "nobody@example.com")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGetByEmail_CaseSensitive(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	user := &model.UserEntity{
		Firstname: "Bob",
		Lastname:  "Smith",
		Email:     "Bob@Example.com",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Exact case should be found.
	found, err := repo.GetByEmail(ctx, "Bob@Example.com")
	if err != nil {
		t.Fatalf("expected no error for exact case, got %v", err)
	}
	if found.Email != "Bob@Example.com" {
		t.Errorf("expected Bob@Example.com, got %s", found.Email)
	}

	// Different case should not be found (PostgreSQL is case-sensitive by default).
	_, err = repo.GetByEmail(ctx, "bob@example.com")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound for different case, got %v", err)
	}
}

func TestCreate_PopulatesAllFields(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	user := &model.UserEntity{
		Firstname: "Test",
		Lastname:  "User",
		Email:     "fields@example.com",
	}

	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify by reading back.
	found, err := repo.GetByEmail(context.Background(), "fields@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if found.ID == "" {
		t.Error("expected non-empty ID")
	}
	if found.Firstname != "Test" {
		t.Errorf("expected firstname Test, got %s", found.Firstname)
	}
	if found.Lastname != "User" {
		t.Errorf("expected lastname User, got %s", found.Lastname)
	}
	if found.Email != "fields@example.com" {
		t.Errorf("expected email fields@example.com, got %s", found.Email)
	}
	if found.HasAccess {
		t.Error("expected has_access false")
	}
}
