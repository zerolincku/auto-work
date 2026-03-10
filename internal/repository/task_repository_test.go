package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

type taskRepositoryFixture struct {
	ctx          context.Context
	sqlDB        *sql.DB
	taskRepo     *repository.TaskRepository
	projectRepo  *repository.ProjectRepository
	runRepo      *repository.RunRepository
	runEventRepo *repository.RunEventRepository
	agentRepo    *repository.AgentRepository
}

func TestTaskRepository_CreateBatchWithReorder(t *testing.T) {
	t.Parallel()

	t.Run("reorders inserted tasks within project", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-batch")
		otherProject := mustCreateProject(t, fixture, "project-batch-other")

		mustCreateTask(t, fixture, &domain.Task{ID: "task-a", ProjectID: project.ID, Title: "Task A", Description: "desc", Priority: 100, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-b", ProjectID: project.ID, Title: "Task B", Description: "desc", Priority: 101, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-c", ProjectID: project.ID, Title: "Task C", Description: "desc", Priority: 102, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-other", ProjectID: otherProject.ID, Title: "Other", Description: "desc", Priority: 100, Status: domain.TaskPending})

		batch := []*domain.Task{
			{ID: "task-x", Title: "Task X", Description: "desc"},
			{ID: "task-y", Title: "Task Y", Description: "desc"},
		}
		if err := fixture.taskRepo.CreateBatchWithReorder(fixture.ctx, project.ID, batch, "task-a"); err != nil {
			t.Fatalf("create batch with reorder: %v", err)
		}

		assertTaskOrder(t, mustListProjectTasks(t, fixture, project.ID), []string{"task-a", "task-x", "task-y", "task-b", "task-c"})
		assertTaskPriorities(t, mustListProjectTasks(t, fixture, project.ID), []int{100, 101, 102, 103, 104})
		assertTaskOrder(t, mustListProjectTasks(t, fixture, otherProject.ID), []string{"task-other"})

		if batch[0].ProjectID != project.ID || batch[1].ProjectID != project.ID {
			t.Fatalf("expected inserted tasks to inherit project %s", project.ID)
		}
		if batch[0].Priority != 101 || batch[1].Priority != 102 {
			t.Fatalf("unexpected inserted priorities: %d %d", batch[0].Priority, batch[1].Priority)
		}
		if batch[0].Status != domain.TaskPending || batch[1].Status != domain.TaskPending {
			t.Fatalf("expected pending status for inserted tasks, got %s and %s", batch[0].Status, batch[1].Status)
		}
	})

	t.Run("returns not found when insert anchor is missing", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-batch-missing")
		mustCreateTask(t, fixture, &domain.Task{ID: "task-a", ProjectID: project.ID, Title: "Task A", Description: "desc", Priority: 100, Status: domain.TaskPending})

		err := fixture.taskRepo.CreateBatchWithReorder(fixture.ctx, project.ID, []*domain.Task{{ID: "task-x", Title: "Task X", Description: "desc"}}, "missing-task")
		if !errors.Is(err, repository.ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}
	})
}

func TestTaskRepository_UpdateNonRunningTaskWithReorder(t *testing.T) {
	t.Parallel()

	t.Run("reorders and preserves runtime fields", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-update")
		nextRetryAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
		createdAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
		updatedAt := createdAt.Add(15 * time.Minute)

		mustCreateTask(t, fixture, &domain.Task{ID: "task-a", ProjectID: project.ID, Title: "Task A", Description: "desc", Priority: 100, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-b", ProjectID: project.ID, Title: "Task B", Description: "desc", Priority: 101, Status: domain.TaskFailed, Provider: "codex", RetryCount: 2, MaxRetries: 5, NextRetryAt: &nextRetryAt, CreatedAt: createdAt, UpdatedAt: updatedAt})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-c", ProjectID: project.ID, Title: "Task C", Description: "desc", Priority: 102, Status: domain.TaskPending})

		updated := &domain.Task{ID: "task-b", Title: "Task B Updated", Description: "updated desc", Priority: 999, Status: domain.TaskPending, Provider: "should-not-change"}
		if err := fixture.taskRepo.UpdateNonRunningTaskWithReorder(fixture.ctx, updated, "task-c"); err != nil {
			t.Fatalf("update with reorder: %v", err)
		}

		assertTaskOrder(t, mustListProjectTasks(t, fixture, project.ID), []string{"task-a", "task-c", "task-b"})
		assertTaskPriorities(t, mustListProjectTasks(t, fixture, project.ID), []int{100, 101, 102})

		persisted, err := fixture.taskRepo.GetByID(fixture.ctx, "task-b")
		if err != nil {
			t.Fatalf("get updated task: %v", err)
		}
		if persisted.Title != "Task B Updated" || persisted.Description != "updated desc" {
			t.Fatalf("unexpected task content after update: %+v", persisted)
		}
		if persisted.Status != domain.TaskFailed {
			t.Fatalf("expected failed status preserved, got %s", persisted.Status)
		}
		if persisted.Provider != "codex" {
			t.Fatalf("expected provider preserved, got %q", persisted.Provider)
		}
		if persisted.RetryCount != 2 || persisted.MaxRetries != 5 {
			t.Fatalf("expected retry fields preserved, got retry_count=%d max_retries=%d", persisted.RetryCount, persisted.MaxRetries)
		}
		if persisted.NextRetryAt == nil || !persisted.NextRetryAt.Equal(nextRetryAt) {
			t.Fatalf("expected next_retry_at preserved, got %v", persisted.NextRetryAt)
		}
		if !persisted.CreatedAt.Equal(createdAt) {
			t.Fatalf("expected created_at preserved, got %s", persisted.CreatedAt)
		}
		if !persisted.UpdatedAt.After(updatedAt) {
			t.Fatalf("expected updated_at to advance beyond %s, got %s", updatedAt, persisted.UpdatedAt)
		}
	})

	t.Run("rejects running task", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-update-running")
		mustCreateTask(t, fixture, &domain.Task{ID: "task-running", ProjectID: project.ID, Title: "Running", Description: "desc", Priority: 100, Status: domain.TaskRunning})

		err := fixture.taskRepo.UpdateNonRunningTaskWithReorder(fixture.ctx, &domain.Task{ID: "task-running", Title: "Updated", Description: "desc"}, "")
		if !errors.Is(err, repository.ErrTaskNotEditable) {
			t.Fatalf("expected ErrTaskNotEditable, got %v", err)
		}
	})
}

func TestTaskRepository_DeleteNonRunningTaskWithReorder(t *testing.T) {
	t.Parallel()

	t.Run("removes linked runtime rows and compacts priorities", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-delete")
		agent := mustCreateAgent(t, fixture, "agent-delete")

		mustCreateTask(t, fixture, &domain.Task{ID: "task-a", ProjectID: project.ID, Title: "Task A", Description: "desc", Priority: 100, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-b", ProjectID: project.ID, Title: "Task B", Description: "desc", Priority: 101, Status: domain.TaskDone})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-c", ProjectID: project.ID, Title: "Task C", Description: "desc", Priority: 102, Status: domain.TaskPending})

		finishedAt := time.Now().UTC().Truncate(time.Second)
		run := &domain.Run{
			ID:             "run-delete-1",
			TaskID:         "task-b",
			AgentID:        agent.ID,
			Attempt:        1,
			Status:         domain.RunDone,
			PromptSnapshot: "snapshot",
			StartedAt:      finishedAt.Add(-time.Minute),
			FinishedAt:     &finishedAt,
		}
		if err := fixture.runRepo.Create(fixture.ctx, run); err != nil {
			t.Fatalf("create run: %v", err)
		}
		if err := fixture.runEventRepo.Append(fixture.ctx, run.ID, "runner.stdout", "log line"); err != nil {
			t.Fatalf("append run event: %v", err)
		}
		if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `
INSERT INTO artifacts(id, run_id, kind, value, created_at)
VALUES (?, ?, ?, ?, ?)`, "artifact-delete-1", run.ID, "log", "artifact line", time.Now().UTC()); err != nil {
			t.Fatalf("insert artifact: %v", err)
		}

		if err := fixture.taskRepo.DeleteNonRunningTaskWithReorder(fixture.ctx, "task-b"); err != nil {
			t.Fatalf("delete task with reorder: %v", err)
		}

		if _, err := fixture.taskRepo.GetByID(fixture.ctx, "task-b"); !errors.Is(err, repository.ErrTaskNotFound) {
			t.Fatalf("expected deleted task to be missing, got %v", err)
		}
		assertTaskOrder(t, mustListProjectTasks(t, fixture, project.ID), []string{"task-a", "task-c"})
		assertTaskPriorities(t, mustListProjectTasks(t, fixture, project.ID), []int{100, 101})
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM runs WHERE task_id = ?`, 0, "task-b")
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM run_events WHERE run_id = ?`, 0, run.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM artifacts WHERE run_id = ?`, 0, run.ID)
	})

	t.Run("rejects running task", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-delete-running")
		mustCreateTask(t, fixture, &domain.Task{ID: "task-running", ProjectID: project.ID, Title: "Running", Description: "desc", Priority: 100, Status: domain.TaskRunning})

		err := fixture.taskRepo.DeleteNonRunningTaskWithReorder(fixture.ctx, "task-running")
		if !errors.Is(err, repository.ErrTaskNotDeletable) {
			t.Fatalf("expected ErrTaskNotDeletable, got %v", err)
		}
	})
}

func TestTaskRepository_ScheduleRetry(t *testing.T) {
	t.Parallel()

	t.Run("schedules failed task with unlimited retry budget", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-retry")
		mustCreateTask(t, fixture, &domain.Task{ID: "task-failed", ProjectID: project.ID, Title: "Failed", Description: "desc", Priority: 100, Status: domain.TaskFailed, RetryCount: 1, MaxRetries: 4})

		nextRetryAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
		retryCount, maxRetries, scheduled, err := fixture.taskRepo.ScheduleRetry(fixture.ctx, "task-failed", nextRetryAt)
		if err != nil {
			t.Fatalf("schedule retry: %v", err)
		}
		if !scheduled {
			t.Fatalf("expected retry to be scheduled")
		}
		if retryCount != 2 || maxRetries != 0 {
			t.Fatalf("unexpected retry response retry_count=%d max_retries=%d", retryCount, maxRetries)
		}

		persisted, err := fixture.taskRepo.GetByID(fixture.ctx, "task-failed")
		if err != nil {
			t.Fatalf("get failed task: %v", err)
		}
		if persisted.RetryCount != 2 || persisted.MaxRetries != 0 {
			t.Fatalf("expected persisted retry fields updated, got retry_count=%d max_retries=%d", persisted.RetryCount, persisted.MaxRetries)
		}
		if persisted.NextRetryAt == nil || !persisted.NextRetryAt.Equal(nextRetryAt) {
			t.Fatalf("expected next_retry_at=%s, got %v", nextRetryAt, persisted.NextRetryAt)
		}
	})

	t.Run("returns current counters when task is not failed", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-retry-pending")
		mustCreateTask(t, fixture, &domain.Task{ID: "task-pending", ProjectID: project.ID, Title: "Pending", Description: "desc", Priority: 100, Status: domain.TaskPending, RetryCount: 3, MaxRetries: 7})

		nextRetryAt := time.Now().UTC().Add(10 * time.Minute)
		retryCount, maxRetries, scheduled, err := fixture.taskRepo.ScheduleRetry(fixture.ctx, "task-pending", nextRetryAt)
		if err != nil {
			t.Fatalf("schedule retry for pending task: %v", err)
		}
		if scheduled {
			t.Fatalf("expected pending task to stay unscheduled")
		}
		if retryCount != 3 || maxRetries != 7 {
			t.Fatalf("unexpected response retry_count=%d max_retries=%d", retryCount, maxRetries)
		}

		persisted, err := fixture.taskRepo.GetByID(fixture.ctx, "task-pending")
		if err != nil {
			t.Fatalf("get pending task: %v", err)
		}
		if persisted.NextRetryAt != nil {
			t.Fatalf("expected next_retry_at to remain nil, got %v", persisted.NextRetryAt)
		}
		if persisted.RetryCount != 3 || persisted.MaxRetries != 7 {
			t.Fatalf("unexpected persisted counters retry_count=%d max_retries=%d", persisted.RetryCount, persisted.MaxRetries)
		}
	})
}

func TestTaskRepository_PromoteFailedRetryToPending(t *testing.T) {
	t.Parallel()

	t.Run("promotes due failed retry back to pending", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-promote-due")
		dueRetryAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
		mustCreateTask(t, fixture, &domain.Task{ID: "task-due", ProjectID: project.ID, Title: "Due", Description: "desc", Priority: 100, Status: domain.TaskFailed, RetryCount: 2, NextRetryAt: &dueRetryAt})

		if err := fixture.taskRepo.PromoteFailedRetryToPending(fixture.ctx, "task-due"); err != nil {
			t.Fatalf("promote due retry: %v", err)
		}

		persisted, err := fixture.taskRepo.GetByID(fixture.ctx, "task-due")
		if err != nil {
			t.Fatalf("get promoted task: %v", err)
		}
		if persisted.Status != domain.TaskPending {
			t.Fatalf("expected pending after promotion, got %s", persisted.Status)
		}
		if persisted.NextRetryAt != nil {
			t.Fatalf("expected next_retry_at cleared, got %v", persisted.NextRetryAt)
		}
		if persisted.RetryCount != 2 {
			t.Fatalf("expected retry_count preserved, got %d", persisted.RetryCount)
		}
	})

	t.Run("returns not found when retry is not due yet", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-promote-future")
		futureRetryAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		mustCreateTask(t, fixture, &domain.Task{ID: "task-future", ProjectID: project.ID, Title: "Future", Description: "desc", Priority: 100, Status: domain.TaskFailed, RetryCount: 1, NextRetryAt: &futureRetryAt})

		err := fixture.taskRepo.PromoteFailedRetryToPending(fixture.ctx, "task-future")
		if !errors.Is(err, repository.ErrTaskNotFound) {
			t.Fatalf("expected ErrTaskNotFound, got %v", err)
		}

		persisted, getErr := fixture.taskRepo.GetByID(fixture.ctx, "task-future")
		if getErr != nil {
			t.Fatalf("get future task: %v", getErr)
		}
		if persisted.Status != domain.TaskFailed {
			t.Fatalf("expected task to stay failed, got %s", persisted.Status)
		}
	})
}

func TestTaskRepository_NextPendingProvider(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	projectOne := mustCreateProject(t, fixture, "project-provider-one")
	projectTwo := mustCreateProject(t, fixture, "project-provider-two")
	projectThree := mustCreateProject(t, fixture, "project-provider-empty")

	mustCreateTask(t, fixture, &domain.Task{ID: "task-project-one-slow", ProjectID: projectOne.ID, Title: "Slow", Description: "desc", Priority: 200, Status: domain.TaskPending, Provider: "claude"})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-project-one-fast", ProjectID: projectOne.ID, Title: "Fast", Description: "desc", Priority: 100, Status: domain.TaskPending, Provider: " codex "})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-project-one-failed", ProjectID: projectOne.ID, Title: "Failed", Description: "desc", Priority: 10, Status: domain.TaskFailed, Provider: "ignored"})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-project-two", ProjectID: projectTwo.ID, Title: "Global First", Description: "desc", Priority: 50, Status: domain.TaskPending, Provider: " claude "})

	provider, err := fixture.taskRepo.NextPendingProvider(fixture.ctx, projectOne.ID)
	if err != nil {
		t.Fatalf("next pending provider by project: %v", err)
	}
	if provider != "codex" {
		t.Fatalf("expected codex for project scope, got %q", provider)
	}

	provider, err = fixture.taskRepo.NextPendingProvider(fixture.ctx, "")
	if err != nil {
		t.Fatalf("next pending provider globally: %v", err)
	}
	if provider != "claude" {
		t.Fatalf("expected global provider claude, got %q", provider)
	}

	provider, err = fixture.taskRepo.NextPendingProvider(fixture.ctx, projectThree.ID)
	if err != nil {
		t.Fatalf("next pending provider for empty project: %v", err)
	}
	if provider != "" {
		t.Fatalf("expected empty provider for project without pending tasks, got %q", provider)
	}
}

func setupTaskRepositoryFixture(t *testing.T) taskRepositoryFixture {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	return taskRepositoryFixture{
		ctx:          ctx,
		sqlDB:        sqlDB,
		taskRepo:     repository.NewTaskRepository(sqlDB),
		projectRepo:  repository.NewProjectRepository(sqlDB),
		runRepo:      repository.NewRunRepository(sqlDB),
		runEventRepo: repository.NewRunEventRepository(sqlDB),
		agentRepo:    repository.NewAgentRepository(sqlDB),
	}
}

func mustCreateProject(t *testing.T, fixture taskRepositoryFixture, id string) *domain.Project {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	project := &domain.Project{
		ID:              id,
		Name:            id,
		Path:            filepath.Join(t.TempDir(), id),
		DefaultProvider: "claude",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := fixture.projectRepo.Create(fixture.ctx, project); err != nil {
		t.Fatalf("create project %s: %v", id, err)
	}
	return project
}

func mustCreateAgent(t *testing.T, fixture taskRepositoryFixture, id string) *domain.Agent {
	t.Helper()

	agent := &domain.Agent{ID: id, Name: id, Provider: "claude", Enabled: true, Concurrency: 1}
	if err := fixture.agentRepo.Upsert(fixture.ctx, agent); err != nil {
		t.Fatalf("create agent %s: %v", id, err)
	}
	return agent
}

func mustCreateTask(t *testing.T, fixture taskRepositoryFixture, task *domain.Task) {
	t.Helper()
	if err := fixture.taskRepo.Create(fixture.ctx, task); err != nil {
		t.Fatalf("create task %s: %v", task.ID, err)
	}
}

func mustListProjectTasks(t *testing.T, fixture taskRepositoryFixture, projectID string) []domain.Task {
	t.Helper()
	tasks, err := fixture.taskRepo.List(fixture.ctx, repository.TaskListFilter{ProjectID: projectID, Limit: 20})
	if err != nil {
		t.Fatalf("list tasks for project %s: %v", projectID, err)
	}
	return tasks
}

func assertTaskOrder(t *testing.T, tasks []domain.Task, want []string) {
	t.Helper()
	got := make([]string, 0, len(tasks))
	for _, task := range tasks {
		got = append(got, task.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected task order: got %v want %v", got, want)
	}
}

func assertTaskPriorities(t *testing.T, tasks []domain.Task, want []int) {
	t.Helper()
	got := make([]int, 0, len(tasks))
	for _, task := range tasks {
		got = append(got, task.Priority)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected task priorities: got %v want %v", got, want)
	}
}

func assertRowCount(t *testing.T, fixture taskRepositoryFixture, query string, want int, args ...any) {
	t.Helper()
	var count int
	if err := fixture.sqlDB.QueryRowContext(fixture.ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("query row count: %v", err)
	}
	if count != want {
		t.Fatalf("unexpected row count: got %d want %d", count, want)
	}
}
