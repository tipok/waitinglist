package postgres

import (
	"testing"
	"time"
)

func TestGetLastSuccess_NoRow(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSchedulerRepository(db)
	pid := defaultProjectSlug()

	got, err := repo.GetLastSuccess(t.Context(), pid, "nonexistent_key")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time for missing key, got %v", got)
	}
}

func TestUpdateLastSuccess_InsertsAndReturnsTimestamp(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSchedulerRepository(db)
	pid := defaultProjectSlug()

	key := "test_last_success"
	before := time.Now().Add(-1 * time.Second)

	err := repo.UpdateLastSuccess(t.Context(), db, pid, key)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := repo.GetLastSuccess(t.Context(), pid, key)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero time after update")
	}
	if got.Before(before) {
		t.Fatalf("expected timestamp after %v, got %v", before, got)
	}
}

func TestUpdateLastSuccess_UpsertUpdatesTimestamp(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSchedulerRepository(db)
	pid := defaultProjectSlug()

	key := "test_upsert"

	err := repo.UpdateLastSuccess(t.Context(), db, pid, key)
	if err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	first, err := repo.GetLastSuccess(t.Context(), pid, key)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Small sleep to ensure timestamp differs
	time.Sleep(10 * time.Millisecond)

	err = repo.UpdateLastSuccess(t.Context(), db, pid, key)
	if err != nil {
		t.Fatalf("second update failed: %v", err)
	}

	second, err := repo.GetLastSuccess(t.Context(), pid, key)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !second.After(first) {
		t.Fatalf("expected second timestamp (%v) to be after first (%v)", second, first)
	}
}

func TestUpdateLastSuccess_RollbackOnTxFailure(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSchedulerRepository(db)
	pid := defaultProjectSlug()

	key := "test_rollback"

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	err = repo.UpdateLastSuccess(t.Context(), tx, pid, key)
	if err != nil {
		t.Fatalf("update in tx failed: %v", err)
	}

	// Rollback the transaction
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	// The value should not be persisted
	got, err := repo.GetLastSuccess(t.Context(), pid, key)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time after rollback, got %v", got)
	}
}
