package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"auto-work/internal/domain"
)

var (
	ErrProjectNotFound = errors.New("project not found")
)

type ProjectRepository struct {
	db *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Create(ctx context.Context, p *domain.Project) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.DefaultProvider == "" {
		p.DefaultProvider = "claude"
	}
	if p.FailurePolicy == "" {
		p.FailurePolicy = domain.ProjectFailurePolicyBlock
	}
	p.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, `
INSERT INTO projects (id, name, path, default_provider, model, system_prompt, failure_policy, auto_dispatch_enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Path, p.DefaultProvider, p.Model, p.SystemPrompt, p.FailurePolicy, boolToInt(p.AutoDispatchEnabled), p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *ProjectRepository) List(ctx context.Context, limit int) ([]domain.Project, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, path, default_provider, model, system_prompt, failure_policy, auto_dispatch_enabled, created_at, updated_at
FROM projects
ORDER BY created_at DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Project, 0)
	for rows.Next() {
		var p domain.Project
		var autoDispatch int
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.DefaultProvider, &p.Model, &p.SystemPrompt, &p.FailurePolicy, &autoDispatch, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if p.DefaultProvider == "" {
			p.DefaultProvider = "claude"
		}
		if p.FailurePolicy == "" {
			p.FailurePolicy = domain.ProjectFailurePolicyBlock
		}
		p.AutoDispatchEnabled = autoDispatch == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, name, path, default_provider, model, system_prompt, failure_policy, auto_dispatch_enabled, created_at, updated_at
FROM projects
WHERE id = ?`, id)
	var p domain.Project
	var autoDispatch int
	if err := row.Scan(&p.ID, &p.Name, &p.Path, &p.DefaultProvider, &p.Model, &p.SystemPrompt, &p.FailurePolicy, &autoDispatch, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	if p.DefaultProvider == "" {
		p.DefaultProvider = "claude"
	}
	if p.FailurePolicy == "" {
		p.FailurePolicy = domain.ProjectFailurePolicyBlock
	}
	p.AutoDispatchEnabled = autoDispatch == 1
	return &p, nil
}

func (r *ProjectRepository) Exists(ctx context.Context, id string) (bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE id = ?`, id)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ProjectRepository) SetAutoDispatchEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE projects
SET auto_dispatch_enabled = ?, updated_at = ?
WHERE id = ?`, boolToInt(enabled), time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *ProjectRepository) AutoDispatchEnabled(ctx context.Context, id string) (bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT auto_dispatch_enabled FROM projects WHERE id = ?`, id)
	var enabled int
	if err := row.Scan(&enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrProjectNotFound
		}
		return false, err
	}
	return enabled == 1, nil
}

func (r *ProjectRepository) ListAutoDispatchEnabledProjectIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM projects
WHERE auto_dispatch_enabled = 1
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *ProjectRepository) UpdateAIConfig(ctx context.Context, id, defaultProvider, model, systemPrompt string, failurePolicy domain.ProjectFailurePolicy) error {
	defaultProvider = strings.TrimSpace(defaultProvider)
	model = strings.TrimSpace(model)
	systemPrompt = strings.TrimSpace(systemPrompt)
	if defaultProvider == "" {
		defaultProvider = "claude"
	}
	if failurePolicy == "" {
		failurePolicy = domain.ProjectFailurePolicyBlock
	}
	res, err := r.db.ExecContext(ctx, `
UPDATE projects
SET default_provider = ?, model = ?, system_prompt = ?, failure_policy = ?, updated_at = ?
WHERE id = ?`, defaultProvider, model, systemPrompt, failurePolicy, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *ProjectRepository) Update(ctx context.Context, id, name, defaultProvider, model, systemPrompt string, failurePolicy domain.ProjectFailurePolicy) error {
	defaultProvider = strings.TrimSpace(defaultProvider)
	model = strings.TrimSpace(model)
	systemPrompt = strings.TrimSpace(systemPrompt)
	if defaultProvider == "" {
		defaultProvider = "claude"
	}
	if failurePolicy == "" {
		failurePolicy = domain.ProjectFailurePolicyBlock
	}
	res, err := r.db.ExecContext(ctx, `
UPDATE projects
SET name = ?, default_provider = ?, model = ?, system_prompt = ?, failure_policy = ?, updated_at = ?
WHERE id = ?`, strings.TrimSpace(name), defaultProvider, model, systemPrompt, failurePolicy, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *ProjectRepository) DeleteWithRelatedData(ctx context.Context, id string) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
DELETE FROM artifacts
WHERE run_id IN (
  SELECT r.id
  FROM runs r
  JOIN tasks t ON t.id = r.task_id
  WHERE t.project_id = ?
)`, id); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `
DELETE FROM run_events
WHERE run_id IN (
  SELECT r.id
  FROM runs r
  JOIN tasks t ON t.id = r.task_id
  WHERE t.project_id = ?
)`, id); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `
DELETE FROM runs
WHERE task_id IN (
  SELECT id FROM tasks WHERE project_id = ?
)`, id); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM tasks WHERE project_id = ?`, id); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrProjectNotFound
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
