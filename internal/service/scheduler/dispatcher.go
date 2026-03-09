package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/domain"
)

var ErrNoRunnableTask = errors.New("no runnable task")

const (
	retryBaseDelay = 30 * time.Second
	retryMaxDelay  = 5 * time.Minute
)

type Dispatcher struct {
	db *sql.DB

	mu              sync.RWMutex
	runFinishedHook RunFinishedHook
}

func NewDispatcher(db *sql.DB) *Dispatcher {
	return &Dispatcher{db: db}
}

type RunFinishedEvent struct {
	RunID      string
	TaskID     string
	RunStatus  domain.RunStatus
	TaskStatus domain.TaskStatus
	Summary    string
	Details    string
	ExitCode   *int
}

type RunFinishedHook func(ctx context.Context, event RunFinishedEvent)

func (d *Dispatcher) SetRunFinishedHook(hook RunFinishedHook) {
	d.mu.Lock()
	d.runFinishedHook = hook
	d.mu.Unlock()
}

func (d *Dispatcher) ClaimNextTaskForAgent(ctx context.Context, agentID, provider, projectID, promptSnapshot string) (*domain.Task, *domain.Run, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "claude"
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if busy, err := agentHasRunningRun(ctx, tx, agentID); err != nil {
		return nil, nil, err
	} else if busy {
		return nil, nil, ErrNoRunnableTask
	}

	candidates, err := listPendingTasks(ctx, tx, provider, projectID, 100)
	if err != nil {
		return nil, nil, err
	}

	for _, task := range candidates {
		allowed, err := projectAllowsTaskDispatch(ctx, tx, task)
		if err != nil {
			return nil, nil, err
		}
		if !allowed {
			continue
		}

		run, err := claimPendingTask(ctx, tx, agentID, provider, task, promptSnapshot)
		if errors.Is(err, ErrNoRunnableTask) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return &task, run, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return nil, nil, ErrNoRunnableTask
}

func (d *Dispatcher) ClaimTaskForAgent(ctx context.Context, agentID, provider, taskID, promptSnapshot string) (*domain.Task, *domain.Run, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "claude"
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil, ErrNoRunnableTask
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if busy, err := agentHasRunningRun(ctx, tx, agentID); err != nil {
		return nil, nil, err
	} else if busy {
		return nil, nil, ErrNoRunnableTask
	}

	task, err := getTaskByIDForDispatch(ctx, tx, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNoRunnableTask
		}
		return nil, nil, err
	}
	if task.Status != domain.TaskPending {
		return nil, nil, ErrNoRunnableTask
	}
	allowed, err := projectAllowsTaskDispatch(ctx, tx, *task)
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return nil, nil, ErrNoRunnableTask
	}

	run, err := claimPendingTask(ctx, tx, agentID, provider, *task, promptSnapshot)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return task, run, nil
}

func (d *Dispatcher) MarkRunFinished(ctx context.Context, runID string, runStatus domain.RunStatus, taskStatus domain.TaskStatus, summary, details string, exitCode *int) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var taskID string
	var currentRunStatus domain.RunStatus
	row := tx.QueryRowContext(ctx, `SELECT task_id, status FROM runs WHERE id = ?`, runID)
	if err = row.Scan(&taskID, &currentRunStatus); err != nil {
		return err
	}
	if currentRunStatus != domain.RunRunning {
		return tx.Commit()
	}

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
UPDATE runs
SET status = ?, result_summary = ?, result_details = ?, exit_code = ?, finished_at = ?, updated_at = ?
WHERE id = ? AND status = 'running'`,
		runStatus, summary, details, exitCode, now, now, runID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return tx.Commit()
	}

	if taskStatus == domain.TaskFailed {
		var (
			retryCount int
		)
		taskRow := tx.QueryRowContext(ctx, `SELECT retry_count FROM tasks WHERE id = ?`, taskID)
		if err = taskRow.Scan(&retryCount); err != nil {
			return err
		}
		nextRetryCount := retryCount + 1
		nextRetryAt := now.Add(retryBackoff(nextRetryCount))

		if _, err = tx.ExecContext(ctx, `
UPDATE tasks
SET status = ?,
    retry_count = ?,
    max_retries = ?,
    next_retry_at = ?,
    updated_at = ?
WHERE id = ?`, taskStatus, nextRetryCount, 0, nextRetryAt, now, taskID); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
UPDATE tasks
SET status = ?, next_retry_at = NULL, updated_at = ?
WHERE id = ?`, taskStatus, now, taskID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	d.mu.RLock()
	hook := d.runFinishedHook
	d.mu.RUnlock()
	if hook != nil {
		event := RunFinishedEvent{
			RunID:      runID,
			TaskID:     taskID,
			RunStatus:  runStatus,
			TaskStatus: taskStatus,
			Summary:    summary,
			Details:    details,
			ExitCode:   exitCode,
		}
		go hook(context.Background(), event)
	}
	return nil
}

func nextRunAttempt(ctx context.Context, tx *sql.Tx, taskID string) (int, error) {
	row := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt), 0) + 1 FROM runs WHERE task_id = ?`, taskID)
	var attempt int
	if err := row.Scan(&attempt); err != nil {
		return 0, err
	}
	if attempt <= 0 {
		return 1, nil
	}
	return attempt, nil
}

func claimPendingTask(ctx context.Context, tx *sql.Tx, agentID, provider string, task domain.Task, promptSnapshot string) (*domain.Run, error) {
	res, err := tx.ExecContext(ctx, `
UPDATE tasks
SET status = 'running', provider = ?, updated_at = ?
WHERE id = ? AND status = 'pending'`, strings.ToLower(strings.TrimSpace(provider)), time.Now().UTC(), task.ID)
	if err != nil {
		return nil, err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if aff == 0 {
		return nil, ErrNoRunnableTask
	}

	now := time.Now().UTC()
	run := &domain.Run{
		ID:             uuid.NewString(),
		TaskID:         task.ID,
		AgentID:        agentID,
		Attempt:        1,
		Status:         domain.RunRunning,
		StartedAt:      now,
		PromptSnapshot: promptSnapshot,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if nextAttempt, nextErr := nextRunAttempt(ctx, tx, task.ID); nextErr == nil && nextAttempt > 0 {
		run.Attempt = nextAttempt
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO runs (
  id, task_id, agent_id, attempt, status, started_at, prompt_snapshot, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TaskID, run.AgentID, run.Attempt, run.Status, run.StartedAt, run.PromptSnapshot, run.CreatedAt, run.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func getTaskByIDForDispatch(ctx context.Context, tx *sql.Tx, taskID string) (*domain.Task, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, created_at, updated_at
FROM tasks
WHERE id = ?`, taskID)

	var (
		task      domain.Task
		projectID sql.NullString
	)
	if err := row.Scan(
		&task.ID, &projectID, &task.Title, &task.Description, &task.Priority, &task.Status, &task.Provider, &task.RetryCount, &task.CreatedAt, &task.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if projectID.Valid {
		task.ProjectID = projectID.String
	}
	return &task, nil
}

func retryBackoff(retryCount int) time.Duration {
	if retryCount <= 1 {
		return retryBaseDelay
	}
	delay := retryBaseDelay
	for i := 1; i < retryCount; i++ {
		if delay >= retryMaxDelay/2 {
			return retryMaxDelay
		}
		delay *= 2
	}
	if delay > retryMaxDelay {
		return retryMaxDelay
	}
	return delay
}

func agentHasRunningRun(ctx context.Context, tx *sql.Tx, agentID string) (bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM runs WHERE agent_id = ? AND status = 'running'`, agentID)
	var c int
	if err := row.Scan(&c); err != nil {
		return false, err
	}
	return c > 0, nil
}

func listPendingTasks(ctx context.Context, tx *sql.Tx, provider, projectID string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 100
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "claude"
	}
	query := `
SELECT t.id, t.project_id, t.title, t.description, t.priority, t.status, t.provider, t.retry_count, t.created_at, t.updated_at
FROM tasks t
LEFT JOIN projects p ON p.id = t.project_id
WHERE t.status = 'pending'
  AND COALESCE(NULLIF(LOWER(TRIM(p.default_provider)), ''), 'claude') = ?`
	args := []any{provider}
	if projectID != "" {
		query += ` AND t.project_id = ?`
		args = append(args, projectID)
	}
	query += `
ORDER BY t.priority ASC, t.created_at ASC
LIMIT ?`
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		var (
			t         domain.Task
			projectID sql.NullString
		)
		if err := rows.Scan(
			&t.ID, &projectID, &t.Title, &t.Description, &t.Priority, &t.Status, &t.Provider, &t.RetryCount, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if projectID.Valid {
			t.ProjectID = projectID.String
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func projectAllowsTaskDispatch(ctx context.Context, tx *sql.Tx, task domain.Task) (bool, error) {
	policy, err := projectFailurePolicy(ctx, tx, task.ProjectID)
	if err != nil {
		return false, err
	}
	if policy == domain.ProjectFailurePolicyContinue {
		return true, nil
	}

	hasFailed, err := projectHasFailedTasks(ctx, tx, task.ProjectID)
	if err != nil {
		return false, err
	}
	if hasFailed {
		return false, nil
	}

	hasPendingRetry, err := projectHasPendingRetryTasks(ctx, tx, task.ProjectID)
	if err != nil {
		return false, err
	}
	if hasPendingRetry && !isPendingRetryTask(task) {
		return false, nil
	}
	return true, nil
}

func isPendingRetryTask(task domain.Task) bool {
	return task.RetryCount > 0 || strings.TrimSpace(task.Provider) != ""
}

func projectFailurePolicy(ctx context.Context, tx *sql.Tx, projectID string) (domain.ProjectFailurePolicy, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.ProjectFailurePolicyBlock, nil
	}
	row := tx.QueryRowContext(ctx, `SELECT failure_policy FROM projects WHERE id = ?`, projectID)
	var policy string
	if err := row.Scan(&policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ProjectFailurePolicyBlock, nil
		}
		return "", err
	}
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		return domain.ProjectFailurePolicyBlock, nil
	}
	return domain.ProjectFailurePolicy(policy), nil
}

func projectHasFailedTasks(ctx context.Context, tx *sql.Tx, projectID string) (bool, error) {
	return projectHasTasksWithStatus(ctx, tx, projectID, domain.TaskFailed)
}

func projectHasTasksWithStatus(ctx context.Context, tx *sql.Tx, projectID string, status domain.TaskStatus) (bool, error) {
	projectID = strings.TrimSpace(projectID)
	var (
		row *sql.Row
		cnt int
	)
	if projectID == "" {
		row = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE project_id IS NULL AND status = ?`, status)
	} else {
		row = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE project_id = ? AND status = ?`, projectID, status)
	}
	if err := row.Scan(&cnt); err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func projectHasPendingRetryTasks(ctx context.Context, tx *sql.Tx, projectID string) (bool, error) {
	projectID = strings.TrimSpace(projectID)
	var (
		row *sql.Row
		cnt int
	)
	if projectID == "" {
		row = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE project_id IS NULL AND status = 'pending' AND (retry_count > 0 OR TRIM(COALESCE(provider, '')) != '')`)
	} else {
		row = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE project_id = ? AND status = 'pending' AND (retry_count > 0 OR TRIM(COALESCE(provider, '')) != '')`, projectID)
	}
	if err := row.Scan(&cnt); err != nil {
		return false, err
	}
	return cnt > 0, nil
}
