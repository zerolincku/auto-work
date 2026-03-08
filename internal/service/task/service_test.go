package task_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
	taskservice "auto-work/internal/service/task"
)

func TestService_CreateAndList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	created, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task A",
		Description: "Desc A",
		Priority:    200,
		DependsOn:   []string{"dep-1", "dep-1", " "},
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected ID")
	}
	if len(created.DependsOn) != 1 {
		t.Fatalf("expected deduped deps, got %d", len(created.DependsOn))
	}

	items, err := svc.List(ctx, "pending", "", projectID, 10)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(items))
	}
}

func TestService_Create_AppendPriorityWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	_, err = svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Seed",
		Description: "Seed",
		Priority:    120,
	})
	if err != nil {
		t.Fatalf("create seed task: %v", err)
	}

	created, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Auto",
		Description: "Auto priority",
	})
	if err != nil {
		t.Fatalf("create auto-priority task: %v", err)
	}
	if created.Priority != 121 {
		t.Fatalf("expected priority 121, got %d", created.Priority)
	}
}

func TestService_UpdateStatus_Validate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task B",
		Description: "Desc B",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.UpdateStatus(ctx, task.ID, "done"); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "invalid"); err == nil {
		t.Fatalf("expected invalid status error")
	}
}

func TestService_Update_PendingTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Pending",
		Description: "Desc Pending",
		Priority:    100,
		DependsOn:   []string{"dep-1"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	updated, err := svc.Update(ctx, taskservice.UpdateTaskInput{
		TaskID:      task.ID,
		Title:       "Task Pending Updated",
		Description: "Desc Pending Updated",
		Priority:    80,
		DependsOn:   []string{"dep-2", "dep-2", " "},
	})
	if err != nil {
		t.Fatalf("update task: %v", err)
	}
	if updated.Title != "Task Pending Updated" {
		t.Fatalf("unexpected title: %s", updated.Title)
	}
	if updated.Description != "Desc Pending Updated" {
		t.Fatalf("unexpected description: %s", updated.Description)
	}
	if updated.Priority != 80 {
		t.Fatalf("unexpected priority: %d", updated.Priority)
	}
	if len(updated.DependsOn) != 1 || updated.DependsOn[0] != "dep-2" {
		t.Fatalf("unexpected depends_on: %#v", updated.DependsOn)
	}
}

func TestService_Update_RunningRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Running",
		Description: "Desc Running",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("set running: %v", err)
	}

	_, err = svc.Update(ctx, taskservice.UpdateTaskInput{
		TaskID:      task.ID,
		Title:       "Task Running Updated",
		Description: "Desc Running Updated",
		Priority:    10,
	})
	if !errors.Is(err, taskservice.ErrTaskNotEditable) {
		t.Fatalf("expected ErrTaskNotEditable, got %v", err)
	}
}

func TestService_Update_DoneTaskAllowed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Done",
		Description: "Desc Done",
		Priority:    90,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "done"); err != nil {
		t.Fatalf("set done: %v", err)
	}

	updated, err := svc.Update(ctx, taskservice.UpdateTaskInput{
		TaskID:      task.ID,
		Title:       "Task Done Updated",
		Description: "Desc Done Updated",
		Priority:    80,
		DependsOn:   []string{"dep-done"},
	})
	if err != nil {
		t.Fatalf("update done task: %v", err)
	}
	if updated.Title != "Task Done Updated" {
		t.Fatalf("unexpected title: %s", updated.Title)
	}
	if updated.Description != "Desc Done Updated" {
		t.Fatalf("unexpected description: %s", updated.Description)
	}
	if updated.Priority != 80 {
		t.Fatalf("unexpected priority: %d", updated.Priority)
	}
	if len(updated.DependsOn) != 1 || updated.DependsOn[0] != "dep-done" {
		t.Fatalf("unexpected depends_on: %#v", updated.DependsOn)
	}
}

func TestService_Delete_NonRunningTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Deletable",
		Description: "Desc Deletable",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "failed"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := svc.Delete(ctx, task.ID); err != nil {
		t.Fatalf("delete failed task: %v", err)
	}
	if _, err := repo.GetByID(ctx, task.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("expected task not found after delete, got %v", err)
	}
}

func TestService_Delete_RunningRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task Running",
		Description: "Desc Running",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := svc.Delete(ctx, task.ID); !errors.Is(err, taskservice.ErrTaskNotDeletable) {
		t.Fatalf("expected ErrTaskNotDeletable, got %v", err)
	}
}

func TestService_Retry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProject(t, ctx, projectRepo)

	task, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task C",
		Description: "Desc C",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.UpdateStatus(ctx, task.ID, "failed"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := svc.Retry(ctx, task.ID); err != nil {
		t.Fatalf("retry failed task: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != domain.TaskPending {
		t.Fatalf("expected pending after retry, got %s", got.Status)
	}
}

func TestService_Create_PendingProviderEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	repo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	svc := taskservice.NewService(repo, projectRepo)
	projectID := createProjectWithProvider(t, ctx, projectRepo, "codex")

	created, err := svc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task D",
		Description: "Desc D",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.Provider != "" {
		t.Fatalf("expected empty provider while pending, got %q", created.Provider)
	}
}

func createProject(t *testing.T, ctx context.Context, repo *repository.ProjectRepository) string {
	return createProjectWithProvider(t, ctx, repo, "claude")
}

func createProjectWithProvider(t *testing.T, ctx context.Context, repo *repository.ProjectRepository, provider string) string {
	t.Helper()
	now := time.Now().UTC()
	p := &domain.Project{
		ID:              uuid.NewString(),
		Name:            "Project-" + uuid.NewString()[:8],
		Path:            filepath.Join(t.TempDir(), "project"),
		DefaultProvider: provider,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p.ID
}
