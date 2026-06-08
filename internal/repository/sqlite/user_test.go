package sqlite_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/database"
	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/repository/sqlite"
)

// projectRoot finds the repository root by walking up from the test file location.
func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// newTestDB creates an in-memory SQLite DB with migrations applied.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _, err := database.New("sqlite://:memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrationsDir := filepath.Join(projectRoot(), "migrations", "sqlite")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := database.RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return db
}

func newUser(projectSlug, email string) *model.UserEntity {
	return &model.UserEntity{
		ProjectSlug: projectSlug,
		Firstname:   "First",
		Lastname:    "Last",
		Email:       email,
	}
}

func TestUserRepository_Create(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "test@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if user.ID == "" {
		t.Error("expected ID to be set after Create")
	}
	if user.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set after Create")
	}
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user1 := newUser("proj1", "dup@example.com")
	if err := repo.Create(ctx, user1); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	user2 := newUser("proj1", "dup@example.com")
	err := repo.Create(ctx, user2)
	if err != model.ErrDuplicateEmail {
		t.Errorf("expected ErrDuplicateEmail, got: %v", err)
	}
}

func TestUserRepository_GetByEmail(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "byemail@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByEmail(ctx, "proj1", "byemail@example.com")
	if err != nil {
		t.Fatalf("GetByEmail failed: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("expected ID %s, got %s", user.ID, got.ID)
	}
}

func TestUserRepository_GetByEmail_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "proj1", "nonexistent@example.com")
	if err != model.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestUserRepository_GetByID(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "byid@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Email != user.Email {
		t.Errorf("expected email %s, got %s", user.Email, got.Email)
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != model.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestUserRepository_GetByIDs(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "a@example.com")
	u2 := newUser("proj1", "b@example.com")
	if err := repo.Create(ctx, u1); err != nil {
		t.Fatalf("Create u1 failed: %v", err)
	}
	if err := repo.Create(ctx, u2); err != nil {
		t.Fatalf("Create u2 failed: %v", err)
	}

	got, err := repo.GetByIDs(ctx, []string{u1.ID, u2.ID})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 users, got %d", len(got))
	}
}

func TestUserRepository_GetByIDs_Empty(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	got, err := repo.GetByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("GetByIDs(nil) failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestUserRepository_GetUserInfoByEmails(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "info@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	infos, err := repo.GetUserInfoByEmails(ctx, "proj1", []string{"info@example.com", "noone@example.com"})
	if err != nil {
		t.Fatalf("GetUserInfoByEmails failed: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("expected 1 result, got %d", len(infos))
	}
}

func TestUserRepository_GetUserInfoByEmails_Empty(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	infos, err := repo.GetUserInfoByEmails(ctx, "proj1", nil)
	if err != nil {
		t.Fatalf("GetUserInfoByEmails(nil) failed: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected empty slice, got %v", infos)
	}
}

func TestUserRepository_GrantAccessTx(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "grant@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	got, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if !got.HasAccess {
		t.Error("expected HasAccess=true after GrantAccess")
	}
	if got.AccessGrantedBy == nil || *got.AccessGrantedBy != "admin" {
		t.Errorf("expected AccessGrantedBy='admin', got: %v", got.AccessGrantedBy)
	}
}

func TestUserRepository_GrantAccessTx_InvalidSource(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	err := repo.GrantAccess(ctx, []string{"any-id"}, "invalid-source")
	if err == nil {
		t.Error("expected error for invalid grant source")
	}
}

func TestUserRepository_GrantAccessTx_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	err := repo.GrantAccess(ctx, []string{"nonexistent"}, "admin")
	if err != model.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestUserRepository_GrantAccessTx_EmptyIDs(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	if err := repo.GrantAccess(ctx, nil, "admin"); err != nil {
		t.Errorf("expected nil for empty ids, got: %v", err)
	}
}

func TestUserRepository_RevokeAccessTx(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "revoke@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	if err := repo.RevokeAccess(ctx, user.ID, "test reason", "admin-user"); err != nil {
		t.Fatalf("RevokeAccess failed: %v", err)
	}

	got, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke failed: %v", err)
	}
	if got.HasAccess {
		t.Error("expected HasAccess=false after RevokeAccess")
	}
	if got.AccessRevokeReason == nil || *got.AccessRevokeReason != "test reason" {
		t.Errorf("expected revoke reason 'test reason', got: %v", got.AccessRevokeReason)
	}
}

func TestUserRepository_RevokeAccessTx_EmptyReason(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	err := repo.RevokeAccess(ctx, "any-id", "   ", "admin")
	if err != model.ErrRevokeReasonRequired {
		t.Errorf("expected ErrRevokeReasonRequired, got: %v", err)
	}
}

func TestUserRepository_RevokeAccessTx_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	err := repo.RevokeAccess(ctx, "nonexistent", "reason", "admin")
	if err != model.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestUserRepository_CountByAccess(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "c1@example.com")
	u2 := newUser("proj1", "c2@example.com")
	if err := repo.Create(ctx, u1); err != nil {
		t.Fatalf("Create u1 failed: %v", err)
	}
	if err := repo.Create(ctx, u2); err != nil {
		t.Fatalf("Create u2 failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{u1.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}
	// Insert a waiting_list row directly to test CountByAccess without waitlist repo.
	_, err := db.ExecContext(ctx,
		`INSERT INTO waiting_list (id, user_id, project_slug) VALUES (?, ?, ?)`,
		"wl-test-id", u2.ID, "proj1",
	)
	if err != nil {
		t.Fatalf("inserting waiting list row: %v", err)
	}

	wl, access, err := repo.CountByAccess(ctx, "proj1")
	if err != nil {
		t.Fatalf("CountByAccess failed: %v", err)
	}
	if wl != 1 {
		t.Errorf("expected waitlist count 1, got %d", wl)
	}
	if access != 1 {
		t.Errorf("expected access count 1, got %d", access)
	}
}

func TestUserRepository_EnlistmentsByDay(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	user := newUser("proj1", "day@example.com")
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	counts, err := repo.EnlistmentsByDay(ctx, "proj1", 7)
	if err != nil {
		t.Fatalf("EnlistmentsByDay failed: %v", err)
	}
	if len(counts) != 7 {
		t.Errorf("expected 7 days, got %d", len(counts))
	}

	total := 0
	for _, dc := range counts {
		total += dc.Count
	}
	if total != 1 {
		t.Errorf("expected total 1 enlistment, got %d", total)
	}
}

func TestUserRepository_ListWithAccess(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "list1@example.com")
	u2 := newUser("proj1", "list2@example.com")
	if err := repo.Create(ctx, u1); err != nil {
		t.Fatalf("Create u1 failed: %v", err)
	}
	if err := repo.Create(ctx, u2); err != nil {
		t.Fatalf("Create u2 failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{u1.ID, u2.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	users, err := repo.ListWithAccess(ctx, "proj1", "", 10, 0)
	if err != nil {
		t.Fatalf("ListWithAccess failed: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestUserRepository_ListWithAccess_EmailFilter(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "alice@example.com")
	u2 := newUser("proj1", "bob@example.com")
	if err := repo.Create(ctx, u1); err != nil {
		t.Fatalf("Create u1 failed: %v", err)
	}
	if err := repo.Create(ctx, u2); err != nil {
		t.Fatalf("Create u2 failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{u1.ID, u2.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	users, err := repo.ListWithAccess(ctx, "proj1", "alice", 10, 0)
	if err != nil {
		t.Fatalf("ListWithAccess with filter failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user matching 'alice', got %d", len(users))
	}
}

func TestUserRepository_ListAllWithAccess(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "allaccess@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{u.ID}, "scheduler"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	users, err := repo.ListAllWithAccess(ctx, "proj1")
	if err != nil {
		t.Fatalf("ListAllWithAccess failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}
}

func TestUserRepository_GetGrantedSince(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)

	u := newUser("proj1", "since@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := repo.GrantAccess(ctx, []string{u.ID}, "admin"); err != nil {
		t.Fatalf("GrantAccess failed: %v", err)
	}

	users, err := repo.GetGrantedSince(ctx, "proj1", before)
	if err != nil {
		t.Fatalf("GetGrantedSince failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user granted since before, got %d", len(users))
	}

	future := time.Now().UTC().Add(time.Hour)
	users, err = repo.GetGrantedSince(ctx, "proj1", future)
	if err != nil {
		t.Fatalf("GetGrantedSince with future time failed: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users with future since time, got %d", len(users))
	}
}
