package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
const userSelectColumns = `id, firstname, lastname, email, has_access, created_at, ip_address,
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
	query := `INSERT INTO user_entity (firstname, lastname, email, ip_address)
		VALUES ($1, $2, $3, $4)
		RETURNING id, has_access, created_at`

	err := tx.QueryRowContext(ctx, query, user.Firstname, user.Lastname, user.Email, user.IPAddress).
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

// GetByEmail retrieves a user by email address. Returns model.ErrUserNotFound
// if no user with the given email exists.
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.UserEntity, error) {
	return r.GetByEmailTx(ctx, r.db, email)
}

// GetByEmailTx retrieves a user by email using the given DBTX (transaction or DB).
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmailTx(ctx context.Context, tx model.DBTX, email string) (*model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE email = $1`

	user := &model.UserEntity{}
	err := tx.QueryRowContext(ctx, query, email).Scan(
		&user.ID, &user.Firstname, &user.Lastname, &user.Email,
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

// GetUserInfoByEmails retrieves user information for the given email addresses.
// Returns an empty slice when no matching users are found.
//
//goland:noinspection ALL
func (r *UserRepository) GetUserInfoByEmails(ctx context.Context, emails []string) ([]model.UserInfo, error) {
	if len(emails) == 0 {
		return []model.UserInfo{}, nil
	}

	//goland:noinspection ALL
	query := `SELECT firstname, lastname, email, has_access, created_at,
			access_granted_at, access_granted_by, access_revoked_at, access_revoke_reason
		FROM user_entity
		WHERE email = ANY($1)`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(emails))
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
			&u.Firstname, &u.Lastname, &u.Email, &u.HasAccess, &u.CreatedAt,
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
