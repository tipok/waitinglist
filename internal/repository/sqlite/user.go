package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tipok/waitinglist/internal/model"
)

// validGrantSources mirrors the CHECK constraint in the SQLite migration.
var validGrantSources = map[string]struct{}{
	"scheduler": {},
	"admin":     {},
}

// userSelectColumns is the canonical column list for SELECTs that hydrate a
// full *model.UserEntity.
const userSelectColumns = `id, project_slug, firstname, lastname, email, has_access, created_at, ip_address,
	access_granted_at, access_granted_by, access_revoked_at, access_revoked_by, access_revoke_reason`

// buildInPlaceholders builds a string of "?,?,?" for n arguments and a slice of
// any-typed args from the given string slice.
func buildInPlaceholders(ids []string) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return strings.Join(placeholders, ","), args
}

// scanUser scans a full user_entity row into a UserEntity pointer.
// SQLite stores timestamps as TEXT, so we use custom time scanners.
func scanUser(scanner interface {
	Scan(dest ...any) error
}, u *model.UserEntity) error {
	return scanner.Scan(
		&u.ID, &u.ProjectSlug, &u.Firstname, &u.Lastname, &u.Email,
		&u.HasAccess, &timeScanner{&u.CreatedAt}, &u.IPAddress,
		&nullTimeScanner{&u.AccessGrantedAt}, &u.AccessGrantedBy,
		&nullTimeScanner{&u.AccessRevokedAt}, &u.AccessRevokedBy, &u.AccessRevokeReason,
	)
}

// UserRepository provides SQLite database operations for the user_entity table.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new SQLite UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user into the user_entity table. Returns
// model.ErrDuplicateEmail if the email already exists for the project.
//
//goland:noinspection ALL
func (r *UserRepository) Create(ctx context.Context, user *model.UserEntity) error {
	return r.CreateTx(ctx, r.db, user)
}

// CreateTx inserts a new user using the given DBTX (transaction or DB).
//
//goland:noinspection ALL
func (r *UserRepository) CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error {
	id := uuid.New().String()

	query := `INSERT INTO user_entity (id, project_slug, firstname, lastname, email, ip_address)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING has_access, created_at`

	err := tx.QueryRowContext(ctx, query,
		id, user.ProjectSlug, user.Firstname, user.Lastname, user.Email, user.IPAddress,
	).Scan(&user.HasAccess, &timeScanner{&user.CreatedAt})
	if err != nil {
		if isSQLiteUniqueViolation(err) {
			return model.ErrDuplicateEmail
		}
		return fmt.Errorf("inserting user: %w", err)
	}

	user.ID = id
	return nil
}

// GetByEmail retrieves a user by email address within a project. Returns
// model.ErrUserNotFound if no user with the given email exists.
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmail(ctx context.Context, projectSlug, email string) (*model.UserEntity, error) {
	return r.GetByEmailTx(ctx, r.db, projectSlug, email)
}

// GetByEmailTx retrieves a user by email scoped to a project using the given DBTX.
//
//goland:noinspection ALL
func (r *UserRepository) GetByEmailTx(ctx context.Context, tx model.DBTX, projectSlug, email string) (*model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE project_slug = ? AND email = ?`

	user := &model.UserEntity{}
	err := scanUser(tx.QueryRowContext(ctx, query, projectSlug, email), user)
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
func (r *UserRepository) GetUserInfoByEmails(ctx context.Context, projectSlug string, emails []string) ([]model.UserInfo, error) {
	if len(emails) == 0 {
		return []model.UserInfo{}, nil
	}

	placeholders, emailArgs := buildInPlaceholders(emails)
	args := append([]any{projectSlug}, emailArgs...)

	//goland:noinspection ALL
	query := `SELECT project_slug, firstname, lastname, email, has_access, created_at,
			access_granted_at, access_granted_by, access_revoked_at, access_revoke_reason
		FROM user_entity
		WHERE project_slug = ? AND email IN (` + placeholders + `)`

	rows, err := r.db.QueryContext(ctx, query, args...)
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
			&u.ProjectSlug, &u.Firstname, &u.Lastname, &u.Email, &u.HasAccess, &timeScanner{&u.CreatedAt},
			&nullTimeScanner{&u.AccessGrantedAt}, &u.AccessGrantedBy, &nullTimeScanner{&u.AccessRevokedAt}, &u.AccessRevokeReason,
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

// GrantAccess flips has_access to true for the given user IDs.
func (r *UserRepository) GrantAccess(ctx context.Context, ids []string, source string) error {
	return r.GrantAccessTx(ctx, r.db, ids, source)
}

// GrantAccessTx flips has_access to 1 for the given user IDs, recording the
// grant timestamp and source. Any prior revocation columns are cleared.
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

	placeholders, idArgs := buildInPlaceholders(ids)
	args := append([]any{source}, idArgs...)

	query := `UPDATE user_entity
		SET    has_access           = 1,
		       access_granted_at    = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
		       access_granted_by    = ?,
		       access_revoked_at    = NULL,
		       access_revoked_by    = NULL,
		       access_revoke_reason = NULL
		WHERE  id IN (` + placeholders + `)`

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("granting access: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
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
		WHERE id = ?`

	user := &model.UserEntity{}
	err := scanUser(r.db.QueryRowContext(ctx, query, id), user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return user, nil
}

// GetByIDs returns users matching the given IDs.
//
//goland:noinspection ALL
func (r *UserRepository) GetByIDs(ctx context.Context, ids []string) ([]model.UserEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders, args := buildInPlaceholders(ids)

	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE id IN (` + placeholders + `)`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying users by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []model.UserEntity
	for rows.Next() {
		var u model.UserEntity
		if err := scanUser(rows, &u); err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// CountByAccess returns (waitListCount, withAccessCount). When projectSlug is
// non-empty, counts are scoped to that project.
//
//goland:noinspection ALL
func (r *UserRepository) CountByAccess(ctx context.Context, projectSlug string) (int, int, error) {
	var waitListCount, withAccessCount int

	if projectSlug == "" {
		const query = `
			SELECT
				(SELECT COUNT(*) FROM waiting_list)                        AS waitlist_count,
				(SELECT COUNT(*) FROM user_entity WHERE has_access = 1)    AS access_count`
		if err := r.db.QueryRowContext(ctx, query).Scan(&waitListCount, &withAccessCount); err != nil {
			return 0, 0, fmt.Errorf("counting users by access: %w", err)
		}
	} else {
		const query = `
			SELECT
				(SELECT COUNT(*) FROM waiting_list WHERE project_slug = ?)                      AS waitlist_count,
				(SELECT COUNT(*) FROM user_entity WHERE has_access = 1 AND project_slug = ?)   AS access_count`
		if err := r.db.QueryRowContext(ctx, query, projectSlug, projectSlug).Scan(&waitListCount, &withAccessCount); err != nil {
			return 0, 0, fmt.Errorf("counting users by access: %w", err)
		}
	}
	return waitListCount, withAccessCount, nil
}

// EnlistmentsByDay returns one DayCount per UTC day in the last `days` days,
// ascending by day. Days with no signups are zero-filled. `days` is clamped
// to [1, 365]. When projectSlug is non-empty, results are scoped to that project.
//
//goland:noinspection ALL
func (r *UserRepository) EnlistmentsByDay(ctx context.Context, projectSlug string, days int) ([]model.DayCount, error) {
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}

	var sqlRows *sql.Rows
	var err error

	if projectSlug == "" {
		//goland:noinspection ALL
		const query = `
			SELECT strftime('%Y-%m-%d', created_at) AS day,
			       COUNT(*) AS count
			FROM   user_entity
			WHERE  created_at >= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-' || ? || ' days')
			GROUP  BY 1
			ORDER  BY 1`
		sqlRows, err = r.db.QueryContext(ctx, query, days)
	} else {
		//goland:noinspection ALL
		const query = `
			SELECT strftime('%Y-%m-%d', created_at) AS day,
			       COUNT(*) AS count
			FROM   user_entity
			WHERE  project_slug = ? AND created_at >= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-' || ? || ' days')
			GROUP  BY 1
			ORDER  BY 1`
		sqlRows, err = r.db.QueryContext(ctx, query, projectSlug, days)
	}
	if err != nil {
		return nil, fmt.Errorf("querying enlistments by day: %w", err)
	}
	defer func() {
		_ = sqlRows.Close()
	}()

	got := make(map[string]int, days)
	for sqlRows.Next() {
		var d model.DayCount
		if err := sqlRows.Scan(&d.Day, &d.Count); err != nil {
			return nil, fmt.Errorf("scanning day count: %w", err)
		}
		got[d.Day] = d.Count
	}
	if err := sqlRows.Err(); err != nil {
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

// ListWithAccess returns users where has_access = 1, optionally filtered by
// a case-insensitive email substring and/or project. Pagination via limit/offset.
// When projectSlug is empty, users from all projects are returned.
//
//goland:noinspection ALL
func (r *UserRepository) ListWithAccess(ctx context.Context, projectSlug, emailLike string, limit, offset int) ([]model.UserEntity, error) {
	var sqlRows *sql.Rows
	var err error

	if projectSlug == "" {
		//goland:noinspection ALL
		query := `SELECT ` + userSelectColumns + `
			FROM   user_entity
			WHERE  has_access = 1
			  AND  (? = '' OR email LIKE '%' || ? || '%')
			ORDER  BY CASE WHEN access_granted_at IS NULL THEN 1 ELSE 0 END, access_granted_at DESC, email ASC
			LIMIT  ? OFFSET ?`
		sqlRows, err = r.db.QueryContext(ctx, query, emailLike, emailLike, limit, offset)
	} else {
		//goland:noinspection ALL
		query := `SELECT ` + userSelectColumns + `
			FROM   user_entity
			WHERE  has_access = 1
			  AND  project_slug = ?
			  AND  (? = '' OR email LIKE '%' || ? || '%')
			ORDER  BY CASE WHEN access_granted_at IS NULL THEN 1 ELSE 0 END, access_granted_at DESC, email ASC
			LIMIT  ? OFFSET ?`
		sqlRows, err = r.db.QueryContext(ctx, query, projectSlug, emailLike, emailLike, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("listing users with access: %w", err)
	}
	defer func() {
		_ = sqlRows.Close()
	}()

	users := make([]model.UserEntity, 0, limit)
	for sqlRows.Next() {
		var u model.UserEntity
		if err := scanUser(sqlRows, &u); err != nil {
			return nil, fmt.Errorf("scanning user with access: %w", err)
		}
		users = append(users, u)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users with access: %w", err)
	}
	return users, nil
}

// ListAllWithAccess returns all users with has_access = 1 for the given
// project, with no pagination limit.
//
//goland:noinspection ALL
func (r *UserRepository) ListAllWithAccess(ctx context.Context, projectSlug string) ([]model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM   user_entity
		WHERE  has_access = 1 AND project_slug = ?
		ORDER  BY CASE WHEN access_granted_at IS NULL THEN 1 ELSE 0 END, access_granted_at DESC, email ASC`

	sqlRows, err := r.db.QueryContext(ctx, query, projectSlug)
	if err != nil {
		return nil, fmt.Errorf("listing all users with access: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var users []model.UserEntity
	for sqlRows.Next() {
		var u model.UserEntity
		if err := scanUser(sqlRows, &u); err != nil {
			return nil, fmt.Errorf("scanning user with access: %w", err)
		}
		users = append(users, u)
	}
	return users, sqlRows.Err()
}

// GetGrantedSince returns users whose access was granted after the given
// timestamp, scoped to a project.
//
//goland:noinspection ALL
func (r *UserRepository) GetGrantedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.UserEntity, error) {
	//goland:noinspection ALL
	query := `SELECT ` + userSelectColumns + `
		FROM user_entity
		WHERE project_slug = ? AND access_granted_at > ?
		ORDER BY access_granted_at ASC`

	sqlRows, err := r.db.QueryContext(ctx, query, projectSlug, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("querying users granted since: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var users []model.UserEntity
	for sqlRows.Next() {
		var u model.UserEntity
		if err := scanUser(sqlRows, &u); err != nil {
			return nil, fmt.Errorf("scanning granted user: %w", err)
		}
		users = append(users, u)
	}
	return users, sqlRows.Err()
}

// RevokeAccess flips has_access to false for one user and records the
// revocation timestamp, admin identifier, and reason.
func (r *UserRepository) RevokeAccess(ctx context.Context, id, reason, by string) error {
	return r.RevokeAccessTx(ctx, r.db, id, reason, by)
}

// RevokeAccessTx is the transactional form of RevokeAccess. The `reason`
// must be non-empty (after trimming) — empty reasons return
// model.ErrRevokeReasonRequired. A missing user returns model.ErrUserNotFound.
//
//goland:noinspection ALL
func (r *UserRepository) RevokeAccessTx(ctx context.Context, tx model.DBTX, id, reason, by string) error {
	if strings.TrimSpace(reason) == "" {
		return model.ErrRevokeReasonRequired
	}

	query := `UPDATE user_entity
		SET    has_access           = 0,
		       access_revoked_at    = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
		       access_revoked_by    = ?,
		       access_revoke_reason = ?
		WHERE  id = ?`

	result, err := tx.ExecContext(ctx, query, by, reason, id)
	if err != nil {
		return fmt.Errorf("revoking access: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return model.ErrUserNotFound
	}

	return nil
}
