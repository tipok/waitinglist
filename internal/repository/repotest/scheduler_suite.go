package repotest

import (
	"context"
	"testing"
	"time"
)

// RunSchedulerRepositorySuite runs all shared scheduler repository behavioral
// tests against the provided SchedulerRepository.
// db is used for transaction tests.
// sleepBetweenUpserts controls how long to wait between two UpdateLastSuccess
// calls to ensure the timestamp changes (SQLite has 1s precision).
func RunSchedulerRepositorySuite(t *testing.T, repo SchedulerRepository, db DB, sleepBetweenUpserts time.Duration) {
	t.Helper()

	t.Run("GetLastSuccess_NoRow", func(t *testing.T) {
		ctx := context.Background()
		got, err := repo.GetLastSuccess(ctx, "proj1", "nonexistent_key")
		if err != nil {
			t.Fatalf("GetLastSuccess: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected zero time for missing key, got %v", got)
		}
	})

	t.Run("UpdateLastSuccess_Insert", func(t *testing.T) {
		ctx := context.Background()
		before := time.Now().UTC().Add(-time.Second)

		if err := repo.UpdateLastSuccess(ctx, db, "proj1", "insert_key"); err != nil {
			t.Fatalf("UpdateLastSuccess: %v", err)
		}

		got, err := repo.GetLastSuccess(ctx, "proj1", "insert_key")
		if err != nil {
			t.Fatalf("GetLastSuccess: %v", err)
		}
		if got.IsZero() {
			t.Fatal("expected non-zero time after UpdateLastSuccess")
		}
		if got.Before(before) {
			t.Errorf("timestamp %v is before test start %v", got, before)
		}
	})

	t.Run("UpdateLastSuccess_Upsert", func(t *testing.T) {
		ctx := context.Background()

		if err := repo.UpdateLastSuccess(ctx, db, "proj1", "upsert_key"); err != nil {
			t.Fatalf("first UpdateLastSuccess: %v", err)
		}

		first, err := repo.GetLastSuccess(ctx, "proj1", "upsert_key")
		if err != nil {
			t.Fatalf("GetLastSuccess after first: %v", err)
		}

		if sleepBetweenUpserts > 0 {
			time.Sleep(sleepBetweenUpserts)
		}

		if err := repo.UpdateLastSuccess(ctx, db, "proj1", "upsert_key"); err != nil {
			t.Fatalf("second UpdateLastSuccess: %v", err)
		}

		second, err := repo.GetLastSuccess(ctx, "proj1", "upsert_key")
		if err != nil {
			t.Fatalf("GetLastSuccess after second: %v", err)
		}

		if !second.After(first) {
			t.Errorf("second timestamp %v should be after first %v", second, first)
		}
	})

	t.Run("UpdateLastSuccess_DifferentProjectsAndKeys", func(t *testing.T) {
		ctx := context.Background()

		if err := repo.UpdateLastSuccess(ctx, db, "proj1", "multi_key"); err != nil {
			t.Fatalf("update proj1: %v", err)
		}
		if err := repo.UpdateLastSuccess(ctx, db, "proj2", "multi_key"); err != nil {
			t.Fatalf("update proj2: %v", err)
		}

		t1, err := repo.GetLastSuccess(ctx, "proj1", "multi_key")
		if err != nil || t1.IsZero() {
			t.Fatalf("proj1/multi_key: time=%v err=%v", t1, err)
		}
		t2, err := repo.GetLastSuccess(ctx, "proj2", "multi_key")
		if err != nil || t2.IsZero() {
			t.Fatalf("proj2/multi_key: time=%v err=%v", t2, err)
		}

		tMissing, err := repo.GetLastSuccess(ctx, "proj1", "missing_key")
		if err != nil {
			t.Fatalf("proj1/missing_key error: %v", err)
		}
		if !tMissing.IsZero() {
			t.Errorf("expected zero for missing key, got %v", tMissing)
		}
	})

	t.Run("UpdateLastSuccess_TransactionRollback", func(t *testing.T) {
		ctx := context.Background()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		if err := repo.UpdateLastSuccess(ctx, tx, "proj1", "rollback_key"); err != nil {
			_ = tx.Rollback()
			t.Fatalf("UpdateLastSuccess in tx: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}

		got, err := repo.GetLastSuccess(ctx, "proj1", "rollback_key")
		if err != nil {
			t.Fatalf("GetLastSuccess: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected zero after rollback, got %v", got)
		}
	})
}
