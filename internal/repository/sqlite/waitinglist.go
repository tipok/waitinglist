package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tipok/waitinglist/internal/model"
)

const waitingListAdminSelectColumns = `wl.id, wl.project_slug, wl.user_id, ue.email, ue.firstname, ue.lastname,
	wl.weight, wl.created_at, wl.weighted_created_at`

// scanWaitingListAdminRow scans a joined waiting_list + user_entity row.
func scanWaitingListAdminRow(scanner interface {
	Scan(dest ...any) error
}, row *model.WaitingListAdminRow) error {
	return scanner.Scan(
		&row.EntryID, &row.ProjectSlug, &row.UserID, &row.Email, &row.Firstname, &row.Lastname,
		&row.Weight, &timeScanner{&row.CreatedAt}, &timeScanner{&row.WeightedCreatedAt},
	)
}

// WaitingListRepository provides SQLite database operations for the waiting_list table.
type WaitingListRepository struct {
	db *sql.DB
}

// NewWaitingListRepository creates a new SQLite WaitingListRepository.
func NewWaitingListRepository(db *sql.DB) *WaitingListRepository {
	return &WaitingListRepository{db: db}
}

// Add inserts a new entry into the waiting_list table. Returns
// model.ErrAlreadyOnWaitingList if the user is already on the list.
// Returns model.ErrWaitingListForeignKey if the user ID does not exist.
//
//goland:noinspection ALL
func (r *WaitingListRepository) Add(ctx context.Context, tx model.DBTX, projectSlug, userID string) (*model.WaitingListEntry, error) {
	id := uuid.New().String()
	query := `INSERT INTO waiting_list (id, project_slug, user_id)
		VALUES (?, ?, ?)
		RETURNING id, project_slug, user_id, created_at`

	entry := &model.WaitingListEntry{}
	err := tx.QueryRowContext(ctx, query, id, projectSlug, userID).
		Scan(&entry.ID, &entry.ProjectSlug, &entry.UserID, &timeScanner{&entry.CreatedAt})
	if err != nil {
		if isSQLiteUniqueViolation(err) {
			return nil, model.ErrAlreadyOnWaitingList
		}
		if isSQLiteForeignKeyViolation(err) {
			return nil, model.ErrWaitingListForeignKey
		}
		return nil, fmt.Errorf("inserting waiting list entry: %w", err)
	}

	return entry, nil
}

// GetAll returns all waiting list entries ordered by weighted_created_at ascending.
func (r *WaitingListRepository) GetAll(ctx context.Context, projectSlug string) ([]model.WaitingListEntry, error) {
	return r.GetWithOffsetLimit(ctx, projectSlug, nil, nil)
}

// GetWithOffsetLimit returns waiting list entries with optional limit/offset pagination.
//
//goland:noinspection ALL
func (r *WaitingListRepository) GetWithOffsetLimit(ctx context.Context, projectSlug string, offset, limit *int) ([]model.WaitingListEntry, error) {
	//goland:noinspection ALL
	const query = `SELECT id, project_slug, user_id, created_at, weighted_created_at
		FROM waiting_list
		WHERE project_slug = ?
		ORDER BY weighted_created_at ASC
		LIMIT ? OFFSET ?`

	effectiveLimit := 2147483647
	effectiveOffset := 0
	if limit != nil {
		effectiveLimit = *limit
	}
	if offset != nil {
		effectiveOffset = *offset
	}

	rows, err := r.db.QueryContext(ctx, query, projectSlug, effectiveLimit, effectiveOffset)
	if err != nil {
		return nil, fmt.Errorf("querying waiting list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]model.WaitingListEntry, 0)
	for rows.Next() {
		var entry model.WaitingListEntry
		if err := rows.Scan(
			&entry.ID, &entry.ProjectSlug, &entry.UserID,
			&timeScanner{&entry.CreatedAt}, &timeScanner{&entry.WeightedCreatedAt},
		); err != nil {
			return nil, fmt.Errorf("scanning waiting list entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating waiting list rows: %w", err)
	}

	return entries, nil
}

// GetEnlistedSince returns waiting-list entries joined with user data created
// after the given timestamp, scoped to a project.
//
//goland:noinspection ALL
func (r *WaitingListRepository) GetEnlistedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.WaitingListAdminRow, error) {
	//goland:noinspection ALL
	const query = `
		SELECT ` + waitingListAdminSelectColumns + `
		FROM   waiting_list wl
		JOIN   user_entity  ue ON ue.id = wl.user_id
		WHERE  wl.project_slug = ? AND wl.created_at > ?
		ORDER  BY wl.created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, projectSlug, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("querying enlisted since: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []model.WaitingListAdminRow
	for rows.Next() {
		var row model.WaitingListAdminRow
		if err := scanWaitingListAdminRow(rows, &row); err != nil {
			return nil, fmt.Errorf("scanning enlisted row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListAllJoined returns all waiting-list entries joined with user data for the
// given project, with no pagination limit.
//
//goland:noinspection ALL
func (r *WaitingListRepository) ListAllJoined(ctx context.Context, projectSlug string) ([]model.WaitingListAdminRow, error) {
	//goland:noinspection ALL
	const query = `
		SELECT ` + waitingListAdminSelectColumns + `
		FROM   waiting_list wl
		JOIN   user_entity  ue ON ue.id = wl.user_id
		WHERE  wl.project_slug = ?
		ORDER  BY wl.weighted_created_at ASC, ue.email ASC`

	rows, err := r.db.QueryContext(ctx, query, projectSlug)
	if err != nil {
		return nil, fmt.Errorf("listing all waiting list joined: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []model.WaitingListAdminRow
	for rows.Next() {
		var row model.WaitingListAdminRow
		if err := scanWaitingListAdminRow(rows, &row); err != nil {
			return nil, fmt.Errorf("scanning waiting list joined row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListJoined returns waiting-list rows joined to their user, optionally
// filtered by a case-insensitive email substring and/or project. Ordered by
// weighted_created_at ascending then by email for stability.
// When projectSlug is empty, rows from all projects are returned.
//
//goland:noinspection ALL
func (r *WaitingListRepository) ListJoined(ctx context.Context, projectSlug, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error) {
	var sqlRows *sql.Rows
	var err error

	if projectSlug == "" {
		//goland:noinspection ALL
		const query = `
			SELECT ` + waitingListAdminSelectColumns + `
			FROM   waiting_list wl
			JOIN   user_entity  ue ON ue.id = wl.user_id
			WHERE  (? = '' OR ue.email LIKE '%' || ? || '%')
			ORDER  BY wl.weighted_created_at ASC, ue.email ASC
			LIMIT  ? OFFSET ?`
		sqlRows, err = r.db.QueryContext(ctx, query, emailLike, emailLike, limit, offset)
	} else {
		//goland:noinspection ALL
		const query = `
			SELECT ` + waitingListAdminSelectColumns + `
			FROM   waiting_list wl
			JOIN   user_entity  ue ON ue.id = wl.user_id
			WHERE  wl.project_slug = ?
			  AND  (? = '' OR ue.email LIKE '%' || ? || '%')
			ORDER  BY wl.weighted_created_at ASC, ue.email ASC
			LIMIT  ? OFFSET ?`
		sqlRows, err = r.db.QueryContext(ctx, query, projectSlug, emailLike, emailLike, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("listing waiting list joined: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	out := make([]model.WaitingListAdminRow, 0, limit)
	for sqlRows.Next() {
		var row model.WaitingListAdminRow
		if err := scanWaitingListAdminRow(sqlRows, &row); err != nil {
			return nil, fmt.Errorf("scanning waiting list joined row: %w", err)
		}
		out = append(out, row)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating waiting list joined rows: %w", err)
	}
	return out, nil
}

// DeleteByIDs deletes waiting list entries with the given IDs.
// Returns nil without executing a query if the slice is empty.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByIDs(ctx context.Context, ids []string) error {
	return r.DeleteByIDsTx(ctx, r.db, ids)
}

// DeleteByIDsTx deletes waiting list entries with the given IDs using the given DBTX.
// Returns nil without executing a query if the slice is empty.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByIDsTx(ctx context.Context, tx model.DBTX, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders, args := buildInPlaceholders(ids)
	query := `DELETE FROM waiting_list WHERE id IN (` + placeholders + `)`

	_, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting waiting list entries: %w", err)
	}

	return nil
}

// BeginTx starts a new database transaction.
func (r *WaitingListRepository) BeginTx(ctx context.Context) (model.Tx, error) {
	return r.db.BeginTx(ctx, nil)
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
	const query = `DELETE FROM waiting_list WHERE id = ?`
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

// DeleteByUserID removes the waiting-list row(s) belonging to the given user.
// Missing rows are not an error: the user may simply not be on the waiting list.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByUserID(ctx context.Context, userID string) error {
	return r.DeleteByUserIDTx(ctx, r.db, userID)
}

// DeleteByUserIDTx is the transactional form of DeleteByUserID.
//
//goland:noinspection ALL
func (r *WaitingListRepository) DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error {
	const query = `DELETE FROM waiting_list WHERE user_id = ?`
	if _, err := tx.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("deleting waiting list by user id: %w", err)
	}
	return nil
}
