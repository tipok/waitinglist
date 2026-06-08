package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/repository/repotest"
)

func TestUserRepository_ParitySuite(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	insertWL := func(t *testing.T, userID, projectSlug string) {
		t.Helper()
		//goland:noinspection ALL
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO waiting_list (user_id, project_slug) VALUES ($1, $2)`,
			userID, projectSlug,
		)
		if err != nil {
			t.Fatalf("inserting waiting list row: %v", err)
		}
	}

	repotest.RunUserRepositorySuite(t, repo, insertWL)
}

func TestWaitingListRepository_ParitySuite(t *testing.T) {
	db := setupTestDB(t)
	wlRepo := NewWaitingListRepository(db)
	userRepo := NewUserRepository(db)
	repotest.RunWaitingListRepositorySuite(t, wlRepo, userRepo, db)
}

func TestSchedulerRepository_ParitySuite(t *testing.T) {
	db := setupTestDB(t)
	repo := NewSchedulerRepository(db)
	// PostgreSQL timestamps have sub-millisecond precision; 10ms is enough.
	repotest.RunSchedulerRepositorySuite(t, repo, db, 10*time.Millisecond)
}
