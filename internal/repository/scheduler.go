package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// SchedulerRepository provides database operations for the scheduler_state table.
type SchedulerRepository struct {
	db *sql.DB
}

// NewSchedulerRepository creates a new SchedulerRepository.
func NewSchedulerRepository(db *sql.DB) *SchedulerRepository {
	return &SchedulerRepository{db: db}
}

// DB returns the underlying database connection.
func (r *SchedulerRepository) DB() *sql.DB {
	return r.db
}

// GetLastSuccess returns the stored timestamp for the given key.
// Returns a zero time.Time and nil error if no row exists.
//
//goland:noinspection ALL
func (r *SchedulerRepository) GetLastSuccess(ctx context.Context, key string) (time.Time, error) {
	query := `SELECT value FROM scheduler_state WHERE key = $1`

	var t time.Time
	err := r.db.QueryRowContext(ctx, query, key).Scan(&t)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("querying scheduler state: %w", err)
	}

	return t, nil
}

// UpdateLastSuccess upserts the row for the given key to NOW().
//
//goland:noinspection ALL
func (r *SchedulerRepository) UpdateLastSuccess(ctx context.Context, tx model.DBTX, key string) error {
	query := `INSERT INTO scheduler_state (key, value)
		VALUES ($1, NOW())
		ON CONFLICT (key) DO UPDATE SET value = NOW()`

	_, err := tx.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("upserting scheduler state: %w", err)
	}

	return nil
}
