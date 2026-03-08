package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"auto-work/internal/domain"
)

var ErrAgentNotFound = errors.New("agent not found")

type AgentRepository struct {
	db *sql.DB
}

func NewAgentRepository(db *sql.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

func (r *AgentRepository) Upsert(ctx context.Context, a *domain.Agent) error {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Concurrency <= 0 {
		a.Concurrency = 1
	}

	_, err := r.db.ExecContext(ctx, `
INSERT INTO agents (id, name, provider, enabled, concurrency, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  provider = excluded.provider,
  enabled = excluded.enabled,
  concurrency = excluded.concurrency,
  last_seen_at = excluded.last_seen_at,
  updated_at = excluded.updated_at`,
		a.ID, a.Name, a.Provider, boolToInt(a.Enabled), a.Concurrency, a.LastSeenAt, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func (r *AgentRepository) GetByID(ctx context.Context, id string) (*domain.Agent, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, name, provider, enabled, concurrency, last_seen_at, created_at, updated_at
FROM agents WHERE id = ?`, id)
	var (
		a       domain.Agent
		enabled int
		last    sql.NullTime
	)
	if err := row.Scan(
		&a.ID, &a.Name, &a.Provider, &enabled, &a.Concurrency, &last, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	a.Enabled = enabled == 1
	if last.Valid {
		t := last.Time
		a.LastSeenAt = &t
	}
	return &a, nil
}

func (r *AgentRepository) ListEnabled(ctx context.Context) ([]domain.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, name, provider, enabled, concurrency, last_seen_at, created_at, updated_at
FROM agents
WHERE enabled = 1
ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Agent, 0)
	for rows.Next() {
		var (
			a       domain.Agent
			enabled int
			last    sql.NullTime
		)
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Provider, &enabled, &a.Concurrency, &last, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.Enabled = enabled == 1
		if last.Valid {
			t := last.Time
			a.LastSeenAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
