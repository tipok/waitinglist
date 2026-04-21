package repository

import (
	"errors"
	"testing"

	"github.com/tipok/waitinglist/internal/model"
)

func TestWaitingList_Add_InsertsEntry(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user := &model.UserEntity{
		Firstname: "John",
		Lastname:  "Doe",
		Email:     "wl-add@example.com",
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	entry, err := wlRepo.Add(ctx, db, user.ID, "203.0.113.50")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if entry.ID == "" {
		t.Error("expected non-empty ID")
	}
	if entry.UserID != user.ID {
		t.Errorf("expected user_id %s, got %s", user.ID, entry.UserID)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("expected created_at to be populated")
	}
	if entry.IPAddress == nil || *entry.IPAddress != "203.0.113.50" {
		t.Errorf("expected ip_address 203.0.113.50, got %v", entry.IPAddress)
	}
}

func TestWaitingList_Add_DuplicateUserID(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user := &model.UserEntity{
		Firstname: "Jane",
		Lastname:  "Doe",
		Email:     "wl-dup@example.com",
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	if _, err := wlRepo.Add(ctx, db, user.ID, "10.0.0.1"); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	_, err := wlRepo.Add(ctx, db, user.ID, "10.0.0.2")
	if !errors.Is(err, model.ErrAlreadyOnWaitingList) {
		t.Fatalf("expected ErrAlreadyOnWaitingList, got %v", err)
	}
}

func TestWaitingList_Add_NonExistentUserID(t *testing.T) {
	db := setupTestDB(t)
	wlRepo := NewWaitingListRepository(db)

	_, err := wlRepo.Add(t.Context(), db, "00000000-0000-0000-0000-000000000000", "10.0.0.1")
	if !errors.Is(err, model.ErrWaitingListForeignKey) {
		t.Fatalf("expected ErrWaitingListForeignKey, got %v", err)
	}
}

func TestWaitingList_GetAll_ReturnsEntries(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user1 := &model.UserEntity{Firstname: "A", Lastname: "User", Email: "a@example.com"}
	user2 := &model.UserEntity{Firstname: "B", Lastname: "User", Email: "b@example.com"}
	if err := userRepo.Create(ctx, user1); err != nil {
		t.Fatalf("failed to create user1: %v", err)
	}
	if err := userRepo.Create(ctx, user2); err != nil {
		t.Fatalf("failed to create user2: %v", err)
	}

	if _, err := wlRepo.Add(ctx, db, user1.ID, "10.0.0.1"); err != nil {
		t.Fatalf("failed to add user1: %v", err)
	}
	if _, err := wlRepo.Add(ctx, db, user2.ID, "10.0.0.2"); err != nil {
		t.Fatalf("failed to add user2: %v", err)
	}

	entries, err := wlRepo.GetAll(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Entries should be ordered by created_at ASC.
	if entries[0].CreatedAt.After(entries[1].CreatedAt) {
		t.Error("expected entries ordered by created_at ASC")
	}
}

func TestWaitingList_GetAll_EmptyTable(t *testing.T) {
	db := setupTestDB(t)
	wlRepo := NewWaitingListRepository(db)

	entries, err := wlRepo.GetAll(t.Context())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if entries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestWaitingList_Add_CreatedAtAutoPopulated(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user := &model.UserEntity{
		Firstname: "Auto",
		Lastname:  "Time",
		Email:     "autotime@example.com",
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	entry, err := wlRepo.Add(ctx, db, user.ID, "10.0.0.1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if entry.CreatedAt.IsZero() {
		t.Error("expected created_at to be auto-populated by database default")
	}
}

func TestWaitingList_DeleteByIDs_SingleEntry(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user := &model.UserEntity{Firstname: "Del", Lastname: "One", Email: "del-one@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	entry, err := wlRepo.Add(ctx, db, user.ID, "10.0.0.1")
	if err != nil {
		t.Fatalf("failed to add entry: %v", err)
	}

	if err := wlRepo.DeleteByIDs(ctx, []string{entry.ID}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	entries, err := wlRepo.GetAll(ctx)
	if err != nil {
		t.Fatalf("failed to get all: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(entries))
	}
}

func TestWaitingList_DeleteByIDs_MultipleEntries(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	wlRepo := NewWaitingListRepository(db)
	ctx := t.Context()

	user1 := &model.UserEntity{Firstname: "Del", Lastname: "Multi1", Email: "del-m1@example.com"}
	user2 := &model.UserEntity{Firstname: "Del", Lastname: "Multi2", Email: "del-m2@example.com"}
	if err := userRepo.Create(ctx, user1); err != nil {
		t.Fatalf("failed to create user1: %v", err)
	}
	if err := userRepo.Create(ctx, user2); err != nil {
		t.Fatalf("failed to create user2: %v", err)
	}

	entry1, err := wlRepo.Add(ctx, db, user1.ID, "10.0.0.1")
	if err != nil {
		t.Fatalf("failed to add entry1: %v", err)
	}
	entry2, err := wlRepo.Add(ctx, db, user2.ID, "10.0.0.2")
	if err != nil {
		t.Fatalf("failed to add entry2: %v", err)
	}

	if err := wlRepo.DeleteByIDs(ctx, []string{entry1.ID, entry2.ID}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	entries, err := wlRepo.GetAll(ctx)
	if err != nil {
		t.Fatalf("failed to get all: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(entries))
	}
}

func TestWaitingList_DeleteByIDs_EmptySlice(t *testing.T) {
	db := setupTestDB(t)
	wlRepo := NewWaitingListRepository(db)

	err := wlRepo.DeleteByIDs(t.Context(), []string{})
	if err != nil {
		t.Fatalf("expected no error for empty slice, got %v", err)
	}
}

func TestWaitingList_DeleteByIDs_NonExistentIDs(t *testing.T) {
	db := setupTestDB(t)
	wlRepo := NewWaitingListRepository(db)

	// Deleting non-existent IDs should not return an error.
	err := wlRepo.DeleteByIDs(t.Context(), []string{"00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("expected no error for non-existent IDs, got %v", err)
	}
}
