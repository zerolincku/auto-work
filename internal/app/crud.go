package app

import (
	"context"

	"auto-work/internal/domain"
	projectservice "auto-work/internal/service/project"
	taskservice "auto-work/internal/service/task"
)

func (a *App) CreateTask(ctx context.Context, req CreateTaskRequest) (*domain.Task, error) {
	return a.taskSvc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   req.ProjectID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		Provider:    req.Provider,
	})
}

func (a *App) UpdateTask(ctx context.Context, req UpdateTaskRequest) (*domain.Task, error) {
	return a.taskSvc.Update(ctx, taskservice.UpdateTaskInput{
		TaskID:      req.TaskID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
	})
}

func (a *App) DeleteTask(ctx context.Context, taskID string) error {
	return a.taskSvc.Delete(ctx, taskID)
}

func (a *App) CreateProject(ctx context.Context, req CreateProjectRequest) (*domain.Project, error) {
	return a.projectSvc.Create(ctx, projectservice.CreateProjectInput{
		Name:                            req.Name,
		Path:                            req.Path,
		DefaultProvider:                 req.DefaultProvider,
		Model:                           req.Model,
		SystemPrompt:                    req.SystemPrompt,
		FailurePolicy:                   req.FailurePolicy,
		FrontendScreenshotReportEnabled: req.FrontendScreenshotReportEnabled,
	})
}

func (a *App) UpdateProjectAIConfig(ctx context.Context, req UpdateProjectAIConfigRequest) (*domain.Project, error) {
	return a.projectSvc.UpdateAIConfig(ctx, projectservice.UpdateProjectAIInput{
		ProjectID:       req.ProjectID,
		DefaultProvider: req.DefaultProvider,
		Model:           req.Model,
		SystemPrompt:    req.SystemPrompt,
		FailurePolicy:   req.FailurePolicy,
	})
}

func (a *App) UpdateProject(ctx context.Context, req UpdateProjectRequest) (*domain.Project, error) {
	return a.projectSvc.Update(ctx, projectservice.UpdateProjectInput{
		ProjectID:                       req.ProjectID,
		Name:                            req.Name,
		DefaultProvider:                 req.DefaultProvider,
		Model:                           req.Model,
		SystemPrompt:                    req.SystemPrompt,
		FailurePolicy:                   req.FailurePolicy,
		FrontendScreenshotReportEnabled: req.FrontendScreenshotReportEnabled,
	})
}

func (a *App) DeleteProject(ctx context.Context, projectID string) error {
	return a.projectSvc.Delete(ctx, projectID)
}

func (a *App) ListProjects(ctx context.Context, limit int) ([]domain.Project, error) {
	return a.projectSvc.List(ctx, limit)
}

func (a *App) ListTasks(ctx context.Context, status, provider, projectID string, limit int) ([]domain.Task, error) {
	return a.taskSvc.List(ctx, status, provider, projectID, limit)
}

func (a *App) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	return a.taskSvc.UpdateStatus(ctx, taskID, status)
}

func (a *App) RetryTask(ctx context.Context, taskID string) error {
	return a.taskSvc.Retry(ctx, taskID)
}

func (a *App) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	return a.agentRepo.ListEnabled(ctx)
}
