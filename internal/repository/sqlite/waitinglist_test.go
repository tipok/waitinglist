package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/repository/sqlite"
)

func TestWaitingListRepository_Add(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "wl-add@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}

	entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if entry.ID == "" {
		t.Error("expected entry ID to be set")
	}
	if entry.UserID != u.ID {
		t.Errorf("expected UserID=%s, got %s", u.ID, entry.UserID)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestWaitingListRepository_Add_AlreadyOnWaitlist(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "dup-wl@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}

	if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	_, err := wlRepo.Add(ctx, db, "proj1", u.ID)
	if err != model.ErrAlreadyOnWaitingList {
		t.Errorf("expected ErrAlreadyOnWaitingList, got: %v", err)
	}
}

func TestWaitingListRepository_Add_ForeignKeyViolation(t *testing.T) {
	db := newTestDB(t)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	_, err := wlRepo.Add(ctx, db, "proj1", "nonexistent-user-id")
	if err != model.ErrWaitingListForeignKey {
		t.Errorf("expected ErrWaitingListForeignKey, got: %v", err)
	}
}

func TestWaitingListRepository_GetAll(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "ga1@example.com")
	u2 := newUser("proj1", "ga2@example.com")
	for _, u := range []*model.UserEntity{u1, u2} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
	}

	entries, err := wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestWaitingListRepository_GetWithOffsetLimit(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		email := "ol" + string(rune('0'+i)) + "@example.com"
		u := newUser("proj1", email)
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
	}

	limit := 2
	offset := 1
	entries, err := wlRepo.GetWithOffsetLimit(ctx, "proj1", &offset, &limit)
	if err != nil {
		t.Fatalf("GetWithOffsetLimit failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestWaitingListRepository_GetEnlistedSince(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)

	u := newUser("proj1", "since-wl@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}
	if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
		t.Fatalf("Add to waitlist failed: %v", err)
	}

	rows, err := wlRepo.GetEnlistedSince(ctx, "proj1", before)
	if err != nil {
		t.Fatalf("GetEnlistedSince failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}

	future := time.Now().UTC().Add(time.Hour)
	rows, err = wlRepo.GetEnlistedSince(ctx, "proj1", future)
	if err != nil {
		t.Fatalf("GetEnlistedSince with future time failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows with future since time, got %d", len(rows))
	}
}

func TestWaitingListRepository_ListAllJoined(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "aj1@example.com")
	u2 := newUser("proj1", "aj2@example.com")
	for _, u := range []*model.UserEntity{u1, u2} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
	}

	rows, err := wlRepo.ListAllJoined(ctx, "proj1")
	if err != nil {
		t.Fatalf("ListAllJoined failed: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Email == "" {
		t.Error("expected email to be populated in joined row")
	}
}

func TestWaitingListRepository_ListJoined(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "charlie@example.com")
	u2 := newUser("proj1", "delta@example.com")
	for _, u := range []*model.UserEntity{u1, u2} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
	}

	rows, err := wlRepo.ListJoined(ctx, "proj1", "", 10, 0)
	if err != nil {
		t.Fatalf("ListJoined failed: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	rows, err = wlRepo.ListJoined(ctx, "proj1", "charlie", 10, 0)
	if err != nil {
		t.Fatalf("ListJoined with filter failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row matching 'charlie', got %d", len(rows))
	}
	if rows[0].Email != "charlie@example.com" {
		t.Errorf("expected charlie@example.com, got %s", rows[0].Email)
	}
}

func TestWaitingListRepository_ListJoined_AllProjects(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "p1user@example.com")
	u2 := newUser("proj2", "p2user@example.com")
	for _, u := range []*model.UserEntity{u1, u2} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, u.ProjectSlug, u.ID); err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
	}

	rows, err := wlRepo.ListJoined(ctx, "", "", 10, 0)
	if err != nil {
		t.Fatalf("ListJoined (all projects) failed: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows across all projects, got %d", len(rows))
	}
}

func TestWaitingListRepository_DeleteByIDs(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u1 := newUser("proj1", "del1@example.com")
	u2 := newUser("proj1", "del2@example.com")
	var entryIDs []string
	for _, u := range []*model.UserEntity{u1, u2} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user failed: %v", err)
		}
		entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
		if err != nil {
			t.Fatalf("Add to waitlist failed: %v", err)
		}
		entryIDs = append(entryIDs, entry.ID)
	}

	if err := wlRepo.DeleteByIDs(ctx, entryIDs); err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}

	entries, err := wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll after delete failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(entries))
	}
}

func TestWaitingListRepository_DeleteByIDs_Empty(t *testing.T) {
	db := newTestDB(t)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	if err := wlRepo.DeleteByIDs(ctx, nil); err != nil {
		t.Errorf("DeleteByIDs(nil) should be a no-op, got: %v", err)
	}
}

func TestWaitingListRepository_DeleteByID(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "delbyid@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}
	entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
	if err != nil {
		t.Fatalf("Add to waitlist failed: %v", err)
	}

	if err := wlRepo.DeleteByID(ctx, entry.ID); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	entries, err := wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll after DeleteByID failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestWaitingListRepository_DeleteByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	err := wlRepo.DeleteByID(ctx, "nonexistent-entry-id")
	if err != model.ErrWaitingListEntryNotFound {
		t.Errorf("expected ErrWaitingListEntryNotFound, got: %v", err)
	}
}

func TestWaitingListRepository_DeleteByUserID(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "delbyuid@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}
	if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
		t.Fatalf("Add to waitlist failed: %v", err)
	}

	if err := wlRepo.DeleteByUserID(ctx, u.ID); err != nil {
		t.Fatalf("DeleteByUserID failed: %v", err)
	}

	entries, err := wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll after DeleteByUserID failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after DeleteByUserID, got %d", len(entries))
	}
}

func TestWaitingListRepository_DeleteByUserID_NotOnList(t *testing.T) {
	db := newTestDB(t)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	// Should not return an error when user is not on the list.
	if err := wlRepo.DeleteByUserID(ctx, "any-user-id"); err != nil {
		t.Errorf("DeleteByUserID on non-existent user should be no-op, got: %v", err)
	}
}

func TestWaitingListRepository_BeginTx_CommitRollback(t *testing.T) {
	db := newTestDB(t)
	userRepo := sqlite.NewUserRepository(db)
	wlRepo := sqlite.NewWaitingListRepository(db)
	ctx := context.Background()

	u := newUser("proj1", "tx-wl@example.com")
	if err := userRepo.Create(ctx, u); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}

	// Test rollback: entry should not be persisted.
	tx, err := wlRepo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	if _, err := wlRepo.Add(ctx, tx, "proj1", u.ID); err != nil {
		t.Fatalf("Add in tx failed: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	entries, err := wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll after rollback failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after rollback, got %d", len(entries))
	}

	// Test commit: entry should be persisted.
	tx2, err := wlRepo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx (commit test) failed: %v", err)
	}
	if _, err := wlRepo.Add(ctx, tx2, "proj1", u.ID); err != nil {
		t.Fatalf("Add in tx2 failed: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	entries, err = wlRepo.GetAll(ctx, "proj1")
	if err != nil {
		t.Fatalf("GetAll after commit failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after commit, got %d", len(entries))
	}
}
