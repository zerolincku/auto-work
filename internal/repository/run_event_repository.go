package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RunEventRepository struct {
	db *sql.DB
}

func NewRunEventRepository(db *sql.DB) *RunEventRepository {
	return &RunEventRepository{db: db}
}

func (r *RunEventRepository) Append(ctx context.Context, runID, kind, payload string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO run_events(id, run_id, ts, kind, payload)
VALUES (?, ?, ?, ?, ?)`,
		uuid.NewString(), runID, time.Now().UTC(), kind, payload,
	)
	return err
}

type RunEventRecord struct {
	ID      string
	RunID   string
	TS      time.Time
	Kind    string
	Payload string
}

type SystemLogRecord struct {
	ID        string
	RunID     string
	TaskID    string
	TaskTitle string
	ProjectID string
	TS        time.Time
	Kind      string
	Payload   string
}

func (r *RunEventRepository) ListByRun(ctx context.Context, runID string, limit int) ([]RunEventRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, run_id, ts, kind, payload
FROM run_events
WHERE run_id = ?
ORDER BY ts DESC
LIMIT ?`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RunEventRecord, 0)
	for rows.Next() {
		var e RunEventRecord
		if err := rows.Scan(&e.ID, &e.RunID, &e.TS, &e.Kind, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].TS.Before(out[j].TS)
	})
	return out, nil
}

func (r *RunEventRepository) ListRecent(ctx context.Context, projectID string, limit int) ([]SystemLogRecord, error) {
	if limit <= 0 {
		limit = 200
	}

	projectID = strings.TrimSpace(projectID)
	query := `
SELECT e.id, e.run_id, r.task_id, t.title, t.project_id, e.ts, e.kind, e.payload
FROM run_events e
JOIN runs r ON r.id = e.run_id
JOIN tasks t ON t.id = r.task_id`
	args := make([]any, 0, 2)
	conditions := []string{"(e.kind LIKE '%.stdout' OR e.kind LIKE '%.stderr')"}
	if projectID != "" {
		conditions = append(conditions, "t.project_id = ?")
		args = append(args, projectID)
	}
	query += `
WHERE ` + strings.Join(conditions, " AND ")
	query += `
ORDER BY e.ts DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SystemLogRecord, 0)
	for rows.Next() {
		var e SystemLogRecord
		if err := rows.Scan(&e.ID, &e.RunID, &e.TaskID, &e.TaskTitle, &e.ProjectID, &e.TS, &e.Kind, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
