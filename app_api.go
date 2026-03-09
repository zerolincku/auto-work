package main

import (
	coreapp "auto-work/internal/app"
	"auto-work/internal/domain"
)

func (a *App) Health() string {
	backend, err := a.backendOrErr()
	if err != nil {
		return err.Error()
	}
	return backend.Health()
}

func (a *App) MCPStatus(projectID string) (*coreapp.MCPStatusView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.MCPStatus(a.ctx, projectID)
}

func (a *App) GetGlobalSettings() (*coreapp.GlobalSettingsView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.GetGlobalSettings(a.ctx)
}

func (a *App) UpdateGlobalSettings(req coreapp.UpdateGlobalSettingsRequest) (*coreapp.GlobalSettingsView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.UpdateGlobalSettings(a.ctx, req)
}

func (a *App) AutoRunEnabled(projectID string) bool {
	backend, err := a.backendOrErr()
	if err != nil {
		return false
	}
	return backend.AutoRunEnabled(a.ctx, projectID)
}

func (a *App) SetAutoRunEnabled(projectID string, enabled bool) bool {
	backend, err := a.backendOrErr()
	if err != nil {
		return false
	}
	return backend.SetAutoRunEnabled(a.ctx, projectID, enabled)
}

func (a *App) CreateTask(req coreapp.CreateTaskRequest) (*domain.Task, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.CreateTask(a.ctx, req)
}

func (a *App) UpdateTask(req coreapp.UpdateTaskRequest) (*domain.Task, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.UpdateTask(a.ctx, req)
}

func (a *App) DeleteTask(taskID string) error {
	backend, err := a.backendOrErr()
	if err != nil {
		return err
	}
	return backend.DeleteTask(a.ctx, taskID)
}

func (a *App) CreateProject(req coreapp.CreateProjectRequest) (*domain.Project, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.CreateProject(a.ctx, req)
}

func (a *App) UpdateProjectAIConfig(req coreapp.UpdateProjectAIConfigRequest) (*domain.Project, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.UpdateProjectAIConfig(a.ctx, req)
}

func (a *App) UpdateProject(req coreapp.UpdateProjectRequest) (*domain.Project, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.UpdateProject(a.ctx, req)
}

func (a *App) DeleteProject(projectID string) error {
	backend, err := a.backendOrErr()
	if err != nil {
		return err
	}
	return backend.DeleteProject(a.ctx, projectID)
}

func (a *App) ListProjects(limit int) ([]domain.Project, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListProjects(a.ctx, limit)
}

func (a *App) ListTasks(status, provider, projectID string, limit int) ([]domain.Task, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListTasks(a.ctx, status, provider, projectID, limit)
}

func (a *App) UpdateTaskStatus(taskID, status string) error {
	backend, err := a.backendOrErr()
	if err != nil {
		return err
	}
	return backend.UpdateTaskStatus(a.ctx, taskID, status)
}

func (a *App) RetryTask(taskID string) error {
	backend, err := a.backendOrErr()
	if err != nil {
		return err
	}
	return backend.RetryTask(a.ctx, taskID)
}

func (a *App) ListAgents() ([]domain.Agent, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListAgents(a.ctx)
}

func (a *App) DispatchOnce(agentID, projectID string) (*coreapp.DispatchResponse, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.DispatchOnce(a.ctx, agentID, projectID)
}

func (a *App) FinishRun(req coreapp.FinishRunRequest) error {
	backend, err := a.backendOrErr()
	if err != nil {
		return err
	}
	return backend.FinishRun(a.ctx, req)
}

func (a *App) ListRunningRuns(projectID string, limit int) ([]coreapp.RunningRunView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListRunningRuns(a.ctx, projectID, limit)
}

func (a *App) ListRunLogs(runID string, limit int) ([]coreapp.RunLogEventView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListRunLogs(a.ctx, runID, limit)
}

func (a *App) ListSystemLogs(projectID string, limit int) ([]coreapp.SystemLogView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.ListSystemLogs(a.ctx, projectID, limit)
}

func (a *App) GetTaskLatestRun(taskID string) (*coreapp.TaskLatestRunView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.GetTaskLatestRun(a.ctx, taskID)
}

func (a *App) GetTaskDetail(taskID string) (*coreapp.TaskDetailView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.GetTaskDetail(a.ctx, taskID)
}
