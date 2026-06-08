package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// SchedulerRepository provides SQLite database operations for the scheduler_state table.
type SchedulerRepository struct {
	db *sql.DB
}

// NewSchedulerRepository creates a new SQLite SchedulerRepository.
func NewSchedulerRepository(db *sql.DB) *SchedulerRepository {
	return &SchedulerRepository{db: db}
}

// GetLastSuccess returns the stored timestamp for the given key scoped to a
// project. Returns a zero time.Time and nil error if no row exists.
//
//goland:noinspection ALL
func (r *SchedulerRepository) GetLastSuccess(ctx context.Context, projectSlug, key string) (time.Time, error) {
	query := `SELECT value FROM scheduler_state WHERE project_slug = ? AND key = ?`

	var raw string
	err := r.db.QueryRowContext(ctx, query, projectSlug, key).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("querying scheduler state: %w", err)
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing scheduler state timestamp %q: %w", raw, err)
	}
	return t, nil
}

// UpdateLastSuccess upserts the row for the given key and project to the current UTC time.
//
//goland:noinspection ALL
func (r *SchedulerRepository) UpdateLastSuccess(ctx context.Context, tx model.DBTX, projectSlug, key string) error {
	query := `INSERT INTO scheduler_state (project_slug, key, value)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT (project_slug, key) DO UPDATE SET value = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`

	_, err := tx.ExecContext(ctx, query, projectSlug, key)
	if err != nil {
		return fmt.Errorf("upserting scheduler state: %w", err)
	}

	return nil
}
