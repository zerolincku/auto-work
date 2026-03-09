package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
	"auto-work/internal/runsignal"
	"auto-work/internal/service/scheduler"
)

var (
	ErrInvalidInput     = errors.New("invalid report input")
	ErrRunTaskMismatch  = errors.New("run_id and task_id mismatch")
	ErrRunNotInProgress = errors.New("run is not running")
)

type Service struct {
	runRepo      *repository.RunRepository
	projectRepo  *repository.ProjectRepository
	taskRepo     *repository.TaskRepository
	eventRepo    *repository.RunEventRepository
	dispatcher   *scheduler.Dispatcher
	expectedRun  string
	expectedTask string
}

type ResultInput struct {
	Status         string `json:"status"`
	Summary        string `json:"summary"`
	Details        string `json:"details"`
	TaskStatus     string `json:"task_status"`
	ExitCode       *int   `json:"exit_code"`
	IdempotencyKey string `json:"idempotency_key"`
}

func NewService(
	runRepo *repository.RunRepository,
	projectRepo *repository.ProjectRepository,
	taskRepo *repository.TaskRepository,
	eventRepo *repository.RunEventRepository,
	dispatcher *scheduler.Dispatcher,
	expectedRunID string,
	expectedTaskID string,
) *Service {
	return &Service{
		runRepo:      runRepo,
		projectRepo:  projectRepo,
		taskRepo:     taskRepo,
		eventRepo:    eventRepo,
		dispatcher:   dispatcher,
		expectedRun:  expectedRunID,
		expectedTask: expectedTaskID,
	}
}

func (s *Service) ReportResult(ctx context.Context, in ResultInput) (string, error) {
	if strings.TrimSpace(in.Status) == "" || strings.TrimSpace(in.Summary) == "" {
		return "", ErrInvalidInput
	}
	if !s.hasRunContext() {
		return "", fmt.Errorf("%w: report_result requires run_id and task_id context", ErrInvalidInput)
	}

	run, err := s.runRepo.GetByID(ctx, s.expectedRun)
	if err != nil {
		return "", err
	}
	if run.TaskID != s.expectedTask {
		return "", ErrRunTaskMismatch
	}

	_ = s.logEvent(ctx, run.ID, "mcp.report_result.request", map[string]any{
		"status":          in.Status,
		"summary":         in.Summary,
		"details":         in.Details,
		"task_status":     in.TaskStatus,
		"idempotency_key": in.IdempotencyKey,
		"exit_code":       in.ExitCode,
	})

	if run.Status != domain.RunRunning {
		return "already-finished", nil
	}

	runStatus, taskStatus, err := mapStatuses(in.Status, in.TaskStatus)
	if err != nil {
		return "", err
	}
	if runStatus == domain.RunDone && taskStatus == domain.TaskDone {
		if reason := s.findNeedsInputReason(ctx, run.ID); strings.TrimSpace(reason) != "" {
			runStatus = domain.RunNeedsInput
			taskStatus = domain.TaskBlocked
			in.Summary = "任务需要人工确认，已阻塞自动完成"
			if strings.TrimSpace(in.Details) == "" {
				in.Details = fmt.Sprintf("needs_input_reason=%s", strings.TrimSpace(reason))
			} else {
				in.Details = fmt.Sprintf("needs_input_reason=%s\n\noriginal_details:\n%s", strings.TrimSpace(reason), in.Details)
			}
			_ = s.logEvent(ctx, run.ID, "mcp.report_result.overridden", map[string]any{
				"from_run_status":  domain.RunDone,
				"to_run_status":    runStatus,
				"from_task_status": domain.TaskDone,
				"to_task_status":   taskStatus,
				"reason":           reason,
			})
		}
	}

	if err := s.dispatcher.MarkRunFinished(ctx, run.ID, runStatus, taskStatus, in.Summary, in.Details, in.ExitCode); err != nil {
		return "", err
	}

	_ = s.logEvent(ctx, run.ID, "mcp.report_result.applied", map[string]any{
		"run_status":  runStatus,
		"task_status": taskStatus,
	})
	return "ok", nil
}

type CreateTaskItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"` // ignored: MCP batch create appends to queue tail automatically
	Provider    string `json:"provider"` // ignored while pending; provider assigned when dispatch starts
}

type CreateTasksInput struct {
	Items             []CreateTaskItem `json:"items"`
	InsertAfterTaskID string           `json:"insert_after_task_id"`
	ProjectID         string           `json:"project_id"`
	ProjectName       string           `json:"project_name"`
	ProjectPath       string           `json:"project_path"`
}

type CreateTasksOutputItem struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Priority    int    `json:"priority"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

type UpdateTaskInput struct {
	TaskID            string  `json:"task_id"`
	Title             *string `json:"title"`
	Description       *string `json:"description"`
	InsertAfterTaskID string  `json:"insert_after_task_id"`
	ProjectID         string  `json:"project_id"`
	ProjectName       string  `json:"project_name"`
	ProjectPath       string  `json:"project_path"`
}

type UpdateTaskOutput struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

type DeleteTaskInput struct {
	TaskID      string `json:"task_id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	ProjectPath string `json:"project_path"`
}

type DeleteTaskOutput struct {
	TaskID      string `json:"task_id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

type TaskSummaryItem struct {
	TaskID      string     `json:"task_id"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	Provider    string     `json:"provider"`
	RetryCount  int        `json:"retry_count,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ProjectID   string     `json:"project_id"`
}

type TaskDetail struct {
	Task TaskSummaryItem     `json:"task"`
	Runs []TaskDetailRunItem `json:"runs"`
}

type TaskDetailRunItem struct {
	RunID         string     `json:"run_id"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty"`
	ResultSummary string     `json:"result_summary,omitempty"`
}

type ProjectSelectorInput struct {
	ProjectID   string
	ProjectName string
	ProjectPath string
}

func (s *Service) CreateTasks(ctx context.Context, in CreateTasksInput) ([]CreateTasksOutputItem, error) {
	if len(in.Items) == 0 || len(in.Items) > 50 {
		return nil, ErrInvalidInput
	}
	project, run, err := s.resolveProjectWithRunFallback(ctx, ProjectSelectorInput{
		ProjectID:   in.ProjectID,
		ProjectName: in.ProjectName,
		ProjectPath: in.ProjectPath,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	createdTasks := make([]*domain.Task, 0, len(in.Items))
	for _, item := range in.Items {
		title := strings.TrimSpace(item.Title)
		desc := strings.TrimSpace(item.Description)
		if title == "" || desc == "" {
			return nil, ErrInvalidInput
		}
		createdTasks = append(createdTasks, &domain.Task{
			ID:          uuid.NewString(),
			ProjectID:   project.ID,
			Title:       title,
			Description: desc,
			Status:      domain.TaskPending,
			Provider:    "",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if err := s.taskRepo.CreateBatchWithReorder(ctx, project.ID, createdTasks, in.InsertAfterTaskID); err != nil {
		return nil, err
	}

	out := make([]CreateTasksOutputItem, 0, len(createdTasks))
	for _, task := range createdTasks {
		out = append(out, CreateTasksOutputItem{
			TaskID:      task.ID,
			Title:       task.Title,
			Priority:    task.Priority,
			ProjectID:   task.ProjectID,
			ProjectName: project.Name,
		})
	}

	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.create_tasks.applied", map[string]any{
			"created_count":        len(out),
			"insert_after_task_id": strings.TrimSpace(in.InsertAfterTaskID),
			"items":                out,
		})
	}
	return out, nil
}

func (s *Service) UpdateTask(ctx context.Context, in UpdateTaskInput) (*UpdateTaskOutput, error) {
	task, project, run, err := s.resolveTaskWithScope(ctx, in.TaskID, ProjectSelectorInput{
		ProjectID:   in.ProjectID,
		ProjectName: in.ProjectName,
		ProjectPath: in.ProjectPath,
	})
	if err != nil {
		return nil, err
	}

	updated := *task
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, ErrInvalidInput
		}
		updated.Title = title
	}
	if in.Description != nil {
		desc := strings.TrimSpace(*in.Description)
		if desc == "" {
			return nil, ErrInvalidInput
		}
		updated.Description = desc
	}
	if strings.TrimSpace(in.InsertAfterTaskID) == updated.ID {
		return nil, ErrInvalidInput
	}
	if err := s.taskRepo.UpdateNonRunningTaskWithReorder(ctx, &updated, in.InsertAfterTaskID); err != nil {
		return nil, err
	}

	updatedTask, err := s.taskRepo.GetByID(ctx, updated.ID)
	if err != nil {
		return nil, err
	}
	out := &UpdateTaskOutput{
		TaskID:      updatedTask.ID,
		Title:       updatedTask.Title,
		Description: updatedTask.Description,
		Status:      string(updatedTask.Status),
		Priority:    updatedTask.Priority,
		ProjectID:   updatedTask.ProjectID,
		ProjectName: project.Name,
	}
	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.update_task.applied", map[string]any{
			"task_id":              out.TaskID,
			"priority":             out.Priority,
			"insert_after_task_id": strings.TrimSpace(in.InsertAfterTaskID),
		})
	}
	return out, nil
}

func (s *Service) DeleteTask(ctx context.Context, in DeleteTaskInput) (*DeleteTaskOutput, error) {
	task, project, run, err := s.resolveTaskWithScope(ctx, in.TaskID, ProjectSelectorInput{
		ProjectID:   in.ProjectID,
		ProjectName: in.ProjectName,
		ProjectPath: in.ProjectPath,
	})
	if err != nil {
		return nil, err
	}
	if err := s.taskRepo.DeleteNonRunningTaskWithReorder(ctx, task.ID); err != nil {
		return nil, err
	}
	out := &DeleteTaskOutput{
		TaskID:      task.ID,
		ProjectID:   task.ProjectID,
		ProjectName: project.Name,
	}
	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.delete_task.applied", map[string]any{
			"task_id": out.TaskID,
		})
	}
	return out, nil
}

func (s *Service) ListPendingTasks(ctx context.Context, limit int, selector ProjectSelectorInput) ([]TaskSummaryItem, error) {
	project, run, err := s.resolveProjectWithRunFallback(ctx, selector)
	if err != nil {
		return nil, err
	}
	items, err := s.taskRepo.List(ctx, repository.TaskListFilter{
		Status:    statusPtr(domain.TaskPending),
		ProjectID: project.ID,
		Limit:     normalizeLimit(limit, 100),
	})
	if err != nil {
		return nil, err
	}
	out := mapTaskSummaries(items)
	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.list_pending_tasks", map[string]any{
			"count": len(out),
		})
	}
	return out, nil
}

func (s *Service) ListHistoryTasks(ctx context.Context, limit int, selector ProjectSelectorInput) ([]TaskSummaryItem, error) {
	project, run, err := s.resolveProjectWithRunFallback(ctx, selector)
	if err != nil {
		return nil, err
	}
	items, err := s.taskRepo.ListByProjectAndStatuses(ctx, project.ID, []domain.TaskStatus{
		domain.TaskDone,
		domain.TaskFailed,
		domain.TaskBlocked,
	}, normalizeLimit(limit, 100))
	if err != nil {
		return nil, err
	}
	out := mapTaskSummaries(items)
	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.list_history_tasks", map[string]any{
			"count": len(out),
		})
	}
	return out, nil
}

func (s *Service) GetTaskDetail(ctx context.Context, taskID string, selector ProjectSelectorInput) (*TaskDetail, error) {
	project, run, err := s.resolveProjectWithRunFallback(ctx, selector)
	if err != nil {
		return nil, err
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, ErrInvalidInput
	}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.ProjectID != project.ID {
		return nil, ErrInvalidInput
	}
	runs, err := s.runRepo.ListByTask(ctx, task.ID, 20)
	if err != nil {
		return nil, err
	}

	detail := &TaskDetail{
		Task: mapTaskSummary(*task),
		Runs: make([]TaskDetailRunItem, 0, len(runs)),
	}
	for _, r := range runs {
		detail.Runs = append(detail.Runs, TaskDetailRunItem{
			RunID:         r.ID,
			Status:        string(r.Status),
			Attempt:       r.Attempt,
			StartedAt:     r.StartedAt,
			FinishedAt:    r.FinishedAt,
			ExitCode:      r.ExitCode,
			ResultSummary: r.ResultSummary,
		})
	}

	if run != nil {
		_ = s.logEvent(ctx, run.ID, "mcp.get_task_detail", map[string]any{
			"task_id": task.ID,
			"runs":    len(detail.Runs),
		})
	}
	return detail, nil
}

func (s *Service) logEvent(ctx context.Context, runID, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.eventRepo.Append(ctx, runID, kind, string(b))
}

func (s *Service) ensureActiveRun(ctx context.Context) (*domain.Run, error) {
	run, err := s.ensureRunContext(ctx)
	if err != nil {
		return nil, err
	}
	if run.Status != domain.RunRunning {
		return nil, ErrRunNotInProgress
	}
	return run, nil
}

func (s *Service) ensureRunContext(ctx context.Context) (*domain.Run, error) {
	run, err := s.runRepo.GetByID(ctx, s.expectedRun)
	if err != nil {
		return nil, err
	}
	if run.TaskID != s.expectedTask {
		return nil, ErrRunTaskMismatch
	}
	return run, nil
}

func (s *Service) sourceTaskInRun(ctx context.Context) (*domain.Task, *domain.Run, error) {
	run, err := s.ensureActiveRun(ctx)
	if err != nil {
		return nil, nil, err
	}
	sourceTask, err := s.taskRepo.GetByID(ctx, run.TaskID)
	if err != nil {
		return nil, nil, err
	}
	return sourceTask, run, nil
}

func (s *Service) hasRunContext() bool {
	return strings.TrimSpace(s.expectedRun) != "" && strings.TrimSpace(s.expectedTask) != ""
}

func (s *Service) resolveProjectWithRunFallback(ctx context.Context, selector ProjectSelectorInput) (*domain.Project, *domain.Run, error) {
	if s.hasRunContext() {
		sourceTask, run, err := s.sourceTaskInRun(ctx)
		if err != nil {
			return nil, nil, err
		}
		projectName := ""
		if s.projectRepo != nil {
			if project, getErr := s.projectRepo.GetByID(ctx, sourceTask.ProjectID); getErr == nil {
				projectName = strings.TrimSpace(project.Name)
			}
		}
		return &domain.Project{
			ID:   sourceTask.ProjectID,
			Name: projectName,
		}, run, nil
	}
	project, err := s.resolveProjectBySelector(ctx, selector)
	if err != nil {
		return nil, nil, err
	}
	return project, nil, nil
}

func (s *Service) resolveTaskWithScope(ctx context.Context, taskID string, selector ProjectSelectorInput) (*domain.Task, *domain.Project, *domain.Run, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil, nil, ErrInvalidInput
	}
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, nil, nil, err
	}

	if s.hasRunContext() {
		sourceTask, run, err := s.sourceTaskInRun(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		if task.ProjectID != sourceTask.ProjectID {
			return nil, nil, nil, ErrInvalidInput
		}
		projectName := ""
		if s.projectRepo != nil {
			if project, getErr := s.projectRepo.GetByID(ctx, sourceTask.ProjectID); getErr == nil {
				projectName = strings.TrimSpace(project.Name)
			}
		}
		return task, &domain.Project{ID: sourceTask.ProjectID, Name: projectName}, run, nil
	}

	if hasProjectSelector(selector) {
		project, err := s.resolveProjectBySelector(ctx, selector)
		if err != nil {
			return nil, nil, nil, err
		}
		if task.ProjectID != project.ID {
			return nil, nil, nil, ErrInvalidInput
		}
		return task, project, nil, nil
	}

	project := &domain.Project{ID: task.ProjectID}
	if s.projectRepo != nil && strings.TrimSpace(task.ProjectID) != "" {
		if item, getErr := s.projectRepo.GetByID(ctx, task.ProjectID); getErr == nil {
			project = item
		}
	}
	return task, project, nil, nil
}

func (s *Service) resolveProjectBySelector(ctx context.Context, selector ProjectSelectorInput) (*domain.Project, error) {
	if s.projectRepo == nil {
		return nil, fmt.Errorf("%w: project repository not available", ErrInvalidInput)
	}
	projectID := strings.TrimSpace(selector.ProjectID)
	if projectID != "" {
		project, err := s.projectRepo.GetByID(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return project, nil
	}

	projectName := strings.TrimSpace(selector.ProjectName)
	if projectName != "" {
		project, err := s.resolveProjectByName(ctx, projectName)
		if err != nil {
			return nil, err
		}
		return project, nil
	}

	projectPath := strings.TrimSpace(selector.ProjectPath)
	if projectPath != "" {
		project, err := s.resolveProjectByPath(ctx, projectPath)
		if err != nil {
			return nil, err
		}
		return project, nil
	}

	return nil, fmt.Errorf("%w: require one of project_id/project_name/project_path when run context is absent", ErrInvalidInput)
}

func hasProjectSelector(selector ProjectSelectorInput) bool {
	return strings.TrimSpace(selector.ProjectID) != "" ||
		strings.TrimSpace(selector.ProjectName) != "" ||
		strings.TrimSpace(selector.ProjectPath) != ""
}

func (s *Service) resolveProjectByName(ctx context.Context, name string) (*domain.Project, error) {
	items, err := s.projectRepo.List(ctx, 5000)
	if err != nil {
		return nil, err
	}
	matches := make([]domain.Project, 0, 2)
	for _, item := range items {
		if strings.TrimSpace(item.Name) == strings.TrimSpace(name) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		for _, item := range items {
			if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) {
				matches = append(matches, item)
			}
		}
	}
	if len(matches) == 1 {
		project := matches[0]
		return &project, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%w: project_name is ambiguous", ErrInvalidInput)
	}
	return nil, fmt.Errorf("%w: project_name not found", ErrInvalidInput)
}

func (s *Service) resolveProjectByPath(ctx context.Context, projectPath string) (*domain.Project, error) {
	normalizedInput, ok := normalizeAbsolutePathForMatch(projectPath)
	if !ok {
		return nil, fmt.Errorf("%w: project_path must be absolute path", ErrInvalidInput)
	}
	items, err := s.projectRepo.List(ctx, 5000)
	if err != nil {
		return nil, err
	}
	matches := make([]domain.Project, 0, 2)
	for _, item := range items {
		normalizedProjectPath, ok := normalizePathForMatch(item.Path)
		if !ok {
			continue
		}
		if normalizedProjectPath == normalizedInput {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		project := matches[0]
		return &project, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%w: project_path matches multiple projects", ErrInvalidInput)
	}
	return nil, fmt.Errorf("%w: project_path not found", ErrInvalidInput)
}

func normalizeAbsolutePathForMatch(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", false
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p), true
}

func normalizePathForMatch(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", false
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", false
		}
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p), true
}

func (s *Service) findNeedsInputReason(ctx context.Context, runID string) string {
	events, err := s.eventRepo.ListByRun(ctx, runID, 300)
	if err != nil {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind == "system.needs_input" {
			if reason := strings.TrimSpace(e.Payload); reason != "" {
				return reason
			}
			return "AI 需要人工输入"
		}
		if e.Kind != "claude.stdout" {
			continue
		}
		if reason := runsignal.ExtractNeedsInputReason(e.Payload); strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
	}
	return ""
}

func mapTaskSummaries(tasks []domain.Task) []TaskSummaryItem {
	out := make([]TaskSummaryItem, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, mapTaskSummary(t))
	}
	return out
}

func mapTaskSummary(t domain.Task) TaskSummaryItem {
	return TaskSummaryItem{
		TaskID:      t.ID,
		Title:       t.Title,
		Status:      string(t.Status),
		Priority:    t.Priority,
		Provider:    t.Provider,
		RetryCount:  t.RetryCount,
		NextRetryAt: t.NextRetryAt,
		UpdatedAt:   t.UpdatedAt,
		ProjectID:   t.ProjectID,
	}
}

func statusPtr(st domain.TaskStatus) *domain.TaskStatus {
	v := st
	return &v
}

func normalizeLimit(limit int, max int) int {
	if limit <= 0 {
		return 20
	}
	if limit > max {
		return max
	}
	return limit
}

func mapStatuses(status, taskStatusRaw string) (domain.RunStatus, domain.TaskStatus, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	taskStatusRaw = strings.ToLower(strings.TrimSpace(taskStatusRaw))

	if taskStatusRaw != "" {
		var ts domain.TaskStatus
		switch taskStatusRaw {
		case "done", "success":
			ts = domain.TaskDone
		case "failed", "error":
			ts = domain.TaskFailed
		case "blocked":
			ts = domain.TaskBlocked
		default:
			return "", "", fmt.Errorf("%w: unsupported task_status %q", ErrInvalidInput, taskStatusRaw)
		}
		switch status {
		case "success", "done", "completed":
			return domain.RunDone, ts, nil
		case "failed", "error":
			return domain.RunFailed, ts, nil
		case "blocked":
			return domain.RunNeedsInput, ts, nil
		default:
			return "", "", fmt.Errorf("%w: unsupported status %q", ErrInvalidInput, status)
		}
	}

	switch status {
	case "success", "done", "completed":
		return domain.RunDone, domain.TaskDone, nil
	case "failed", "error":
		return domain.RunFailed, domain.TaskFailed, nil
	case "blocked":
		return domain.RunNeedsInput, domain.TaskBlocked, nil
	default:
		return "", "", fmt.Errorf("%w: unsupported status %q", ErrInvalidInput, status)
	}
}
