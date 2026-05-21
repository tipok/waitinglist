package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/tipok/waitinglist/internal/model"
)

// validGrantSources is the set of allowed access_granted_by values. Update
// the migration 007 CHECK constraint in lockstep with this set.
var validGrantSources = map[string]struct{}{
	"scheduler": {},
	"admin":     {},
}

// userSelectColumns is the canonical column list for SELECTs that hydrate a
// full *model.UserEntity.
const userSelectColumns = `id, project_id, firstname, lastname, email, has_access, created_at, ip_address,
	access_granted_at, access_granted_by, access_revoked_at, access_revoked_by, access_revoke_reason`

// UserRepository provides database operations for the user_entity table.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user into the user_entity table and populates the
// generated ID on the provided UserEntity. Returns model.ErrDuplicateEmail
// if the email already exists.
//
//goland:noinspection ALL
func (r *UserRepository) Create(ctx context.Context, user *model.UserEntity) error {
	return r.CreateTx(ctx, r.db, user)
}

// CreateTx inserts a new user using the given DBTX (transaction or DB).
//
//goland:noinspection ALL
func (r *UserRepository) CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error {
	query := `INSERT INTO user_entity (project_id, firstname, lastname, email, ip_address)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, has_access, created_at`

	err := tx.QueryRowContext(ctx, query, user.ProjectID, user.Firstname, user.Lastname, user.Email, user.IPAddress).
		Scan(&user.ID, &user.HasAccess, &user.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return model.ErrDuplicateEmail
		}
		return fmt.Errorf("inserting user: %w", err)
	}

	return nil
}

// GetByEmail retrieves a user by email address within a project. Returns
// model.ErrUserNotFound if no user with the given email exists.
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmail(ctx context.Context, projectID, email string) (*model.UserEntity, error) {
	return r.GetByEmailTx(ctx, r.db, projectID, email)
}

// GetByEmailTx retrieves a user by email scoped to a project using the given
// DBTX (transaction or DB).
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmailTx(ctx context.Context, tx model.DBTX, projectID, email string) (*model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE project_id = $1 AND email = $2`

	user := &model.UserEntity{}
	err := tx.QueryRowContext(ctx, query, projectID, email).Scan(
		&user.ID, &user.ProjectID, &user.Firstname, &user.Lastname, &user.Email,
		&user.HasAccess, &user.CreatedAt, &user.IPAddress,
		&user.AccessGrantedAt, &user.AccessGrantedBy,
		&user.AccessRevokedAt, &user.AccessRevokedBy, &user.AccessRevokeReason,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by email: %w", err)
	}

	return user, nil
}

// GetUserInfoByEmails retrieves user information for the given email addresses
// within a project. Returns an empty slice when no matching users are found.
//
//goland:noinspection ALL
func (r *UserRepository) GetUserInfoByEmails(ctx context.Context, projectID string, emails []string) ([]model.UserInfo, error) {
	if len(emails) == 0 {
		return []model.UserInfo{}, nil
	}

	//goland:noinspection ALL
	query := `SELECT project_id, firstname, lastname, email, has_access, created_at,
			access_granted_at, access_granted_by, access_revoked_at, access_revoke_reason
		FROM user_entity
		WHERE project_id = $1 AND email = ANY($2)`

	rows, err := r.db.QueryContext(ctx, query, projectID, pq.Array(emails))
	if err != nil {
		return nil, fmt.Errorf("querying users by emails: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	users := make([]model.UserInfo, 0)
	for rows.Next() {
		var u model.UserInfo
		if err := rows.Scan(
			&u.ProjectID, &u.Firstname, &u.Lastname, &u.Email, &u.HasAccess, &u.CreatedAt,
			&u.AccessGrantedAt, &u.AccessGrantedBy, &u.AccessRevokedAt, &u.AccessRevokeReason,
		); err != nil {
			return nil, fmt.Errorf("scanning user info: %w", err)
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating user info rows: %w", err)
	}

	return users, nil
}

// SetHasAccess sets has_access to true for the users with the given IDs.
// Returns model.ErrUserNotFound if none of the given IDs match any rows.
//
// Deprecated: use GrantAccess (or GrantAccessTx) so the audit columns
// (access_granted_at, access_granted_by) are populated. This wrapper exists
// only so the existing scheduler call site continues to compile during the
// plan-16 transition.
//
//goland:noinspection ALL
func (r *UserRepository) SetHasAccess(ctx context.Context, ids []string) error {
	return r.GrantAccessTx(ctx, r.db, ids, "scheduler")
}

// SetHasAccessTx is the transactional form of SetHasAccess.
//
// Deprecated: use GrantAccessTx(ctx, tx, ids, "scheduler") directly.
//
//goland:noinspection ALL
func (r *UserRepository) SetHasAccessTx(ctx context.Context, tx model.DBTX, ids []string) error {
	return r.GrantAccessTx(ctx, tx, ids, "scheduler")
}

// GrantAccess flips has_access to true for the given user IDs and records
// the grant timestamp/source.
func (r *UserRepository) GrantAccess(ctx context.Context, ids []string, source string) error {
	return r.GrantAccessTx(ctx, r.db, ids, source)
}

// GrantAccessTx flips has_access to true for the given user IDs, recording
// the grant timestamp and source ('scheduler' | 'admin'). Any prior
// revocation columns are cleared so re-granting access leaves the audit
// state consistent.
//
// Returns model.ErrUserNotFound if none of the given IDs match any rows.
// An empty `ids` slice is a no-op.
//
//goland:noinspection ALL
func (r *UserRepository) GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error {
	if _, ok := validGrantSources[source]; !ok {
		return fmt.Errorf("invalid grant source %q", source)
	}
	if len(ids) == 0 {
		return nil
	}

	query := `UPDATE user_entity
		SET    has_access           = TRUE,
		       access_granted_at    = NOW(),
		       access_granted_by    = $1,
		       access_revoked_at    = NULL,
		       access_revoked_by    = NULL,
		       access_revoke_reason = NULL
		WHERE  id = ANY($2)`

	result, err := tx.ExecContext(ctx, query, source, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("granting access: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return model.ErrUserNotFound
	}

	return nil
}

// GetByID returns a user by primary key. Returns model.ErrUserNotFound if
// no row matches.
//
//goland:noinspection ALL
func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE id = $1`

	user := &model.UserEntity{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.ProjectID, &user.Firstname, &user.Lastname, &user.Email,
		&user.HasAccess, &user.CreatedAt, &user.IPAddress,
		&user.AccessGrantedAt, &user.AccessGrantedBy,
		&user.AccessRevokedAt, &user.AccessRevokedBy, &user.AccessRevokeReason,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return user, nil
}

// CountByAccess returns (waitListCount, withAccessCount) — the number of
// users currently on the waiting list and the number of users who currently
// have access. When projectID is non-empty, counts are scoped to that project.
//
//goland:noinspection ALL
func (r *UserRepository) CountByAccess(ctx context.Context, projectID string) (int, int, error) {
	var waitListCount, withAccessCount int

	if projectID == "" {
		const query = `
			SELECT
				(SELECT COUNT(*) FROM waiting_list)                       AS waitlist_count,
				(SELECT COUNT(*) FROM user_entity WHERE has_access = TRUE) AS access_count`
		if err := r.db.QueryRowContext(ctx, query).Scan(&waitListCount, &withAccessCount); err != nil {
			return 0, 0, fmt.Errorf("counting users by access: %w", err)
		}
	} else {
		const query = `
			SELECT
				(SELECT COUNT(*) FROM waiting_list WHERE project_id = $1)                       AS waitlist_count,
				(SELECT COUNT(*) FROM user_entity WHERE has_access = TRUE AND project_id = $1) AS access_count`
		if err := r.db.QueryRowContext(ctx, query, projectID).Scan(&waitListCount, &withAccessCount); err != nil {
			return 0, 0, fmt.Errorf("counting users by access: %w", err)
		}
	}
	return waitListCount, withAccessCount, nil
}

// EnlistmentsByDay returns one DayCount per UTC day in the last `days`
// days, ascending by day. Days with no signups are zero-filled. `days` is
// clamped to [1, 365]. When projectID is non-empty, results are scoped to
// that project.
//
//goland:noinspection ALL
func (r *UserRepository) EnlistmentsByDay(ctx context.Context, projectID string, days int) ([]model.DayCount, error) {
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}

	var rows *sql.Rows
	var err error

	if projectID == "" {
		//goland:noinspection ALL
		const query = `
			SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS day,
			       COUNT(*) AS count
			FROM   user_entity
			WHERE  created_at >= NOW() - ($1 || ' days')::interval
			GROUP  BY 1
			ORDER  BY 1`
		rows, err = r.db.QueryContext(ctx, query, days)
	} else {
		//goland:noinspection ALL
		const query = `
			SELECT to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS day,
			       COUNT(*) AS count
			FROM   user_entity
			WHERE  project_id = $1 AND created_at >= NOW() - ($2 || ' days')::interval
			GROUP  BY 1
			ORDER  BY 1`
		rows, err = r.db.QueryContext(ctx, query, projectID, days)
	}
	if err != nil {
		return nil, fmt.Errorf("querying enlistments by day: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	got := make(map[string]int, days)
	for rows.Next() {
		var d model.DayCount
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return nil, fmt.Errorf("scanning day count: %w", err)
		}
		got[d.Day] = d.Count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating day count rows: %w", err)
	}

	out := make([]model.DayCount, 0, days)
	now := time.Now().UTC()
	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, model.DayCount{Day: day, Count: got[day]})
	}
	return out, nil
}

// ListWithAccess returns users where has_access = true, optionally filtered
// by a case-insensitive email substring and/or project. Pagination via
// limit/offset. The caller is responsible for clamping limit/offset to sane
// values. When projectID is empty, users from all projects are returned.
//
//goland:noinspection ALL
func (r *UserRepository) ListWithAccess(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM   user_entity
		WHERE  has_access = TRUE
		  AND  ($1 = '' OR project_id = $1::uuid)
		  AND  ($2 = '' OR email ILIKE '%' || $2 || '%')
		ORDER  BY access_granted_at DESC NULLS LAST, email ASC
		LIMIT  $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, projectID, emailLike, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing users with access: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	users := make([]model.UserEntity, 0, limit)
	for rows.Next() {
		var u model.UserEntity
		if err := rows.Scan(
			&u.ID, &u.ProjectID, &u.Firstname, &u.Lastname, &u.Email,
			&u.HasAccess, &u.CreatedAt, &u.IPAddress,
			&u.AccessGrantedAt, &u.AccessGrantedBy,
			&u.AccessRevokedAt, &u.AccessRevokedBy, &u.AccessRevokeReason,
		); err != nil {
			return nil, fmt.Errorf("scanning user with access: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users with access: %w", err)
	}
	return users, nil
}

// RevokeAccess flips has_access to false for one user and records the
// revocation timestamp, admin identifier, and reason.
func (r *UserRepository) RevokeAccess(ctx context.Context, id, reason, by string) error {
	return r.RevokeAccessTx(ctx, r.db, id, reason, by)
}

// RevokeAccessTx is the transactional form of RevokeAccess. The `reason`
// must be non-empty (after trimming) — empty reasons return
// model.ErrRevokeReasonRequired. A missing user returns
// model.ErrUserNotFound.
//
// This is the only code path permitted to set has_access from true to false.
// Migration 007 dropped the database trigger that previously enforced
// one-way semantics; the invariant now lives at the application layer.
//
//goland:noinspection ALL
func (r *UserRepository) RevokeAccessTx(ctx context.Context, tx model.DBTX, id, reason, by string) error {
	if strings.TrimSpace(reason) == "" {
		return model.ErrRevokeReasonRequired
	}

	query := `UPDATE user_entity
		SET    has_access           = FALSE,
		       access_revoked_at    = NOW(),
		       access_revoked_by    = $1,
		       access_revoke_reason = $2
		WHERE  id = $3`

	result, err := tx.ExecContext(ctx, query, by, reason, id)
	if err != nil {
		return fmt.Errorf("revoking access: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return model.ErrUserNotFound
	}

	return nil
}
