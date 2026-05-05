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
	query := `SELECT id, user_id, created_at, weighted_created_at
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
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.CreatedAt, &entry.WeightedCreatedAt); err != nil {
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

// ListJoined returns waiting-list rows joined to their user, optionally
// filtered by a case-insensitive email substring. Ordered by
// weighted_created_at ascending (queue order) then by email for stability.
//
//goland:noinspection ALL
func (r *WaitingListRepository) ListJoined(ctx context.Context, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error) {
	const query = `
		SELECT wl.id, wl.user_id, ue.email, ue.firstname, ue.lastname,
		       wl.weight, wl.created_at, wl.weighted_created_at
		FROM   waiting_list wl
		JOIN   user_entity  ue ON ue.id = wl.user_id
		WHERE  ($1 = '' OR ue.email ILIKE '%' || $1 || '%')
		ORDER  BY wl.weighted_created_at ASC, ue.email ASC
		LIMIT  $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, emailLike, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing waiting list joined: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]model.WaitingListAdminRow, 0, limit)
	for rows.Next() {
		var row model.WaitingListAdminRow
		if err := rows.Scan(
			&row.EntryID, &row.UserID, &row.Email, &row.Firstname, &row.Lastname,
			&row.Weight, &row.CreatedAt, &row.WeightedCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning waiting list joined row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating waiting list joined rows: %w", err)
	}
	return out, nil
}

// DeleteByID removes a single waiting-list row by its primary key. Returns
// model.ErrWaitingListEntryNotFound if no row matched.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByID(ctx context.Context, id string) error {
	return r.DeleteByIDTx(ctx, r.db, id)
}

// DeleteByIDTx is the transactional form of DeleteByID.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByIDTx(ctx context.Context, tx model.DBTX, id string) error {
	const query = `DELETE FROM waiting_list WHERE id = $1`
	res, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deleting waiting list entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return model.ErrWaitingListEntryNotFound
	}
	return nil
}

// DeleteByUserID removes the waiting-list row(s) belonging to the given
// user. Used by the admin grant-access flow. Unlike DeleteByID, missing
// rows are not an error: the user may simply not be on the waiting list.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByUserID(ctx context.Context, userID string) error {
	return r.DeleteByUserIDTx(ctx, r.db, userID)
}

// DeleteByUserIDTx is the transactional form of DeleteByUserID.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error {
	const query = `DELETE FROM waiting_list WHERE user_id = $1`
	if _, err := tx.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("deleting waiting list by user id: %w", err)
	}
	return nil
}
