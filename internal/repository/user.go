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
	query := `INSERT INTO user_entity (firstname, lastname, email)
		VALUES ($1, $2, $3)
		RETURNING id, has_access`

	err := r.db.QueryRowContext(ctx, query, user.Firstname, user.Lastname, user.Email).
		Scan(&user.ID, &user.HasAccess)
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
	query := `SELECT id, firstname, lastname, email, has_access
		FROM user_entity
		WHERE email = $1`

	user := &model.UserEntity{}
	err := r.db.QueryRowContext(ctx, query, email).
		Scan(&user.ID, &user.Firstname, &user.Lastname, &user.Email, &user.HasAccess)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by email: %w", err)
	}

	return user, nil
}
