// Package repotest provides shared test helpers and behavioral test suites
// that validate identical behavior across PostgreSQL and SQLite backends.
package repotest

import (
	"context"
	"database/sql"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// UserRepository is the interface that both postgres.UserRepository and
// sqlite.UserRepository must satisfy for shared parity tests.
type UserRepository interface {
	Create(ctx context.Context, user *model.UserEntity) error
	CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error
	GetByEmail(ctx context.Context, projectSlug, email string) (*model.UserEntity, error)
	GetByEmailTx(ctx context.Context, tx model.DBTX, projectSlug, email string) (*model.UserEntity, error)
	GetUserInfoByEmails(ctx context.Context, projectSlug string, emails []string) ([]model.UserInfo, error)
	GrantAccess(ctx context.Context, ids []string, source string) error
	GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error
	GetByID(ctx context.Context, id string) (*model.UserEntity, error)
	GetByIDs(ctx context.Context, ids []string) ([]model.UserEntity, error)
	CountByAccess(ctx context.Context, projectSlug string) (int, int, error)
	EnlistmentsByDay(ctx context.Context, projectSlug string, days int) ([]model.DayCount, error)
	ListWithAccess(ctx context.Context, projectSlug, emailLike string, limit, offset int) ([]model.UserEntity, error)
	ListAllWithAccess(ctx context.Context, projectSlug string) ([]model.UserEntity, error)
	GetGrantedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.UserEntity, error)
	RevokeAccess(ctx context.Context, id, reason, by string) error
	RevokeAccessTx(ctx context.Context, tx model.DBTX, id, reason, by string) error
}

// WaitingListRepository is the interface that both backends must satisfy.
type WaitingListRepository interface {
	Add(ctx context.Context, tx model.DBTX, projectSlug, userID string) (*model.WaitingListEntry, error)
	GetAll(ctx context.Context, projectSlug string) ([]model.WaitingListEntry, error)
	GetWithOffsetLimit(ctx context.Context, projectSlug string, offset, limit *int) ([]model.WaitingListEntry, error)
	GetEnlistedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.WaitingListAdminRow, error)
	ListAllJoined(ctx context.Context, projectSlug string) ([]model.WaitingListAdminRow, error)
	ListJoined(ctx context.Context, projectSlug, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error)
	DeleteByIDs(ctx context.Context, ids []string) error
	DeleteByIDsTx(ctx context.Context, tx model.DBTX, ids []string) error
	BeginTx(ctx context.Context) (model.Tx, error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByIDTx(ctx context.Context, tx model.DBTX, id string) error
	DeleteByUserID(ctx context.Context, userID string) error
	DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error
}

// SchedulerRepository is the interface that both backends must satisfy.
type SchedulerRepository interface {
	GetLastSuccess(ctx context.Context, projectSlug, key string) (time.Time, error)
	UpdateLastSuccess(ctx context.Context, tx model.DBTX, projectSlug, key string) error
}

// NewUser creates a test UserEntity with the given project and email.
func NewUser(projectSlug, email string) *model.UserEntity {
	return &model.UserEntity{
		ProjectSlug: projectSlug,
		Firstname:   "First",
		Lastname:    "Last",
		Email:       email,
	}
}

// DB is an abstraction allowing the shared test suite to begin transactions.
type DB interface {
	model.DBTX
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}
