package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"auto-work/internal/domain"
)

var ErrRunNotFound = errors.New("run not found")

type RunRepository struct {
	db *sql.DB
}

type RunningRunRecord struct {
	RunID       string
	TaskID      string
	TaskTitle   string
	ProjectID   string
	AgentID     string
	PID         *int
	Status      domain.RunStatus
	StartedAt   time.Time
	HeartbeatAt *time.Time
}

func NewRunRepository(db *sql.DB) *RunRepository {
	return &RunRepository{db: db}
}

func (r *RunRepository) Create(ctx context.Context, run *domain.Run) error {
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.Status == "" {
		run.Status = domain.RunRunning
	}
	if run.Attempt <= 0 {
		run.Attempt = 1
	}

	_, err := r.db.ExecContext(ctx, `
INSERT INTO runs (
  id, task_id, agent_id, attempt, status, pid, heartbeat_at, started_at, finished_at, exit_code,
  provider_session_id, prompt_snapshot, result_summary, result_details, idempotency_key,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TaskID, run.AgentID, run.Attempt, run.Status, run.PID, run.HeartbeatAt, run.StartedAt,
		run.FinishedAt, run.ExitCode, run.ProviderSessionID, run.PromptSnapshot, run.ResultSummary,
		run.ResultDetails, run.IdempotencyKey, run.CreatedAt, run.UpdatedAt,
	)
	return err
}

func (r *RunRepository) GetRunningByAgent(ctx context.Context, agentID string) (*domain.Run, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, task_id, agent_id, attempt, status, pid, heartbeat_at, started_at, finished_at, exit_code,
       provider_session_id, prompt_snapshot, result_summary, result_details, idempotency_key, created_at, updated_at
FROM runs
WHERE agent_id = ? AND status = 'running'
LIMIT 1`, agentID)
	run, err := scanRun(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return run, nil
}

func (r *RunRepository) GetByID(ctx context.Context, runID string) (*domain.Run, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, task_id, agent_id, attempt, status, pid, heartbeat_at, started_at, finished_at, exit_code,
       provider_session_id, prompt_snapshot, result_summary, result_details, idempotency_key, created_at, updated_at
FROM runs
WHERE id = ?`, runID)
	run, err := scanRun(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return run, nil
}

func (r *RunRepository) ListRunning(ctx context.Context, projectID string, limit int) ([]RunningRunRecord, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
SELECT r.id, r.task_id, t.title, t.project_id, r.agent_id, r.pid, r.status, r.started_at, r.heartbeat_at
FROM runs r
JOIN tasks t ON t.id = r.task_id
WHERE r.status = 'running'`
	args := make([]any, 0, 3)
	if projectID != "" {
		query += ` AND t.project_id = ?`
		args = append(args, projectID)
	}
	query += `
ORDER BY r.started_at DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RunningRunRecord, 0)
	for rows.Next() {
		var (
			v         RunningRunRecord
			pid       sql.NullInt64
			heartbeat sql.NullTime
		)
		if err := rows.Scan(&v.RunID, &v.TaskID, &v.TaskTitle, &v.ProjectID, &v.AgentID, &pid, &v.Status, &v.StartedAt, &heartbeat); err != nil {
			return nil, err
		}
		if pid.Valid {
			p := int(pid.Int64)
			v.PID = &p
		}
		if heartbeat.Valid {
			t := heartbeat.Time
			v.HeartbeatAt = &t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *RunRepository) RecoverOrphanRunningRuns(ctx context.Context, reason string) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `
UPDATE tasks
SET status = 'failed', updated_at = ?
WHERE id IN (
  SELECT task_id FROM runs
  WHERE status = 'running' AND pid IS NULL
)`, now); err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx, `
UPDATE runs
SET status = 'failed',
    result_summary = 'orphaned running run recovered',
    result_details = ?,
    finished_at = ?,
    updated_at = ?
WHERE status = 'running' AND pid IS NULL`, reason, now, now)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func (r *RunRepository) ListByTask(ctx context.Context, taskID string, limit int) ([]domain.Run, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, task_id, agent_id, attempt, status, pid, heartbeat_at, started_at, finished_at, exit_code,
       provider_session_id, prompt_snapshot, result_summary, result_details, idempotency_key, created_at, updated_at
FROM runs
WHERE task_id = ?
ORDER BY started_at DESC
LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (r *RunRepository) UpdateHeartbeat(ctx context.Context, runID string, t time.Time) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE runs
SET heartbeat_at = ?, updated_at = ?
WHERE id = ? AND status = 'running'`, t.UTC(), time.Now().UTC(), runID)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if aff == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (r *RunRepository) AttachProcess(ctx context.Context, runID string, pid int) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
UPDATE runs
SET pid = ?, heartbeat_at = ?, updated_at = ?
WHERE id = ? AND status = 'running'`, pid, now, now, runID)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if aff == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (r *RunRepository) Finish(ctx context.Context, runID string, status domain.RunStatus, exitCode *int, summary, details string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
UPDATE runs
SET status = ?, exit_code = ?, result_summary = ?, result_details = ?, finished_at = ?, updated_at = ?
WHERE id = ?`, status, exitCode, summary, details, now, now, runID)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if aff == 0 {
		return ErrRunNotFound
	}
	return nil
}

func scanRun(scan scannerFn) (*domain.Run, error) {
	var (
		r                 domain.Run
		pid               sql.NullInt64
		heartbeat         sql.NullTime
		finished          sql.NullTime
		exitCode          sql.NullInt64
		providerSessionID sql.NullString
		promptSnapshot    sql.NullString
		resultSummary     sql.NullString
		resultDetails     sql.NullString
		idempotencyKey    sql.NullString
	)

	if err := scan(
		&r.ID, &r.TaskID, &r.AgentID, &r.Attempt, &r.Status, &pid, &heartbeat, &r.StartedAt, &finished, &exitCode,
		&providerSessionID, &promptSnapshot, &resultSummary, &resultDetails, &idempotencyKey, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if providerSessionID.Valid {
		r.ProviderSessionID = providerSessionID.String
	}
	if promptSnapshot.Valid {
		r.PromptSnapshot = promptSnapshot.String
	}
	if resultSummary.Valid {
		r.ResultSummary = resultSummary.String
	}
	if resultDetails.Valid {
		r.ResultDetails = resultDetails.String
	}
	if idempotencyKey.Valid {
		r.IdempotencyKey = idempotencyKey.String
	}
	if pid.Valid {
		v := int(pid.Int64)
		r.PID = &v
	}
	if heartbeat.Valid {
		t := heartbeat.Time
		r.HeartbeatAt = &t
	}
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		r.ExitCode = &v
	}
	return &r, nil
}
