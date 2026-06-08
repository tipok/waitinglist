package postgres

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
	if err := database.RunMigrations(db, "../../../migrations/postgres", logger); err != nil {
		db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Clean up tables before each test.
	if _, err := db.Exec("DELETE FROM waiting_list"); err != nil {
		db.Close()
		t.Fatalf("failed to clean waiting_list: %v", err)
	}
	if _, err := db.Exec("DELETE FROM user_entity"); err != nil {
		db.Close()
		t.Fatalf("failed to clean user_entity: %v", err)
	}
	if _, err := db.Exec("DELETE FROM scheduler_state"); err != nil {
		db.Close()
		t.Fatalf("failed to clean scheduler_state: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func defaultProjectSlug() string {
	return "default"
}

func TestCreate_InsertsUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
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

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
		Firstname: "John",
		Lastname:  "Doe",
		Email:     "dup@example.com",
	}

	if err := repo.Create(t.Context(), user); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	user2 := &model.UserEntity{ProjectSlug: pid,
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
	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
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
	pid := defaultProjectSlug()

	original := &model.UserEntity{ProjectSlug: pid,
		Firstname: "Alice",
		Lastname:  "Wonder",
		Email:     "alice@example.com",
	}
	if err := repo.Create(ctx, original); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "alice@example.com")
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

	_, err := repo.GetByEmail(t.Context(), defaultProjectSlug(), "nobody@example.com")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGetByEmail_CaseSensitive(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
		Firstname: "Bob",
		Lastname:  "Smith",
		Email:     "Bob@Example.com",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Exact case should be found.
	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "Bob@Example.com")
	if err != nil {
		t.Fatalf("expected no error for exact case, got %v", err)
	}
	if found.Email != "Bob@Example.com" {
		t.Errorf("expected Bob@Example.com, got %s", found.Email)
	}

	// Different case should not be found (PostgreSQL is case-sensitive by default).
	_, err = repo.GetByEmail(ctx, defaultProjectSlug(), "bob@example.com")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound for different case, got %v", err)
	}
}

func TestCreate_PopulatesAllFields(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
		Firstname: "Test",
		Lastname:  "User",
		Email:     "fields@example.com",
	}

	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify by reading back.
	found, err := repo.GetByEmail(context.Background(), pid, "fields@example.com")
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

func TestGrantAccess_SingleUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid,
		Firstname: "Grant",
		Lastname:  "Access",
		Email:     "grant@example.com",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := repo.GrantAccess(ctx, []string{user.ID}, "scheduler"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "grant@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found.HasAccess {
		t.Error("expected has_access to be true")
	}
}

func TestGrantAccess_MultipleUsers(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	user1 := &model.UserEntity{
		Firstname: "User",
		Lastname:  "One",
		Email:     "user1@example.com",
	}
	user2 := &model.UserEntity{
		Firstname: "User",
		Lastname:  "Two",
		Email:     "user2@example.com",
	}
	if err := repo.Create(ctx, user1); err != nil {
		t.Fatalf("create user1 failed: %v", err)
	}
	if err := repo.Create(ctx, user2); err != nil {
		t.Fatalf("create user2 failed: %v", err)
	}

	if err := repo.GrantAccess(ctx, []string{user1.ID, user2.ID}, "scheduler"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	found1, err := repo.GetByEmail(ctx, defaultProjectSlug(), "user1@example.com")
	if err != nil {
		t.Fatalf("get user1 failed: %v", err)
	}
	if !found1.HasAccess {
		t.Error("expected user1 has_access to be true")
	}

	found2, err := repo.GetByEmail(ctx, defaultProjectSlug(), "user2@example.com")
	if err != nil {
		t.Fatalf("get user2 failed: %v", err)
	}
	if !found2.HasAccess {
		t.Error("expected user2 has_access to be true")
	}
}

func TestGrantAccess_EmptySlice(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	err := repo.GrantAccess(t.Context(), []string{}, "scheduler")
	if err != nil {
		t.Fatalf("expected no error for empty slice, got %v", err)
	}
}

func TestGrantAccess_UserNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	err := repo.GrantAccess(t.Context(), []string{"00000000-0000-0000-0000-000000000000"}, "scheduler")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// Migration 007 dropped the one-way has_access trigger from migration 006.
// Revocation is now allowed at the SQL level; the application enforces
// "only via RevokeAccessTx" instead.
func TestHasAccessOneWayTrigger_DroppedByMigration007(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Raw", Lastname: "Update", Email: "raw@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "scheduler"); err != nil {
		t.Fatalf("grant access failed: %v", err)
	}

	//goland:noinspection ALL
	_, err := db.ExecContext(ctx, "UPDATE user_entity SET has_access = false WHERE id = $1", user.ID)
	if err != nil {
		t.Fatalf("expected raw UPDATE to succeed (trigger should be gone), got %v", err)
	}
}

func TestGrantAccessTx_PopulatesAuditColumns(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Granted", Lastname: "Audit", Email: "granted@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := repo.GrantAccess(ctx, []string{user.ID}, "scheduler"); err != nil {
		t.Fatalf("grant access failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "granted@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found.HasAccess {
		t.Error("expected has_access true")
	}
	if found.AccessGrantedAt == nil {
		t.Error("expected access_granted_at populated")
	}
	if found.AccessGrantedBy == nil || *found.AccessGrantedBy != "scheduler" {
		t.Errorf("expected access_granted_by=scheduler, got %v", found.AccessGrantedBy)
	}
	if found.AccessRevokedAt != nil || found.AccessRevokedBy != nil || found.AccessRevokeReason != nil {
		t.Error("expected revoke columns to remain NULL on grant")
	}
}

func TestGrantAccessTx_AdminSource(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Admin", Lastname: "Granted", Email: "admingrant@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("grant access failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "admingrant@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if found.AccessGrantedBy == nil || *found.AccessGrantedBy != "admin" {
		t.Errorf("expected access_granted_by=admin, got %v", found.AccessGrantedBy)
	}
}

func TestGrantAccessTx_RejectsUnknownSource(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Bad", Lastname: "Source", Email: "badsrc@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err := repo.GrantAccess(ctx, []string{user.ID}, "robot")
	if err == nil {
		t.Fatal("expected error for unknown grant source")
	}
	if !strings.Contains(err.Error(), "invalid grant source") {
		t.Errorf("expected error mentioning invalid grant source, got %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "badsrc@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if found.HasAccess {
		t.Error("expected has_access to remain false after rejected grant")
	}
}

func TestRevokeAccessTx_PopulatesAuditAndFlipsFlag(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Revoked", Lastname: "User", Email: "revoked@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("grant access failed: %v", err)
	}

	if err := repo.RevokeAccess(ctx, user.ID, "policy violation", "admin1"); err != nil {
		t.Fatalf("revoke access failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "revoked@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if found.HasAccess {
		t.Error("expected has_access false after revoke")
	}
	if found.AccessRevokedAt == nil {
		t.Error("expected access_revoked_at populated")
	}
	if found.AccessRevokedBy == nil || *found.AccessRevokedBy != "admin1" {
		t.Errorf("expected access_revoked_by=admin1, got %v", found.AccessRevokedBy)
	}
	if found.AccessRevokeReason == nil || *found.AccessRevokeReason != "policy violation" {
		t.Errorf("expected reason=policy violation, got %v", found.AccessRevokeReason)
	}
}

func TestRevokeAccessTx_EmptyReasonRejected(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Empty", Lastname: "Reason", Email: "empty@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("grant access failed: %v", err)
	}

	err := repo.RevokeAccess(ctx, user.ID, "   ", "admin1")
	if !errors.Is(err, model.ErrRevokeReasonRequired) {
		t.Fatalf("expected ErrRevokeReasonRequired, got %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "empty@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found.HasAccess {
		t.Error("expected has_access to remain true after rejected revoke")
	}
}

func TestRevokeAccessTx_UserNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	err := repo.RevokeAccess(t.Context(), "00000000-0000-0000-0000-000000000000", "x", "admin1")
	if !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGrantAccessTx_ClearsPriorRevocation(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Re", Lastname: "Granted", Email: "regranted@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("first grant failed: %v", err)
	}
	if err := repo.RevokeAccess(ctx, user.ID, "abuse", "admin1"); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("re-grant failed: %v", err)
	}

	found, err := repo.GetByEmail(ctx, defaultProjectSlug(), "regranted@example.com")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found.HasAccess {
		t.Error("expected has_access true after re-grant")
	}
	if found.AccessRevokedAt != nil || found.AccessRevokedBy != nil || found.AccessRevokeReason != nil {
		t.Error("expected revoke columns cleared after re-grant")
	}
}

func TestGetUserInfoByEmails_IncludesRevokeReason(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Lookup", Lastname: "Revoked", Email: "lookup@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "scheduler"); err != nil {
		t.Fatalf("grant failed: %v", err)
	}
	if err := repo.RevokeAccess(ctx, user.ID, "spam", "admin1"); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	infos, err := repo.GetUserInfoByEmails(ctx, defaultProjectSlug(), []string{"lookup@example.com"})
	if err != nil {
		t.Fatalf("get user info failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 row, got %d", len(infos))
	}
	got := infos[0]
	if got.HasAccess {
		t.Error("expected has_access false")
	}
	if got.AccessRevokeReason == nil || *got.AccessRevokeReason != "spam" {
		t.Errorf("expected reason=spam, got %v", got.AccessRevokeReason)
	}
	if got.AccessRevokedAt == nil {
		t.Error("expected access_revoked_at populated")
	}
}

func TestRevokePairCheckConstraint(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := t.Context()

	pid := defaultProjectSlug()
	user := &model.UserEntity{ProjectSlug: pid, Firstname: "Pair", Lastname: "Check", Email: "pair@example.com"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Setting access_revoked_at without a reason must violate the
	// user_entity_revoke_pair_check constraint installed by migration 007.
	//goland:noinspection ALL
	_, err := db.ExecContext(ctx,
		"UPDATE user_entity SET access_revoked_at = NOW() WHERE id = $1", user.ID)
	if err == nil {
		t.Fatal("expected revoke pair CHECK constraint to reject revoked_at without reason")
	}
	if !strings.Contains(err.Error(), "user_entity_revoke_pair_check") {
		t.Errorf("expected error mentioning the constraint name, got %v", err)
	}
}
