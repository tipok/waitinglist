package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/repository/repotest"
	"github.com/tipok/waitinglist/internal/repository/sqlite"
)

func TestUserRepository_ParitySuite(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewUserRepository(db)

	insertWL := func(t *testing.T, userID, projectSlug string) {
		t.Helper()
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO waiting_list (id, user_id, project_slug) VALUES (?, ?, ?)`,
			"wl-parity-"+userID, userID, projectSlug,
		)
		if err != nil {
			t.Fatalf("inserting waiting list row: %v", err)
		}
	}

	repotest.RunUserRepositorySuite(t, repo, insertWL)
}

func TestWaitingListRepository_ParitySuite(t *testing.T) {
	db := newTestDB(t)
	wlRepo := sqlite.NewWaitingListRepository(db)
	userRepo := sqlite.NewUserRepository(db)
	repotest.RunWaitingListRepositorySuite(t, wlRepo, userRepo, db)
}

func TestSchedulerRepository_ParitySuite(t *testing.T) {
	db := newTestDB(t)
	repo := sqlite.NewSchedulerRepository(db)
	// SQLite datetime('now') has 1-second precision, so we need > 1s between upserts.
	repotest.RunSchedulerRepositorySuite(t, repo, db, 1100*time.Millisecond)
}
