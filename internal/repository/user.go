package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/tipok/waitinglist/internal/model"
)

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
	query := `INSERT INTO user_entity (firstname, lastname, email)
		VALUES ($1, $2, $3)
		RETURNING id, has_access, created_at`

	err := tx.QueryRowContext(ctx, query, user.Firstname, user.Lastname, user.Email).
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
	query := `SELECT id, firstname, lastname, email, has_access, created_at
		FROM user_entity
		WHERE email = $1`

	user := &model.UserEntity{}
	err := tx.QueryRowContext(ctx, query, email).
		Scan(&user.ID, &user.Firstname, &user.Lastname, &user.Email, &user.HasAccess, &user.CreatedAt)
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

	query := `SELECT firstname, lastname, email, has_access, created_at
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
		if err := rows.Scan(&u.Firstname, &u.Lastname, &u.Email, &u.HasAccess, &u.CreatedAt); err != nil {
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
//goland:noinspection ALL
func (r *UserRepository) SetHasAccess(ctx context.Context, ids []string) error {
	return r.SetHasAccessTx(ctx, r.db, ids)
}

// SetHasAccessTx sets has_access to true using the given DBTX (transaction or DB).
// Returns model.ErrUserNotFound if none of the given IDs match any rows.
//
//goland:noinspection ALL
func (r *UserRepository) SetHasAccessTx(ctx context.Context, tx model.DBTX, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	query := `UPDATE user_entity SET has_access = true WHERE id = ANY($1)`

	result, err := tx.ExecContext(ctx, query, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("updating has_access: %w", err)
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
