package project_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	sqlite "modernc.org/sqlite"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
	projectservice "auto-work/internal/service/project"
)

func TestService_Create_ValidateInput(t *testing.T) {
	t.Parallel()

	svc := projectservice.NewService(nil)
	tests := []struct {
		name  string
		input projectservice.CreateProjectInput
	}{
		{
			name: "empty name",
			input: projectservice.CreateProjectInput{
				Name: "   ",
				Path: "/tmp/project",
			},
		},
		{
			name: "empty path",
			input: projectservice.CreateProjectInput{
				Name: "demo",
				Path: "   ",
			},
		},
		{
			name: "invalid provider",
			input: projectservice.CreateProjectInput{
				Name:            "demo",
				Path:            "/tmp/project",
				DefaultProvider: "gpt",
			},
		},
		{
			name: "invalid failure policy",
			input: projectservice.CreateProjectInput{
				Name:          "demo",
				Path:          "/tmp/project",
				FailurePolicy: "skip",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := svc.Create(context.Background(), tt.input)
			if !errors.Is(err, projectservice.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got created=%v err=%v", created, err)
			}
		})
	}
}

func TestService_Create_NormalizeAndCleanPath(t *testing.T) {
	t.Parallel()

	ctx, _, svc := newProjectService(t)
	separator := string(filepath.Separator)
	rawPath := t.TempDir() + separator + "workspace" + separator + "nested" + separator + ".." + separator + "project"

	created, err := svc.Create(ctx, projectservice.CreateProjectInput{
		Name:                            "  Demo Project  ",
		Path:                            "  " + rawPath + "  ",
		DefaultProvider:                 "  CoDeX  ",
		Model:                           "  gpt-4.1  ",
		SystemPrompt:                    "  be helpful  ",
		FailurePolicy:                   "  CoNtInUe  ",
		FrontendScreenshotReportEnabled: true,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if created.Name != "Demo Project" {
		t.Fatalf("expected trimmed name, got %q", created.Name)
	}
	if created.Path != filepath.Clean(rawPath) {
		t.Fatalf("expected cleaned path %q, got %q", filepath.Clean(rawPath), created.Path)
	}
	if created.DefaultProvider != "codex" {
		t.Fatalf("expected provider codex, got %q", created.DefaultProvider)
	}
	if created.Model != "gpt-4.1" {
		t.Fatalf("expected trimmed model, got %q", created.Model)
	}
	if created.SystemPrompt != "be helpful" {
		t.Fatalf("expected trimmed system prompt, got %q", created.SystemPrompt)
	}
	if created.FailurePolicy != domain.ProjectFailurePolicyContinue {
		t.Fatalf("expected failure policy continue, got %q", created.FailurePolicy)
	}
	if created.AutoDispatchEnabled {
		t.Fatalf("expected auto dispatch disabled by default")
	}
	if !created.FrontendScreenshotReportEnabled {
		t.Fatalf("expected frontend screenshot report enabled")
	}
}

func TestService_Create_FillDefaultsWhenEmpty(t *testing.T) {
	t.Parallel()

	ctx, _, svc := newProjectService(t)

	created, err := svc.Create(ctx, projectservice.CreateProjectInput{
		Name: "Defaults Project",
		Path: filepath.Join(t.TempDir(), "defaults-project"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if created.DefaultProvider != "claude" {
		t.Fatalf("expected default provider claude, got %q", created.DefaultProvider)
	}
	if created.FailurePolicy != domain.ProjectFailurePolicyBlock {
		t.Fatalf("expected default failure policy block, got %q", created.FailurePolicy)
	}
}

func TestService_Create_PassthroughRepoError(t *testing.T) {
	t.Parallel()

	ctx, _, svc := newProjectService(t)
	path := filepath.Join(t.TempDir(), "duplicate-project")

	if _, err := svc.Create(ctx, projectservice.CreateProjectInput{
		Name: "Project One",
		Path: path,
	}); err != nil {
		t.Fatalf("create first project: %v", err)
	}

	_, err := svc.Create(ctx, projectservice.CreateProjectInput{
		Name: "Project Two",
		Path: path + string(filepath.Separator) + ".",
	})
	if err == nil {
		t.Fatalf("expected create error for duplicate cleaned path")
	}

	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		t.Fatalf("expected sqlite error passthrough, got %T %v", err, err)
	}
}

func TestService_Update_ValidateInput(t *testing.T) {
	t.Parallel()

	svc := projectservice.NewService(nil)
	tests := []struct {
		name  string
		input projectservice.UpdateProjectInput
	}{
		{
			name: "empty project id",
			input: projectservice.UpdateProjectInput{
				ProjectID: "   ",
				Name:      "demo",
			},
		},
		{
			name: "empty name",
			input: projectservice.UpdateProjectInput{
				ProjectID: "project-1",
				Name:      "   ",
			},
		},
		{
			name: "invalid provider",
			input: projectservice.UpdateProjectInput{
				ProjectID:       "project-1",
				Name:            "demo",
				DefaultProvider: "gpt",
			},
		},
		{
			name: "invalid failure policy",
			input: projectservice.UpdateProjectInput{
				ProjectID:     "project-1",
				Name:          "demo",
				FailurePolicy: "skip",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updated, err := svc.Update(context.Background(), tt.input)
			if !errors.Is(err, projectservice.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got updated=%v err=%v", updated, err)
			}
		})
	}
}

func TestService_Update_NormalizeProviderAndFailurePolicy(t *testing.T) {
	t.Parallel()

	ctx, repo, svc := newProjectService(t)
	project := seedProject(t, ctx, repo, "Original", filepath.Join(t.TempDir(), "original"))

	updated, err := svc.Update(ctx, projectservice.UpdateProjectInput{
		ProjectID:                       "  " + project.ID + "  ",
		Name:                            "  Updated Project  ",
		DefaultProvider:                 "  CoDeX  ",
		Model:                           "  o3-mini  ",
		SystemPrompt:                    "  keep it short  ",
		FailurePolicy:                   "  CoNtInUe  ",
		FrontendScreenshotReportEnabled: true,
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}

	if updated.Name != "Updated Project" {
		t.Fatalf("expected trimmed name, got %q", updated.Name)
	}
	if updated.DefaultProvider != "codex" {
		t.Fatalf("expected provider codex, got %q", updated.DefaultProvider)
	}
	if updated.Model != "o3-mini" {
		t.Fatalf("expected trimmed model, got %q", updated.Model)
	}
	if updated.SystemPrompt != "keep it short" {
		t.Fatalf("expected trimmed system prompt, got %q", updated.SystemPrompt)
	}
	if updated.FailurePolicy != domain.ProjectFailurePolicyContinue {
		t.Fatalf("expected failure policy continue, got %q", updated.FailurePolicy)
	}
	if !updated.FrontendScreenshotReportEnabled {
		t.Fatalf("expected frontend screenshot report enabled")
	}
}

func TestService_Update_FillDefaultsWhenEmpty(t *testing.T) {
	t.Parallel()

	ctx, repo, svc := newProjectService(t)
	project := seedProject(t, ctx, repo, "Original", filepath.Join(t.TempDir(), "original"))

	updated, err := svc.Update(ctx, projectservice.UpdateProjectInput{
		ProjectID: project.ID,
		Name:      "Defaults Project",
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}

	if updated.DefaultProvider != "claude" {
		t.Fatalf("expected default provider claude, got %q", updated.DefaultProvider)
	}
	if updated.FailurePolicy != domain.ProjectFailurePolicyBlock {
		t.Fatalf("expected default failure policy block, got %q", updated.FailurePolicy)
	}
}

func TestService_Update_PassthroughRepoError(t *testing.T) {
	t.Parallel()

	ctx, _, svc := newProjectService(t)

	_, err := svc.Update(ctx, projectservice.UpdateProjectInput{
		ProjectID:       "missing-project",
		Name:            "Updated Project",
		DefaultProvider: "claude",
		FailurePolicy:   string(domain.ProjectFailurePolicyBlock),
	})
	if !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestService_Delete_ValidateInput(t *testing.T) {
	t.Parallel()

	svc := projectservice.NewService(nil)
	if err := svc.Delete(context.Background(), "   "); !errors.Is(err, projectservice.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestService_Delete_PassthroughRepoError(t *testing.T) {
	t.Parallel()

	ctx, _, svc := newProjectService(t)
	if err := svc.Delete(ctx, "missing-project"); !errors.Is(err, repository.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func newProjectService(t *testing.T) (context.Context, *repository.ProjectRepository, *projectservice.Service) {
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

	repo := repository.NewProjectRepository(sqlDB)
	return ctx, repo, projectservice.NewService(repo)
}

func seedProject(t *testing.T, ctx context.Context, repo *repository.ProjectRepository, name, path string) *domain.Project {
	t.Helper()

	now := time.Now().UTC()
	project := &domain.Project{
		ID:            uuid.NewString(),
		Name:          name,
		Path:          path,
		CreatedAt:     now,
		UpdatedAt:     now,
		FailurePolicy: domain.ProjectFailurePolicyBlock,
	}
	if err := repo.Create(ctx, project); err != nil {
		t.Fatalf("create seed project: %v", err)
	}
	return project
}
