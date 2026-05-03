package model

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DBTX is an interface satisfied by both *sql.DB and *sql.Tx, allowing
// repository methods to work within or outside a transaction.
type DBTX interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Tx extends DBTX with transaction commit and rollback capabilities.
// *sql.Tx satisfies this interface.
type Tx interface {
	DBTX
	Commit() error
	Rollback() error
}

// Sentinel errors for repository operations.
var (
	ErrDuplicateEmail        = errors.New("email already exists")
	ErrUserNotFound          = errors.New("user not found")
	ErrAlreadyOnWaitingList  = errors.New("user is already on the waiting list")
	ErrWaitingListForeignKey = errors.New("user does not exist")
	ErrAlreadyHasAccess      = errors.New("user already has access")
)

// UserEntity represents a user stored in the user_entity table.
type UserEntity struct {
	ID        string    `json:"id"`
	Firstname string    `json:"firstname"`
	Lastname  string    `json:"lastname"`
	Email     string    `json:"email"`
	HasAccess bool      `json:"has_access"`
	CreatedAt time.Time `json:"created_at"`
	IPAddress *string   `json:"ip_address,omitzero"`
}

// UserInfo represents user information returned by the lookup endpoint.
type UserInfo struct {
	Firstname string    `json:"firstname"`
	Lastname  string    `json:"lastname"`
	Email     string    `json:"email"`
	HasAccess bool      `json:"has_access"`
	CreatedAt time.Time `json:"created_at"`
}

// UserInfoList wraps a slice of UserInfo for JSON serialization.
type UserInfoList struct {
	Users []UserInfo `json:"users"`
}

// WaitingListEntry represents an entry in the waiting_list table.
type WaitingListEntry struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	CreatedAt         time.Time `json:"created_at"`
	WeightedCreatedAt time.Time `json:"weighted_created_at"`
}
