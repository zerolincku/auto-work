package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-work/internal/domain"
)

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskNotEditable = errors.New("task is not editable while running")
var ErrTaskNotDeletable = errors.New("task is not deletable while running")

const taskPriorityBase = 100

type TaskRepository struct {
	db *sql.DB
}

type TaskListFilter struct {
	Status    *domain.TaskStatus
	Provider  string
	ProjectID string
	Limit     int
}

func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Create(ctx context.Context, task *domain.Task) error {
	if task.CreatedAt.IsZero() {
		now := time.Now().UTC()
		task.CreatedAt = now
		task.UpdatedAt = now
	}
	if task.Status == "" {
		task.Status = domain.TaskPending
	}
	if task.MaxRetries < 0 {
		task.MaxRetries = 0
	}
	var projectID any
	if task.ProjectID != "" {
		projectID = task.ProjectID
	}

	_, err := r.db.ExecContext(ctx, `
INSERT INTO tasks (id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, projectID, task.Title, task.Description, task.Priority, task.Status, task.Provider,
		task.RetryCount, task.MaxRetries, task.NextRetryAt, task.CreatedAt, task.UpdatedAt,
	)
	return err
}

func (r *TaskRepository) GetByID(ctx context.Context, id string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks WHERE id = ?`, id)

	task, err := scanTask(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

func (r *TaskRepository) ListByStatus(ctx context.Context, status domain.TaskStatus, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE status = ?
ORDER BY priority ASC, created_at ASC
LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func (r *TaskRepository) List(ctx context.Context, filter TaskListFilter) ([]domain.Task, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	query := `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE 1=1`
	args := make([]any, 0, 4)

	if filter.Status != nil {
		query += ` AND status = ?`
		args = append(args, *filter.Status)
	}
	if filter.Provider != "" {
		query += ` AND provider = ?`
		args = append(args, filter.Provider)
	}
	if filter.ProjectID != "" {
		query += ` AND project_id = ?`
		args = append(args, filter.ProjectID)
	}

	query += ` ORDER BY priority ASC, created_at ASC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func (r *TaskRepository) ListByProjectAndStatuses(ctx context.Context, projectID string, statuses []domain.TaskStatus, limit int) ([]domain.Task, error) {
	if strings.TrimSpace(projectID) == "" || len(statuses) == 0 {
		return []domain.Task{}, nil
	}
	if limit <= 0 {
		limit = 100
	}

	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses)+2)
	args = append(args, projectID)
	for _, st := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, st)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE project_id = ? AND status IN (%s)
ORDER BY updated_at DESC
LIMIT ?`, strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func (r *TaskRepository) NextAppendPriority(ctx context.Context, projectID string, base int) (int, error) {
	if base <= 0 {
		base = 100
	}
	var (
		maxPriority sql.NullInt64
		row         *sql.Row
	)
	if strings.TrimSpace(projectID) == "" {
		row = r.db.QueryRowContext(ctx, `SELECT MAX(priority) FROM tasks WHERE project_id IS NULL`)
	} else {
		row = r.db.QueryRowContext(ctx, `SELECT MAX(priority) FROM tasks WHERE project_id = ?`, strings.TrimSpace(projectID))
	}
	if err := row.Scan(&maxPriority); err != nil {
		return 0, err
	}
	if !maxPriority.Valid {
		return base, nil
	}
	next := int(maxPriority.Int64) + 1
	if next < base {
		next = base
	}
	return next, nil
}

func (r *TaskRepository) ListRecentByStatuses(ctx context.Context, statuses []domain.TaskStatus, limit int) ([]domain.Task, error) {
	if len(statuses) == 0 {
		return []domain.Task{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses)+1)
	for _, st := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, st)
	}
	args = append(args, limit)

	// Keep running tasks ahead of done/failed/blocked, then newest first.
	query := fmt.Sprintf(`
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE status IN (%s)
ORDER BY CASE status
  WHEN 'running' THEN 0
  WHEN 'pending' THEN 1
  WHEN 'done' THEN 2
  WHEN 'failed' THEN 3
  WHEN 'blocked' THEN 4
  ELSE 9
END ASC, updated_at DESC
LIMIT ?`, strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func (r *TaskRepository) UpdateStatus(ctx context.Context, id string, status domain.TaskStatus) error {
	now := time.Now().UTC()
	var nextRetryAt any
	if status == domain.TaskFailed {
		nextRetryAt = now.Add(30 * time.Second)
	}
	res, err := r.db.ExecContext(ctx, `
UPDATE tasks
SET status = ?,
    updated_at = ?,
    max_retries = CASE WHEN ? = 'failed' THEN 0 ELSE max_retries END,
    next_retry_at = CASE
      WHEN ? = 'failed' THEN COALESCE(next_retry_at, ?)
      ELSE NULL
    END
WHERE id = ?`, status, now, status, status, nextRetryAt, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (r *TaskRepository) UpdateNonRunningTask(ctx context.Context, task *domain.Task) error {
	if task == nil {
		return ErrTaskNotFound
	}
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
UPDATE tasks
SET title = ?,
    description = ?,
    priority = ?,
    updated_at = ?
WHERE id = ?
  AND status != 'running'`,
		task.Title, task.Description, task.Priority, now, task.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		task.UpdatedAt = now
		return nil
	}

	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE id = ?`, task.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrTaskNotFound
	}
	return ErrTaskNotEditable
}

func (r *TaskRepository) CreateBatchWithReorder(ctx context.Context, projectID string, tasks []*domain.Task, insertAfterTaskID string) (err error) {
	projectID = strings.TrimSpace(projectID)
	if len(tasks) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	existing, err := r.listByProjectTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	insertPos, err := findInsertPosition(existing, strings.TrimSpace(insertAfterTaskID))
	if err != nil {
		return err
	}

	orderedIDs := make([]string, 0, len(existing)+len(tasks))
	for _, task := range existing[:insertPos] {
		orderedIDs = append(orderedIDs, task.ID)
	}
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.ID) == "" {
			return ErrTaskNotFound
		}
		orderedIDs = append(orderedIDs, task.ID)
	}
	for _, task := range existing[insertPos:] {
		orderedIDs = append(orderedIDs, task.ID)
	}
	priorityByID := buildPriorityByID(orderedIDs, taskPriorityBase)

	now := time.Now().UTC()
	for _, task := range tasks {
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
		}
		task.ProjectID = projectID
		task.Priority = priorityByID[task.ID]
		if err := insertTaskTx(ctx, tx, task); err != nil {
			return err
		}
	}
	if err := applyPriorityOrderTx(ctx, tx, orderedIDs, priorityByID); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *TaskRepository) UpdateNonRunningTaskWithReorder(ctx context.Context, task *domain.Task, insertAfterTaskID string) (err error) {
	if task == nil {
		return ErrTaskNotFound
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := getTaskByIDTx(ctx, tx, task.ID)
	if err != nil {
		return err
	}
	if current.Status == domain.TaskRunning {
		return ErrTaskNotEditable
	}

	existing, err := r.listByProjectTx(ctx, tx, current.ProjectID)
	if err != nil {
		return err
	}
	orderedIDs, err := reorderTaskIDs(existing, task.ID, strings.TrimSpace(insertAfterTaskID))
	if err != nil {
		return err
	}
	priorityByID := buildPriorityByID(orderedIDs, taskPriorityBase)

	task.ProjectID = current.ProjectID
	task.Priority = priorityByID[task.ID]
	task.Status = current.Status
	task.Provider = current.Provider
	task.RetryCount = current.RetryCount
	task.MaxRetries = current.MaxRetries
	task.NextRetryAt = current.NextRetryAt
	task.CreatedAt = current.CreatedAt

	if err := updateNonRunningTaskTx(ctx, tx, task); err != nil {
		return err
	}
	if err := applyPriorityOrderTx(ctx, tx, orderedIDs, priorityByID); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *TaskRepository) DeleteNonRunningTaskWithReorder(ctx context.Context, id string) (err error) {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskNotFound
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	task, err := getTaskByIDTx(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if task.Status == domain.TaskRunning {
		return ErrTaskNotDeletable
	}

	existing, err := r.listByProjectTx(ctx, tx, task.ProjectID)
	if err != nil {
		return err
	}
	orderedIDs := make([]string, 0, len(existing)-1)
	for _, item := range existing {
		if item.ID == taskID {
			continue
		}
		orderedIDs = append(orderedIDs, item.ID)
	}
	priorityByID := buildPriorityByID(orderedIDs, taskPriorityBase)

	if _, err = tx.ExecContext(ctx, `DELETE FROM artifacts WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM run_events WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM runs WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND status != 'running'`, taskID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotDeletable
	}
	if err := applyPriorityOrderTx(ctx, tx, orderedIDs, priorityByID); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *TaskRepository) DeleteNonRunningTask(ctx context.Context, id string) (err error) {
	taskID := strings.TrimSpace(id)
	if taskID == "" {
		return ErrTaskNotFound
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var status domain.TaskStatus
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id = ?`, taskID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		return err
	}
	if status == domain.TaskRunning {
		return ErrTaskNotDeletable
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM artifacts WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM run_events WHERE run_id IN (SELECT id FROM runs WHERE task_id = ?)`, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM runs WHERE task_id = ?`, taskID); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND status != 'running'`, taskID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE id = ?`, taskID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrTaskNotFound
		}
		return ErrTaskNotDeletable
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *TaskRepository) ResetRetryToPending(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
UPDATE tasks
SET status = 'pending',
    retry_count = 0,
    next_retry_at = NULL,
    updated_at = ?
WHERE id = ?`, now, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (r *TaskRepository) ScheduleRetry(ctx context.Context, id string, nextRetryAt time.Time) (retryCount int, maxRetries int, scheduled bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var (
		status  domain.TaskStatus
		current int
		max     int
	)
	row := tx.QueryRowContext(ctx, `SELECT status, retry_count, max_retries FROM tasks WHERE id = ?`, id)
	if err = row.Scan(&status, &current, &max); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, false, ErrTaskNotFound
		}
		return 0, 0, false, err
	}
	if status != domain.TaskFailed {
		if err = tx.Commit(); err != nil {
			return 0, 0, false, err
		}
		return current, max, false, nil
	}
	// max_retries <= 0 means unlimited retries; enforce unlimited for failed tasks.
	max = 0

	nextCount := current + 1
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
UPDATE tasks
SET retry_count = ?,
    max_retries = ?,
    next_retry_at = ?,
    updated_at = ?
WHERE id = ? AND status = 'failed'`, nextCount, max, nextRetryAt.UTC(), now, id)
	if err != nil {
		return 0, 0, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, false, err
	}
	return nextCount, max, affected > 0, nil
}

func (r *TaskRepository) ListDueRetryTaskIDs(ctx context.Context, now time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM tasks
WHERE status = 'failed'
  AND next_retry_at IS NOT NULL
  AND next_retry_at <= ?
ORDER BY next_retry_at ASC, priority ASC
LIMIT ?`, now.UTC(), limit)
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

func (r *TaskRepository) PromoteFailedRetryToPending(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
UPDATE tasks
SET status = 'pending',
    next_retry_at = NULL,
    updated_at = ?
WHERE id = ?
  AND status = 'failed'
  AND next_retry_at IS NOT NULL
  AND next_retry_at <= ?`, now, id, now)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (r *TaskRepository) NextPendingProvider(ctx context.Context, projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	var row *sql.Row
	if projectID == "" {
		row = r.db.QueryRowContext(ctx, `
SELECT provider
FROM tasks
WHERE status = 'pending'
ORDER BY priority ASC, created_at ASC
LIMIT 1`)
	} else {
		row = r.db.QueryRowContext(ctx, `
SELECT provider
FROM tasks
WHERE status = 'pending' AND project_id = ?
ORDER BY priority ASC, created_at ASC
LIMIT 1`, projectID)
	}
	var provider string
	if err := row.Scan(&provider); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(provider), nil
}

type scannerFn func(dest ...any) error

func scanTask(scan scannerFn) (*domain.Task, error) {
	var (
		t           domain.Task
		projectID   sql.NullString
		nextRetryAt sql.NullTime
	)
	if err := scan(
		&t.ID, &projectID, &t.Title, &t.Description, &t.Priority, &t.Status, &t.Provider, &t.RetryCount, &t.MaxRetries, &nextRetryAt, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if projectID.Valid {
		t.ProjectID = projectID.String
	}
	if nextRetryAt.Valid {
		n := nextRetryAt.Time
		t.NextRetryAt = &n
	}
	return &t, nil
}

func (r *TaskRepository) listByProjectTx(ctx context.Context, tx *sql.Tx, projectID string) ([]domain.Task, error) {
	projectID = strings.TrimSpace(projectID)

	var (
		rows *sql.Rows
		err  error
	)
	if projectID == "" {
		rows, err = tx.QueryContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE project_id IS NULL
ORDER BY priority ASC, created_at ASC, id ASC`)
	} else {
		rows, err = tx.QueryContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks
WHERE project_id = ?
ORDER BY priority ASC, created_at ASC, id ASC`, projectID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func getTaskByIDTx(ctx context.Context, tx *sql.Tx, id string) (*domain.Task, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at
FROM tasks WHERE id = ?`, id)
	task, err := scanTask(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

func insertTaskTx(ctx context.Context, tx *sql.Tx, task *domain.Task) error {
	if task == nil {
		return ErrTaskNotFound
	}
	if task.Status == "" {
		task.Status = domain.TaskPending
	}
	if task.MaxRetries < 0 {
		task.MaxRetries = 0
	}
	var projectID any
	if strings.TrimSpace(task.ProjectID) != "" {
		projectID = strings.TrimSpace(task.ProjectID)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO tasks (id, project_id, title, description, priority, status, provider, retry_count, max_retries, next_retry_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, projectID, task.Title, task.Description, task.Priority, task.Status, task.Provider,
		task.RetryCount, task.MaxRetries, task.NextRetryAt, task.CreatedAt, task.UpdatedAt,
	)
	return err
}

func updateNonRunningTaskTx(ctx context.Context, tx *sql.Tx, task *domain.Task) error {
	if task == nil {
		return ErrTaskNotFound
	}
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
UPDATE tasks
SET title = ?,
    description = ?,
    priority = ?,
    updated_at = ?
WHERE id = ?
  AND status != 'running'`,
		task.Title, task.Description, task.Priority, now, task.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		task.UpdatedAt = now
		return nil
	}

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE id = ?`, task.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrTaskNotFound
	}
	return ErrTaskNotEditable
}

func applyPriorityOrderTx(ctx context.Context, tx *sql.Tx, orderedIDs []string, priorityByID map[string]int) error {
	for _, taskID := range orderedIDs {
		priority, ok := priorityByID[taskID]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET priority = ? WHERE id = ?`, priority, taskID); err != nil {
			return err
		}
	}
	return nil
}

func buildPriorityByID(orderedIDs []string, base int) map[string]int {
	if base <= 0 {
		base = taskPriorityBase
	}
	out := make(map[string]int, len(orderedIDs))
	for idx, taskID := range orderedIDs {
		out[taskID] = base + idx
	}
	return out
}

func findInsertPosition(tasks []domain.Task, insertAfterTaskID string) (int, error) {
	insertAfterTaskID = strings.TrimSpace(insertAfterTaskID)
	if insertAfterTaskID == "" {
		return len(tasks), nil
	}
	for idx, task := range tasks {
		if task.ID == insertAfterTaskID {
			return idx + 1, nil
		}
	}
	return 0, ErrTaskNotFound
}

func reorderTaskIDs(tasks []domain.Task, taskID string, insertAfterTaskID string) ([]string, error) {
	taskID = strings.TrimSpace(taskID)
	insertAfterTaskID = strings.TrimSpace(insertAfterTaskID)
	if taskID == "" {
		return nil, ErrTaskNotFound
	}
	if insertAfterTaskID == taskID {
		return nil, fmt.Errorf("cannot insert task after itself")
	}

	orderedIDs := make([]string, 0, len(tasks))
	originalIndex := -1
	for idx, task := range tasks {
		if task.ID == taskID {
			originalIndex = idx
			continue
		}
		orderedIDs = append(orderedIDs, task.ID)
	}
	if originalIndex < 0 {
		return nil, ErrTaskNotFound
	}

	insertPos := originalIndex
	if insertPos > len(orderedIDs) {
		insertPos = len(orderedIDs)
	}
	if insertAfterTaskID != "" {
		found := false
		for idx, id := range orderedIDs {
			if id == insertAfterTaskID {
				insertPos = idx + 1
				found = true
				break
			}
		}
		if !found {
			return nil, ErrTaskNotFound
		}
	}

	orderedIDs = append(orderedIDs[:insertPos], append([]string{taskID}, orderedIDs[insertPos:]...)...)
	return orderedIDs, nil
}
