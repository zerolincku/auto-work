package repository_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

func TestProjectRepository_CreateUsesDefaultsAndGeneratedID(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	project := &domain.Project{
		Name: "project-generated",
		Path: filepath.Join(t.TempDir(), "project-generated"),
	}

	if err := fixture.projectRepo.Create(fixture.ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	if project.ID != "1" {
		t.Fatalf("expected generated project id 1, got %q", project.ID)
	}
	if project.DefaultProvider != "claude" {
		t.Fatalf("expected default provider claude, got %q", project.DefaultProvider)
	}
	if project.FailurePolicy != domain.ProjectFailurePolicyBlock {
		t.Fatalf("expected default failure policy block, got %q", project.FailurePolicy)
	}
	if project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be initialized, got created_at=%s updated_at=%s", project.CreatedAt, project.UpdatedAt)
	}

	persisted, err := fixture.projectRepo.GetByID(fixture.ctx, project.ID)
	if err != nil {
		t.Fatalf("get project by id: %v", err)
	}
	if persisted.Name != project.Name || persisted.Path != project.Path {
		t.Fatalf("unexpected persisted project: %+v", persisted)
	}
	if persisted.DefaultProvider != "claude" {
		t.Fatalf("expected persisted default provider claude, got %q", persisted.DefaultProvider)
	}
	if persisted.FailurePolicy != domain.ProjectFailurePolicyBlock {
		t.Fatalf("expected persisted failure policy block, got %q", persisted.FailurePolicy)
	}
}

func TestProjectRepository_UpdateAndDeleteWithRelatedData(t *testing.T) {
	t.Parallel()

	t.Run("updates project settings", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-update")

		if err := fixture.projectRepo.SetAutoDispatchEnabled(fixture.ctx, project.ID, true); err != nil {
			t.Fatalf("set auto dispatch enabled: %v", err)
		}
		enabled, err := fixture.projectRepo.AutoDispatchEnabled(fixture.ctx, project.ID)
		if err != nil {
			t.Fatalf("get auto dispatch enabled: %v", err)
		}
		if !enabled {
			t.Fatalf("expected auto dispatch enabled")
		}

		if err := fixture.projectRepo.UpdateAIConfig(fixture.ctx, project.ID, "  codex  ", " gpt-5 ", "  system prompt  ", domain.ProjectFailurePolicyContinue); err != nil {
			t.Fatalf("update ai config: %v", err)
		}
		if err := fixture.projectRepo.Update(fixture.ctx, project.ID, "  Project Updated  ", "", "  model-2  ", "  narrowed prompt  ", "", true); err != nil {
			t.Fatalf("update project: %v", err)
		}

		persisted, err := fixture.projectRepo.GetByID(fixture.ctx, project.ID)
		if err != nil {
			t.Fatalf("get updated project: %v", err)
		}
		if persisted.Name != "Project Updated" {
			t.Fatalf("expected trimmed name, got %q", persisted.Name)
		}
		if persisted.DefaultProvider != "claude" {
			t.Fatalf("expected blank provider to fall back to claude, got %q", persisted.DefaultProvider)
		}
		if persisted.Model != "model-2" {
			t.Fatalf("expected trimmed model, got %q", persisted.Model)
		}
		if persisted.SystemPrompt != "narrowed prompt" {
			t.Fatalf("expected trimmed system prompt, got %q", persisted.SystemPrompt)
		}
		if persisted.FailurePolicy != domain.ProjectFailurePolicyBlock {
			t.Fatalf("expected blank failure policy to fall back to block, got %q", persisted.FailurePolicy)
		}
		if !persisted.AutoDispatchEnabled {
			t.Fatalf("expected auto dispatch flag to remain true")
		}
		if !persisted.FrontendScreenshotReportEnabled {
			t.Fatalf("expected frontend screenshot reporting to be enabled")
		}

		projectIDs, err := fixture.projectRepo.ListAutoDispatchEnabledProjectIDs(fixture.ctx, 10)
		if err != nil {
			t.Fatalf("list auto dispatch enabled project ids: %v", err)
		}
		if !slices.Equal(projectIDs, []string{project.ID}) {
			t.Fatalf("unexpected auto dispatch project ids: %v", projectIDs)
		}
	})

	t.Run("deletes project and related runtime data only within scope", func(t *testing.T) {
		fixture := setupTaskRepositoryFixture(t)
		project := mustCreateProject(t, fixture, "project-delete")
		otherProject := mustCreateProject(t, fixture, "project-keep")
		agent := mustCreateAgent(t, fixture, "agent-project-delete")

		mustCreateTask(t, fixture, &domain.Task{ID: "task-project-delete", ProjectID: project.ID, Title: "Delete", Description: "desc", Priority: 100, Status: domain.TaskPending})
		mustCreateTask(t, fixture, &domain.Task{ID: "task-project-keep", ProjectID: otherProject.ID, Title: "Keep", Description: "desc", Priority: 100, Status: domain.TaskPending})

		startedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
		finishedAt := startedAt.Add(30 * time.Second)
		projectRun := &domain.Run{ID: "run-project-delete", TaskID: "task-project-delete", AgentID: agent.ID, PromptSnapshot: "snapshot", Status: domain.RunDone, StartedAt: startedAt, FinishedAt: &finishedAt}
		otherRun := &domain.Run{ID: "run-project-keep", TaskID: "task-project-keep", AgentID: agent.ID, PromptSnapshot: "snapshot", Status: domain.RunDone, StartedAt: startedAt, FinishedAt: &finishedAt}
		if err := fixture.runRepo.Create(fixture.ctx, projectRun); err != nil {
			t.Fatalf("create project run: %v", err)
		}
		if err := fixture.runRepo.Create(fixture.ctx, otherRun); err != nil {
			t.Fatalf("create other run: %v", err)
		}
		if err := fixture.runEventRepo.Append(fixture.ctx, projectRun.ID, "runner.stdout", "project output"); err != nil {
			t.Fatalf("append project run event: %v", err)
		}
		if err := fixture.runEventRepo.Append(fixture.ctx, otherRun.ID, "runner.stdout", "other output"); err != nil {
			t.Fatalf("append other run event: %v", err)
		}
		if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `
INSERT INTO artifacts(id, run_id, kind, value, created_at)
VALUES (?, ?, ?, ?, ?)`, "artifact-project-delete", projectRun.ID, "log", "project artifact", time.Now().UTC()); err != nil {
			t.Fatalf("insert project artifact: %v", err)
		}
		if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `
INSERT INTO artifacts(id, run_id, kind, value, created_at)
VALUES (?, ?, ?, ?, ?)`, "artifact-project-keep", otherRun.ID, "log", "other artifact", time.Now().UTC()); err != nil {
			t.Fatalf("insert other artifact: %v", err)
		}

		if err := fixture.projectRepo.DeleteWithRelatedData(fixture.ctx, project.ID); err != nil {
			t.Fatalf("delete project with related data: %v", err)
		}

		if _, err := fixture.projectRepo.GetByID(fixture.ctx, project.ID); !errors.Is(err, repository.ErrProjectNotFound) {
			t.Fatalf("expected deleted project to be missing, got %v", err)
		}
		if _, err := fixture.projectRepo.GetByID(fixture.ctx, otherProject.ID); err != nil {
			t.Fatalf("expected unrelated project to remain, got %v", err)
		}

		assertRowCount(t, fixture, `SELECT COUNT(1) FROM tasks WHERE project_id = ?`, 0, project.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM tasks WHERE project_id = ?`, 1, otherProject.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM runs WHERE task_id = ?`, 0, "task-project-delete")
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM runs WHERE task_id = ?`, 1, "task-project-keep")
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM run_events WHERE run_id = ?`, 0, projectRun.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM run_events WHERE run_id = ?`, 1, otherRun.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM artifacts WHERE run_id = ?`, 0, projectRun.ID)
		assertRowCount(t, fixture, `SELECT COUNT(1) FROM artifacts WHERE run_id = ?`, 1, otherRun.ID)

		if err := fixture.projectRepo.DeleteWithRelatedData(fixture.ctx, project.ID); !errors.Is(err, repository.ErrProjectNotFound) {
			t.Fatalf("expected ErrProjectNotFound on repeated delete, got %v", err)
		}
	})
}
