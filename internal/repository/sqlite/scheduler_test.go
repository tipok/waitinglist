package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/repository/sqlite"
)

func TestSchedulerRepository_GetLastSuccess_NoRow(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	ctx := context.Background()

	got, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil {
		t.Fatalf("GetLastSuccess returned error: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestSchedulerRepository_UpdateLastSuccess_Insert(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)

	if err := repo.UpdateLastSuccess(ctx, db, "proj1", "waitlist"); err != nil {
		t.Fatalf("UpdateLastSuccess failed: %v", err)
	}

	got, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil {
		t.Fatalf("GetLastSuccess failed: %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero time after UpdateLastSuccess")
	}
	if got.Before(before) {
		t.Errorf("timestamp %v is before test start %v", got, before)
	}
}

func TestSchedulerRepository_UpdateLastSuccess_Upsert(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	ctx := context.Background()

	if err := repo.UpdateLastSuccess(ctx, db, "proj1", "waitlist"); err != nil {
		t.Fatalf("first UpdateLastSuccess failed: %v", err)
	}

	first, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil {
		t.Fatalf("GetLastSuccess after first update failed: %v", err)
	}

	// Sleep 1 second so the timestamp changes (SQLite datetime precision is 1s).
	time.Sleep(1100 * time.Millisecond)

	if err := repo.UpdateLastSuccess(ctx, db, "proj1", "waitlist"); err != nil {
		t.Fatalf("second UpdateLastSuccess failed: %v", err)
	}

	second, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil {
		t.Fatalf("GetLastSuccess after second update failed: %v", err)
	}

	if !second.After(first) {
		t.Errorf("second timestamp %v should be after first %v", second, first)
	}
}

func TestSchedulerRepository_UpdateLastSuccess_DifferentProjectsAndKeys(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	ctx := context.Background()

	if err := repo.UpdateLastSuccess(ctx, db, "proj1", "waitlist"); err != nil {
		t.Fatalf("update proj1/waitlist: %v", err)
	}
	if err := repo.UpdateLastSuccess(ctx, db, "proj2", "waitlist"); err != nil {
		t.Fatalf("update proj2/waitlist: %v", err)
	}

	t1, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil || t1.IsZero() {
		t.Fatalf("proj1/waitlist: time=%v err=%v", t1, err)
	}

	t2, err := repo.GetLastSuccess(ctx, "proj2", "waitlist")
	if err != nil || t2.IsZero() {
		t.Fatalf("proj2/waitlist: time=%v err=%v", t2, err)
	}

	// proj1/digest should still return zero (no row).
	tMissing, err := repo.GetLastSuccess(ctx, "proj1", "digest")
	if err != nil {
		t.Fatalf("proj1/digest error: %v", err)
	}
	if !tMissing.IsZero() {
		t.Errorf("expected zero for missing key, got %v", tMissing)
	}
}

func TestSchedulerRepository_UpdateLastSuccess_TransactionRollback(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	if err := repo.UpdateLastSuccess(ctx, tx, "proj1", "waitlist"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("UpdateLastSuccess in tx: %v", err)
	}

	// Roll back without committing.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	got, err := repo.GetLastSuccess(ctx, "proj1", "waitlist")
	if err != nil {
		t.Fatalf("GetLastSuccess: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero after rollback, got %v", got)
	}
}
