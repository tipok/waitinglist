package repotest

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// RunWaitingListRepositorySuite runs all shared waiting list repository
// behavioral tests against the provided WaitingListRepository.
// userRepo is needed to create users before adding them to the waitlist.
// db is used for transaction tests.
func RunWaitingListRepositorySuite(t *testing.T, wlRepo WaitingListRepository, userRepo UserRepository, db DB) {
	t.Helper()

	t.Run("Add", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "wladd@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user: %v", err)
		}
		entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
		if err != nil {
			t.Fatalf("Add: %v", err)
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
	})

	t.Run("Add_AlreadyOnWaitlist", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "dupwl@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create user: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("first Add: %v", err)
		}
		_, err := wlRepo.Add(ctx, db, "proj1", u.ID)
		if err != model.ErrAlreadyOnWaitingList {
			t.Errorf("expected ErrAlreadyOnWaitingList, got: %v", err)
		}
	})

	t.Run("Add_ForeignKeyViolation", func(t *testing.T) {
		ctx := context.Background()
		_, err := wlRepo.Add(ctx, db, "proj1", "nonexistent-user-id")
		if err != model.ErrWaitingListForeignKey {
			t.Errorf("expected ErrWaitingListForeignKey, got: %v", err)
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "ga1@repotest.com")
		u2 := NewUser("proj1", "ga2@repotest.com")
		for _, u := range []*model.UserEntity{u1, u2} {
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		entries, err := wlRepo.GetAll(ctx, "proj1")
		if err != nil {
			t.Fatalf("GetAll: %v", err)
		}
		if len(entries) < 2 {
			t.Errorf("expected at least 2 entries, got %d", len(entries))
		}
	})

	t.Run("GetWithOffsetLimit", func(t *testing.T) {
		ctx := context.Background()
		for i := 0; i < 3; i++ {
			email := "ol" + string(rune('a'+i)) + "@repotest.com"
			u := NewUser("proj1", email)
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		limit := 2
		offset := 0
		entries, err := wlRepo.GetWithOffsetLimit(ctx, "proj1", &offset, &limit)
		if err != nil {
			t.Fatalf("GetWithOffsetLimit: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("GetEnlistedSince", func(t *testing.T) {
		ctx := context.Background()
		before := time.Now().UTC().Add(-time.Second)

		u := NewUser("proj1", "since-wl@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add: %v", err)
		}

		rows, err := wlRepo.GetEnlistedSince(ctx, "proj1", before)
		if err != nil {
			t.Fatalf("GetEnlistedSince: %v", err)
		}
		if len(rows) < 1 {
			t.Errorf("expected at least 1 row, got %d", len(rows))
		}

		future := time.Now().UTC().Add(time.Hour)
		rows, err = wlRepo.GetEnlistedSince(ctx, "proj1", future)
		if err != nil {
			t.Fatalf("GetEnlistedSince with future: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("expected 0 rows with future since time, got %d", len(rows))
		}
	})

	t.Run("ListAllJoined", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "aj1@repotest.com")
		u2 := NewUser("proj1", "aj2@repotest.com")
		for _, u := range []*model.UserEntity{u1, u2} {
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		rows, err := wlRepo.ListAllJoined(ctx, "proj1")
		if err != nil {
			t.Fatalf("ListAllJoined: %v", err)
		}
		if len(rows) < 2 {
			t.Errorf("expected at least 2 rows, got %d", len(rows))
		}
		if rows[0].Email == "" {
			t.Error("expected email to be populated in joined row")
		}
	})

	t.Run("ListJoined", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "charlie@repotest.com")
		u2 := NewUser("proj1", "delta@repotest.com")
		for _, u := range []*model.UserEntity{u1, u2} {
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}

		rows, err := wlRepo.ListJoined(ctx, "proj1", "", 10, 0)
		if err != nil {
			t.Fatalf("ListJoined: %v", err)
		}
		if len(rows) < 2 {
			t.Errorf("expected at least 2 rows, got %d", len(rows))
		}

		rows, err = wlRepo.ListJoined(ctx, "proj1", "charlie", 10, 0)
		if err != nil {
			t.Fatalf("ListJoined with filter: %v", err)
		}
		if len(rows) != 1 {
			t.Errorf("expected 1 row matching 'charlie', got %d", len(rows))
		}
		if rows[0].Email != "charlie@repotest.com" {
			t.Errorf("expected charlie@repotest.com, got %s", rows[0].Email)
		}
	})

	t.Run("ListJoined_AllProjects", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "p1crossuser@repotest.com")
		u2 := NewUser("proj2", "p2crossuser@repotest.com")
		for _, u := range []*model.UserEntity{u1, u2} {
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if _, err := wlRepo.Add(ctx, db, u.ProjectSlug, u.ID); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		rows, err := wlRepo.ListJoined(ctx, "", "", 100, 0)
		if err != nil {
			t.Fatalf("ListJoined all projects: %v", err)
		}
		if len(rows) < 2 {
			t.Errorf("expected at least 2 rows across all projects, got %d", len(rows))
		}
	})

	t.Run("DeleteByIDs", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "del1@repotest.com")
		u2 := NewUser("proj1", "del2@repotest.com")
		var entryIDs []string
		for _, u := range []*model.UserEntity{u1, u2} {
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			entryIDs = append(entryIDs, entry.ID)
		}
		if err := wlRepo.DeleteByIDs(ctx, entryIDs); err != nil {
			t.Fatalf("DeleteByIDs: %v", err)
		}
		// Verify entries are gone by checking they're no longer returned for these user IDs.
		for _, u := range []*model.UserEntity{u1, u2} {
			all, err := wlRepo.GetAll(ctx, "proj1")
			if err != nil {
				t.Fatalf("GetAll: %v", err)
			}
			for _, e := range all {
				if e.UserID == u.ID {
					t.Errorf("expected entry for user %s to be deleted", u.ID)
				}
			}
		}
	})

	t.Run("DeleteByIDs_Empty", func(t *testing.T) {
		ctx := context.Background()
		if err := wlRepo.DeleteByIDs(ctx, nil); err != nil {
			t.Errorf("DeleteByIDs(nil) should be no-op, got: %v", err)
		}
	})

	t.Run("DeleteByID", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "delbyid@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		entry, err := wlRepo.Add(ctx, db, "proj1", u.ID)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := wlRepo.DeleteByID(ctx, entry.ID); err != nil {
			t.Fatalf("DeleteByID: %v", err)
		}
		all, err := wlRepo.GetAll(ctx, "proj1")
		if err != nil {
			t.Fatalf("GetAll: %v", err)
		}
		for _, e := range all {
			if e.ID == entry.ID {
				t.Error("expected entry to be deleted")
			}
		}
	})

	t.Run("DeleteByID_NotFound", func(t *testing.T) {
		ctx := context.Background()
		err := wlRepo.DeleteByID(ctx, "nonexistent-entry-id")
		if err != model.ErrWaitingListEntryNotFound {
			t.Errorf("expected ErrWaitingListEntryNotFound, got: %v", err)
		}
	})

	t.Run("DeleteByUserID", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "delbyuid@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := wlRepo.Add(ctx, db, "proj1", u.ID); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := wlRepo.DeleteByUserID(ctx, u.ID); err != nil {
			t.Fatalf("DeleteByUserID: %v", err)
		}
		all, err := wlRepo.GetAll(ctx, "proj1")
		if err != nil {
			t.Fatalf("GetAll: %v", err)
		}
		for _, e := range all {
			if e.UserID == u.ID {
				t.Error("expected user's waitlist entry to be deleted")
			}
		}
	})

	t.Run("DeleteByUserID_NotOnList", func(t *testing.T) {
		ctx := context.Background()
		if err := wlRepo.DeleteByUserID(ctx, "any-user-id"); err != nil {
			t.Errorf("DeleteByUserID on non-existent user should be no-op, got: %v", err)
		}
	})

	t.Run("BeginTx_CommitRollback", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "tx-wl@repotest.com")
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// Test rollback: entry should not persist.
		tx, err := wlRepo.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		if _, err := wlRepo.Add(ctx, tx, "proj1", u.ID); err != nil {
			t.Fatalf("Add in tx: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		all, err := wlRepo.GetAll(ctx, "proj1")
		if err != nil {
			t.Fatalf("GetAll after rollback: %v", err)
		}
		for _, e := range all {
			if e.UserID == u.ID {
				t.Error("expected entry to be absent after rollback")
			}
		}

		// Test commit: entry should persist.
		tx2, err := wlRepo.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx (commit): %v", err)
		}
		if _, err := wlRepo.Add(ctx, tx2, "proj1", u.ID); err != nil {
			t.Fatalf("Add in tx2: %v", err)
		}
		if err := tx2.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		all, err = wlRepo.GetAll(ctx, "proj1")
		if err != nil {
			t.Fatalf("GetAll after commit: %v", err)
		}
		found := false
		for _, e := range all {
			if e.UserID == u.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected entry to persist after commit")
		}
	})
}
