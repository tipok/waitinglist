package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/tipok/waitinglist/internal/model"
)

// ProjectRepository provides database operations for the project table.
type ProjectRepository struct {
	db *sql.DB
}

// NewProjectRepository creates a new ProjectRepository.
func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// GetAll returns all projects ordered by created_at ascending.
//
//goland:noinspection ALL
func (r *ProjectRepository) GetAll(ctx context.Context) ([]model.Project, error) {
	const query = `SELECT id, slug, name, entry_batch_size, entry_window_interval,
		waitlist_check_interval, scheduler_disabled, created_at
		FROM project
		ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying projects: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	projects := make([]model.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating project rows: %w", err)
	}
	return projects, nil
}

// GetBySlug returns a project by its unique slug. Returns
// model.ErrProjectNotFound if no project matches.
//
//goland:noinspection ALL
func (r *ProjectRepository) GetBySlug(ctx context.Context, slug string) (*model.Project, error) {
	const query = `SELECT id, slug, name, entry_batch_size, entry_window_interval,
		waitlist_check_interval, scheduler_disabled, created_at
		FROM project
		WHERE slug = $1`

	var p model.Project
	var batchSize sql.NullInt64
	var windowInterval, checkInterval *string

	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&p.ID, &p.Slug, &p.Name, &batchSize, &windowInterval,
		&checkInterval, &p.SchedulerDisabled, &p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrProjectNotFound
		}
		return nil, fmt.Errorf("querying project by slug: %w", err)
	}

	if err := scanProjectFromRow(&p, batchSize, windowInterval, checkInterval); err != nil {
		return nil, err
	}

	return &p, nil
}

// GetByID returns a project by its primary key. Returns
// model.ErrProjectNotFound if no project matches.
//
//goland:noinspection ALL
func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*model.Project, error) {
	const query = `SELECT id, slug, name, entry_batch_size, entry_window_interval,
		waitlist_check_interval, scheduler_disabled, created_at
		FROM project
		WHERE id = $1`

	var p model.Project
	var batchSize sql.NullInt64
	var windowInterval, checkInterval *string

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID, &p.Slug, &p.Name, &batchSize, &windowInterval,
		&checkInterval, &p.SchedulerDisabled, &p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrProjectNotFound
		}
		return nil, fmt.Errorf("querying project by id: %w", err)
	}

	if err := scanProjectFromRow(&p, batchSize, windowInterval, checkInterval); err != nil {
		return nil, err
	}

	return &p, nil
}

// Create inserts a new project. Returns model.ErrDuplicateProjectSlug if the
// slug is already taken.
//
//goland:noinspection ALL
func (r *ProjectRepository) Create(ctx context.Context, p *model.Project) error {
	const query = `INSERT INTO project (slug, name, entry_batch_size, entry_window_interval,
		waitlist_check_interval, scheduler_disabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`

	var windowInterval, checkInterval *string
	if p.EntryWindowInterval != nil {
		s := durationToInterval(*p.EntryWindowInterval)
		windowInterval = &s
	}
	if p.WaitlistCheckInterval != nil {
		s := durationToInterval(*p.WaitlistCheckInterval)
		checkInterval = &s
	}

	err := r.db.QueryRowContext(ctx, query,
		p.Slug, p.Name, p.EntryBatchSize, windowInterval,
		checkInterval, p.SchedulerDisabled,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return model.ErrDuplicateProjectSlug
		}
		return fmt.Errorf("inserting project: %w", err)
	}
	return nil
}

// Update modifies a project's mutable fields (name, scheduler config).
// Returns model.ErrProjectNotFound if no row matches.
//
//goland:noinspection ALL
func (r *ProjectRepository) Update(ctx context.Context, p *model.Project) error {
	const query = `UPDATE project
		SET name = $1, entry_batch_size = $2, entry_window_interval = $3,
		    waitlist_check_interval = $4, scheduler_disabled = $5
		WHERE id = $6`

	var windowInterval, checkInterval *string
	if p.EntryWindowInterval != nil {
		s := durationToInterval(*p.EntryWindowInterval)
		windowInterval = &s
	}
	if p.WaitlistCheckInterval != nil {
		s := durationToInterval(*p.WaitlistCheckInterval)
		checkInterval = &s
	}

	result, err := r.db.ExecContext(ctx, query,
		p.Name, p.EntryBatchSize, windowInterval,
		checkInterval, p.SchedulerDisabled, p.ID,
	)
	if err != nil {
		return fmt.Errorf("updating project: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return model.ErrProjectNotFound
	}
	return nil
}

// scanProjectFromRow populates a model.Project from pre-scanned nullable
// fields. It converts the raw interval strings to time.Duration values and
// propagates parse errors.
func scanProjectFromRow(p *model.Project, batchSize sql.NullInt64, windowInterval, checkInterval *string) error {
	if batchSize.Valid {
		v := int(batchSize.Int64)
		p.EntryBatchSize = &v
	}
	if windowInterval != nil {
		d, err := parseInterval(*windowInterval)
		if err != nil {
			return fmt.Errorf("parsing entry_window_interval %q: %w", *windowInterval, err)
		}
		p.EntryWindowInterval = &d
	}
	if checkInterval != nil {
		d, err := parseInterval(*checkInterval)
		if err != nil {
			return fmt.Errorf("parsing waitlist_check_interval %q: %w", *checkInterval, err)
		}
		p.WaitlistCheckInterval = &d
	}
	return nil
}

func scanProject(rows *sql.Rows) (model.Project, error) {
	var p model.Project
	var batchSize sql.NullInt64
	var windowInterval, checkInterval *string

	if err := rows.Scan(
		&p.ID, &p.Slug, &p.Name, &batchSize, &windowInterval,
		&checkInterval, &p.SchedulerDisabled, &p.CreatedAt,
	); err != nil {
		return model.Project{}, fmt.Errorf("scanning project: %w", err)
	}

	if err := scanProjectFromRow(&p, batchSize, windowInterval, checkInterval); err != nil {
		return model.Project{}, err
	}

	return p, nil
}

func parseInterval(s string) (model.Duration, error) {
	d, err := time.ParseDuration(s)
	return model.Duration(d), err
}

func durationToInterval(d model.Duration) string {
	return time.Duration(d).String()
}
