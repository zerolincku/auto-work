package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
)

func TestDispatcher_ClaimNextTaskForAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	agent := &domain.Agent{
		ID:          "agent-1",
		Name:        "default",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	t1 := newTask("task-1", "first", 100)
	t2 := newTask("task-2", "second", 200)
	if err := taskRepo.Create(ctx, t1); err != nil {
		t.Fatalf("create t1: %v", err)
	}
	if err := taskRepo.Create(ctx, t2); err != nil {
		t.Fatalf("create t2: %v", err)
	}

	task, run, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-1")
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if task.ID != "task-1" {
		t.Fatalf("expect task-1, got %s", task.ID)
	}
	if run.TaskID != "task-1" {
		t.Fatalf("run task mismatch: %s", run.TaskID)
	}

	if _, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-2"); err == nil {
		t.Fatalf("expected no runnable task when agent has running run")
	}

	exitCode := 0
	if err := dispatcher.MarkRunFinished(ctx, run.ID, domain.RunDone, domain.TaskDone, "ok", "done", &exitCode); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	task2, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-3")
	if err != nil {
		t.Fatalf("claim second: %v", err)
	}
	if task2.ID != "task-2" {
		t.Fatalf("expect task-2, got %s", task2.ID)
	}
}

func TestDispatcher_FailedRunSchedulesRetry_AndNextAttemptIncrements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	agent := &domain.Agent{
		ID:          "agent-retry",
		Name:        "retry-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	task := newTask("task-retry", "retry", 100)
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed, run1, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-1")
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if claimed.ID != task.ID {
		t.Fatalf("unexpected claimed task: %s", claimed.ID)
	}
	if run1.Attempt != 1 {
		t.Fatalf("expected first attempt=1, got %d", run1.Attempt)
	}

	exitCode := 1
	if err := dispatcher.MarkRunFinished(ctx, run1.ID, domain.RunFailed, domain.TaskFailed, "failed", "boom", &exitCode); err != nil {
		t.Fatalf("mark run failed: %v", err)
	}

	taskAfterFailed, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task after failed: %v", err)
	}
	if taskAfterFailed.Status != domain.TaskFailed {
		t.Fatalf("expected failed status, got %s", taskAfterFailed.Status)
	}
	if taskAfterFailed.RetryCount != 1 {
		t.Fatalf("expected retry_count=1, got %d", taskAfterFailed.RetryCount)
	}
	if taskAfterFailed.MaxRetries != 0 {
		t.Fatalf("expected max_retries=0 (unlimited), got %d", taskAfterFailed.MaxRetries)
	}
	if taskAfterFailed.NextRetryAt == nil {
		t.Fatalf("expected next_retry_at set")
	}

	// Fast-forward due retry and re-queue.
	if _, err := sqlDB.ExecContext(ctx, `UPDATE tasks SET next_retry_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), task.ID); err != nil {
		t.Fatalf("force due retry: %v", err)
	}
	if err := taskRepo.PromoteFailedRetryToPending(ctx, task.ID); err != nil {
		t.Fatalf("promote retry to pending: %v", err)
	}

	_, run2, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-2")
	if err != nil {
		t.Fatalf("claim second: %v", err)
	}
	if run2.Attempt != 2 {
		t.Fatalf("expected second attempt=2, got %d", run2.Attempt)
	}

	if err := dispatcher.MarkRunFinished(ctx, run2.ID, domain.RunFailed, domain.TaskFailed, "failed again", "boom2", &exitCode); err != nil {
		t.Fatalf("mark second run failed: %v", err)
	}

	taskAfterSecondFailed, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task after second failed: %v", err)
	}
	if taskAfterSecondFailed.RetryCount != 2 {
		t.Fatalf("expected retry_count=2 after second failure, got %d", taskAfterSecondFailed.RetryCount)
	}
	if taskAfterSecondFailed.MaxRetries != 0 {
		t.Fatalf("expected max_retries still unlimited(0), got %d", taskAfterSecondFailed.MaxRetries)
	}
	if taskAfterSecondFailed.NextRetryAt == nil {
		t.Fatalf("expected next_retry_at after second failure")
	}
}

func TestDispatcher_MarkRunFinished_TriggersHook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	agent := &domain.Agent{
		ID:          "agent-hook",
		Name:        "hook-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	task := newTask("task-hook", "hook task", 100)
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, run, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", "", "prompt-hook")
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}

	events := make(chan scheduler.RunFinishedEvent, 1)
	dispatcher.SetRunFinishedHook(func(_ context.Context, event scheduler.RunFinishedEvent) {
		events <- event
	})

	exitCode := 0
	if err := dispatcher.MarkRunFinished(ctx, run.ID, domain.RunDone, domain.TaskDone, "ok", "done", &exitCode); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	select {
	case evt := <-events:
		if evt.RunID != run.ID {
			t.Fatalf("unexpected run id: %s", evt.RunID)
		}
		if evt.TaskID != task.ID {
			t.Fatalf("unexpected task id: %s", evt.TaskID)
		}
		if evt.TaskStatus != domain.TaskDone {
			t.Fatalf("unexpected task status: %s", evt.TaskStatus)
		}
		if evt.RunStatus != domain.RunDone {
			t.Fatalf("unexpected run status: %s", evt.RunStatus)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("run finished hook not triggered")
	}
}

func TestDispatcher_ClaimNextTaskForAgent_UsesProjectDefaultProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	project := &domain.Project{
		ID:              "project-provider",
		Name:            "provider-project",
		Path:            t.TempDir(),
		DefaultProvider: "codex",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := &domain.Agent{
		ID:          "agent-codex",
		Name:        "codex-agent",
		Provider:    "codex",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	task := &domain.Task{
		ID:          "task-provider",
		ProjectID:   project.ID,
		Title:       "provider task",
		Description: "desc",
		Priority:    100,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "codex", project.ID, "prompt-provider")
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed.ID != task.ID {
		t.Fatalf("expected claimed task %s, got %s", task.ID, claimed.ID)
	}

	persisted, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if persisted.Provider != "codex" {
		t.Fatalf("expected task provider overwritten to codex, got %s", persisted.Provider)
	}
}

func TestDispatcher_ClaimTaskForAgent_ClaimsSpecifiedPendingTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	agent := &domain.Agent{
		ID:          "agent-specific",
		Name:        "specific-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	first := newTask("task-specific-1", "first", 100)
	second := newTask("task-specific-2", "second", 200)
	if err := taskRepo.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := taskRepo.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}

	claimed, run, err := dispatcher.ClaimTaskForAgent(ctx, agent.ID, "claude", second.ID, "prompt-specific")
	if err != nil {
		t.Fatalf("claim specific task: %v", err)
	}
	if claimed.ID != second.ID {
		t.Fatalf("expected claimed task %s, got %s", second.ID, claimed.ID)
	}
	if run.TaskID != second.ID {
		t.Fatalf("expected run task %s, got %s", second.ID, run.TaskID)
	}

	persisted, err := taskRepo.GetByID(ctx, second.ID)
	if err != nil {
		t.Fatalf("get claimed task: %v", err)
	}
	if persisted.Status != domain.TaskRunning {
		t.Fatalf("expected claimed task running, got %s", persisted.Status)
	}
}

func TestDispatcher_BlockFailurePolicy_BlocksPendingTasksUntilRetryRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	project := &domain.Project{
		ID:              "project-block",
		Name:            "block-project",
		Path:            t.TempDir(),
		DefaultProvider: "claude",
		FailurePolicy:   domain.ProjectFailurePolicyBlock,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := &domain.Agent{
		ID:          "agent-block",
		Name:        "block-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	nextRetryAt := time.Now().UTC().Add(time.Minute)
	failedRetry := &domain.Task{
		ID:          "task-retry-block",
		ProjectID:   project.ID,
		Title:       "retry task",
		Description: "desc",
		Priority:    200,
		Status:      domain.TaskFailed,
		RetryCount:  1,
		NextRetryAt: &nextRetryAt,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	otherPending := &domain.Task{
		ID:          "task-pending-block",
		ProjectID:   project.ID,
		Title:       "other task",
		Description: "desc",
		Priority:    100,
		Status:      domain.TaskPending,
		CreatedAt:   time.Now().UTC().Add(time.Second),
		UpdatedAt:   time.Now().UTC().Add(time.Second),
	}
	if err := taskRepo.Create(ctx, failedRetry); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := taskRepo.Create(ctx, otherPending); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	if _, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-block-1"); !errors.Is(err, scheduler.ErrNoRunnableTask) {
		t.Fatalf("expected no runnable task while failed task unresolved, got %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, `UPDATE tasks SET next_retry_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), failedRetry.ID); err != nil {
		t.Fatalf("force due retry: %v", err)
	}
	if err := taskRepo.PromoteFailedRetryToPending(ctx, failedRetry.ID); err != nil {
		t.Fatalf("promote retry to pending: %v", err)
	}

	retried, run, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-block-2")
	if err != nil {
		t.Fatalf("claim retry task: %v", err)
	}
	if retried.ID != failedRetry.ID {
		t.Fatalf("expected retry task claimed first, got %s", retried.ID)
	}

	exitCode := 0
	if err := dispatcher.MarkRunFinished(ctx, run.ID, domain.RunDone, domain.TaskDone, "ok", "done", &exitCode); err != nil {
		t.Fatalf("finish retry run: %v", err)
	}

	nextTask, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-block-3")
	if err != nil {
		t.Fatalf("claim next pending task: %v", err)
	}
	if nextTask.ID != otherPending.ID {
		t.Fatalf("expected other pending task after retry success, got %s", nextTask.ID)
	}
}

func TestDispatcher_AllowsConcurrentClaimsOnlyForSamePriorityWithinProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	project := &domain.Project{
		ID:              "project-concurrent-priority",
		Name:            "concurrent-priority-project",
		Path:            t.TempDir(),
		DefaultProvider: "claude",
		FailurePolicy:   domain.ProjectFailurePolicyBlock,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := &domain.Agent{
		ID:          "agent-concurrent-priority",
		Name:        "concurrent-priority-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 2,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	first := newTask("task-concurrent-1", "first", 100)
	first.ProjectID = project.ID
	second := newTask("task-concurrent-2", "second", 100)
	second.ProjectID = project.ID
	third := newTask("task-concurrent-3", "third", 200)
	third.ProjectID = project.ID
	second.CreatedAt = first.CreatedAt.Add(time.Second)
	second.UpdatedAt = second.CreatedAt
	third.CreatedAt = second.CreatedAt.Add(time.Second)
	third.UpdatedAt = third.CreatedAt
	if err := taskRepo.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := taskRepo.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := taskRepo.Create(ctx, third); err != nil {
		t.Fatalf("create third: %v", err)
	}

	claimed1, run1, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-concurrent-1")
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if claimed1.ID != first.ID {
		t.Fatalf("expected first task, got %s", claimed1.ID)
	}

	claimed2, run2, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-concurrent-2")
	if err != nil {
		t.Fatalf("claim second same-priority task: %v", err)
	}
	if claimed2.ID != second.ID {
		t.Fatalf("expected second task, got %s", claimed2.ID)
	}

	if _, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-concurrent-3"); !errors.Is(err, scheduler.ErrNoRunnableTask) {
		t.Fatalf("expected no runnable task while concurrency full, got %v", err)
	}

	exitCode := 0
	if err := dispatcher.MarkRunFinished(ctx, run1.ID, domain.RunDone, domain.TaskDone, "ok", "done", &exitCode); err != nil {
		t.Fatalf("finish first run: %v", err)
	}

	if _, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-concurrent-4"); !errors.Is(err, scheduler.ErrNoRunnableTask) {
		t.Fatalf("expected lower-priority task blocked until same-priority batch completes, got %v", err)
	}

	if err := dispatcher.MarkRunFinished(ctx, run2.ID, domain.RunDone, domain.TaskDone, "ok", "done", &exitCode); err != nil {
		t.Fatalf("finish second run: %v", err)
	}

	claimed3, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-concurrent-5")
	if err != nil {
		t.Fatalf("claim third task after first batch finished: %v", err)
	}
	if claimed3.ID != third.ID {
		t.Fatalf("expected third task after first batch, got %s", claimed3.ID)
	}
}

func TestDispatcher_ContinueFailurePolicy_AllowsFollowUpDispatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := setupDB(t)
	taskRepo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	project := &domain.Project{
		ID:              "project-continue",
		Name:            "continue-project",
		Path:            t.TempDir(),
		DefaultProvider: "claude",
		FailurePolicy:   domain.ProjectFailurePolicyContinue,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent := &domain.Agent{
		ID:          "agent-continue",
		Name:        "continue-agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	failedTask := &domain.Task{
		ID:          "task-failed-continue",
		ProjectID:   project.ID,
		Title:       "failed task",
		Description: "desc",
		Priority:    100,
		Status:      domain.TaskFailed,
		RetryCount:  1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	pendingTask := &domain.Task{
		ID:          "task-pending-continue",
		ProjectID:   project.ID,
		Title:       "pending task",
		Description: "desc",
		Priority:    200,
		Status:      domain.TaskPending,
		CreatedAt:   time.Now().UTC().Add(time.Second),
		UpdatedAt:   time.Now().UTC().Add(time.Second),
	}
	if err := taskRepo.Create(ctx, failedTask); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := taskRepo.Create(ctx, pendingTask); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	claimed, _, err := dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, "claude", project.ID, "prompt-continue")
	if err != nil {
		t.Fatalf("claim pending task: %v", err)
	}
	if claimed.ID != pendingTask.ID {
		t.Fatalf("expected pending task claimed under continue policy, got %s", claimed.ID)
	}
}

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := migrate.Up(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return sqlDB
}

func newTask(id, title string, priority int) *domain.Task {
	now := time.Now().UTC()
	return &domain.Task{
		ID:          id,
		Title:       title,
		Description: title + "-desc",
		Priority:    priority,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
