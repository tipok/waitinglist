package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Duration wraps time.Duration with JSON string marshaling (e.g. "30h").
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

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
	ErrDuplicateEmail           = errors.New("email already exists")
	ErrUserNotFound             = errors.New("user not found")
	ErrAlreadyOnWaitingList     = errors.New("user is already on the waiting list")
	ErrWaitingListForeignKey    = errors.New("user does not exist")
	ErrAlreadyHasAccess         = errors.New("user already has access")
	ErrRevokeReasonRequired     = errors.New("access revoke reason is required")
	ErrWaitingListEntryNotFound = errors.New("waiting list entry not found")
	ErrProjectNotFound          = errors.New("project not found")
	ErrDuplicateProjectSlug     = errors.New("project slug already exists")
)

// Project represents a tenant project stored in the project table.
type Project struct {
	ID                    string    `json:"id"`
	Slug                  string    `json:"slug"`
	Name                  string    `json:"name"`
	EntryBatchSize        *int      `json:"entry_batch_size,omitempty"`
	EntryWindowInterval   *Duration `json:"entry_window_interval,omitempty"`
	WaitlistCheckInterval *Duration `json:"waitlist_check_interval,omitempty"`
	SchedulerDisabled     bool      `json:"scheduler_disabled"`
	CreatedAt             time.Time `json:"created_at"`
}

// UserEntity represents a user stored in the user_entity table.
type UserEntity struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	Firstname          string     `json:"firstname"`
	Lastname           string     `json:"lastname"`
	Email              string     `json:"email"`
	HasAccess          bool       `json:"has_access"`
	CreatedAt          time.Time  `json:"created_at"`
	IPAddress          *string    `json:"ip_address,omitempty"`
	AccessGrantedAt    *time.Time `json:"access_granted_at,omitempty"`
	AccessGrantedBy    *string    `json:"access_granted_by,omitempty"`
	AccessRevokedAt    *time.Time `json:"access_revoked_at,omitempty"`
	AccessRevokedBy    *string    `json:"access_revoked_by,omitempty"`
	AccessRevokeReason *string    `json:"access_revoke_reason,omitempty"`
}

// UserInfo represents user information returned by the public lookup endpoint
// (GET /waitinglist/users). The admin identifier (access_revoked_by) is
// deliberately omitted so the public endpoint does not leak it.
type UserInfo struct {
	ProjectID          string     `json:"project_id"`
	Firstname          string     `json:"firstname"`
	Lastname           string     `json:"lastname"`
	Email              string     `json:"email"`
	HasAccess          bool       `json:"has_access"`
	CreatedAt          time.Time  `json:"created_at"`
	AccessGrantedAt    *time.Time `json:"access_granted_at,omitempty"`
	AccessGrantedBy    *string    `json:"access_granted_by,omitempty"`
	AccessRevokedAt    *time.Time `json:"access_revoked_at,omitempty"`
	AccessRevokeReason *string    `json:"access_revoke_reason,omitempty"`
}

// UserInfoList wraps a slice of UserInfo for JSON serialization.
type UserInfoList struct {
	Users []UserInfo `json:"users"`
}

// WaitingListEntry represents an entry in the waiting_list table.
type WaitingListEntry struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	UserID            string    `json:"user_id"`
	CreatedAt         time.Time `json:"created_at"`
	WeightedCreatedAt time.Time `json:"weighted_created_at"`
}

// DayCount is one bucket of the dashboard "enlistments per day" series.
type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// WaitingListAdminRow is a denormalized waiting-list view used by the admin
// list endpoint. It joins user_entity onto waiting_list so the UI can
// render rows in a single round trip.
type WaitingListAdminRow struct {
	EntryID           string    `json:"entry_id"`
	ProjectID         string    `json:"project_id"`
	UserID            string    `json:"user_id"`
	Email             string    `json:"email"`
	Firstname         string    `json:"firstname"`
	Lastname          string    `json:"lastname"`
	Weight            int       `json:"weight"`
	CreatedAt         time.Time `json:"created_at"`
	WeightedCreatedAt time.Time `json:"weighted_created_at"`
}
