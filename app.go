package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	coreapp "auto-work/internal/app"
	"auto-work/internal/config"
	"auto-work/internal/domain"
	"auto-work/internal/integration/telegrambot"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx        context.Context
	mu         sync.RWMutex
	backend    *coreapp.App
	startupErr error
	ready      chan struct{}
	readyOnce  sync.Once
}

var errBackendNotReady = errors.New("backend not initialized")

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		ready: make(chan struct{}),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	backend, err := coreapp.New(ctx, config.Load())
	if err != nil {
		a.mu.Lock()
		a.startupErr = fmt.Errorf("initialize backend failed: %w", err)
		a.mu.Unlock()
		a.markReady()
		return
	}
	backend.SetTelegramIncomingReporter(func(message telegrambot.IncomingMessage) {
		runtime.EventsEmit(ctx, "telegram.incoming", message)
	})
	a.mu.Lock()
	a.backend = backend
	a.startupErr = nil
	a.mu.Unlock()
	a.markReady()
}

func (a *App) shutdown(ctx context.Context) {
	a.markReady()
	a.mu.RLock()
	backend := a.backend
	a.mu.RUnlock()
	if backend != nil {
		_ = backend.Close()
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

func (a *App) Health() string {
	backend, err := a.backendOrErr()
	if err != nil {
		return err.Error()
	}
	return backend.Health()
}

func (a *App) MCPStatus() (*coreapp.MCPStatusView, error) {
	backend, err := a.backendOrErr()
	if err != nil {
		return nil, err
	}
	return backend.MCPStatus(a.ctx)
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

func (a *App) backendOrErr() (*coreapp.App, error) {
	a.mu.RLock()
	backend := a.backend
	startupErr := a.startupErr
	ready := a.ready
	a.mu.RUnlock()
	if backend != nil {
		return backend, nil
	}
	if startupErr != nil {
		return nil, startupErr
	}
	if ready == nil {
		return nil, errBackendNotReady
	}

	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		a.mu.RLock()
		backend = a.backend
		startupErr = a.startupErr
		a.mu.RUnlock()
		if backend != nil {
			return backend, nil
		}
		if startupErr != nil {
			return nil, startupErr
		}
		return nil, errBackendNotReady
	case <-timer.C:
		return nil, errBackendNotReady
	}
}

func (a *App) markReady() {
	a.readyOnce.Do(func() {
		if a.ready != nil {
			close(a.ready)
		}
	})
}
