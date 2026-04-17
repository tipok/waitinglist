package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"github.com/tipok/waitinglist/internal/logger"

	"github.com/tipok/waitinglist/internal/model"
)

// WaitingListRepository provides database operations for the waiting_list table.
type WaitingListRepository struct {
	db *sql.DB
}

// NewWaitingListRepository creates a new WaitingListRepository.
func NewWaitingListRepository(db *sql.DB) *WaitingListRepository {
	return &WaitingListRepository{db: db}
}

// Add inserts a new entry into the waiting_list table for the given user ID.
// Returns model.ErrAlreadyOnWaitingList if the user is already on the list.
// Returns model.ErrWaitingListForeignKey if the user ID does not exist.
//
//goland:noinspection ALL
func (r *WaitingListRepository) Add(ctx context.Context, tx model.DBTX, userID string) (*model.WaitingListEntry, error) {
	query := `INSERT INTO waiting_list (user_id)
		VALUES ($1)
		RETURNING id, user_id, created_at`

	entry := &model.WaitingListEntry{}
	err := tx.QueryRowContext(ctx, query, userID).
		Scan(&entry.ID, &entry.UserID, &entry.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case "23505":
				return nil, model.ErrAlreadyOnWaitingList
			case "23503":
				return nil, model.ErrWaitingListForeignKey
			}
		}
		return nil, fmt.Errorf("inserting waiting list entry: %w", err)
	}

	return entry, nil
}

// GetAll returns all waiting list entries ordered by created_at ascending.
func (r *WaitingListRepository) GetAll(ctx context.Context) ([]model.WaitingListEntry, error) {
	return r.GetWithOffsetLimit(ctx, nil, nil)
}

//goland:noinspection ALL
func (r *WaitingListRepository) GetWithOffsetLimit(ctx context.Context, offset, limit *int) ([]model.WaitingListEntry, error) {
	query := `SELECT id, user_id, created_at
		FROM waiting_list
		ORDER BY weighted_created_at ASC`

	if offset != nil {
		query += fmt.Sprintf(" OFFSET %d", *offset)
	}

	if limit != nil {
		query += fmt.Sprintf(" LIMIT %d", *limit)
	}

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying waiting list: %w", err)
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			logger.NewLogger().Error("Error closing waiting list rows", "error", err)
		}
	}()

	entries := make([]model.WaitingListEntry, 0)
	for rows.Next() {
		var entry model.WaitingListEntry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning waiting list entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating waiting list rows: %w", err)
	}

	return entries, nil
}

// DeleteByIDs deletes waiting list entries with the given IDs.
// Returns nil without executing a query if the slice is empty.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByIDs(ctx context.Context, ids []string) error {
	return r.DeleteByIDsTx(ctx, r.db, ids)
}

// DeleteByIDsTx deletes waiting list entries with the given IDs using the given DBTX (transaction or DB).
// Returns nil without executing a query if the slice is empty.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByIDsTx(ctx context.Context, tx model.DBTX, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	query := `DELETE FROM waiting_list WHERE id = ANY($1)`

	_, err := tx.ExecContext(ctx, query, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("deleting waiting list entries: %w", err)
	}

	return nil
}

// BeginTx starts a new database transaction.
func (r *WaitingListRepository) BeginTx(ctx context.Context) (model.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
