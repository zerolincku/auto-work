package app

import (
	"context"
	"strings"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/service/scheduler"
)

func (a *App) SetFrontendRunReporter(reporter func(FrontendRunNotification)) {
	a.frontendRunNotifyMu.Lock()
	a.frontendRunReporter = reporter
	a.frontendRunNotifyMu.Unlock()
}

func (a *App) notifyTaskStartedFrontend(task *domain.Task, run *domain.Run, agent domain.Agent) {
	if task == nil || run == nil {
		return
	}
	a.emitFrontendRunNotification(FrontendRunNotification{
		Kind:        "started",
		ProjectID:   strings.TrimSpace(task.ProjectID),
		ProjectName: a.projectNameForFrontendNotify(context.Background(), task),
		TaskID:      strings.TrimSpace(task.ID),
		TaskTitle:   strings.TrimSpace(task.Title),
		RunID:       strings.TrimSpace(run.ID),
		Status:      string(domain.TaskRunning),
		RunStatus:   string(domain.RunRunning),
		Provider:    normalizeFrontendRunProvider(task.Provider, agent.Provider),
		Attempt:     run.Attempt,
	})
}

func (a *App) onRunFinishedNotify(ctx context.Context, event scheduler.RunFinishedEvent) {
	a.onRunFinishedFrontendNotify(ctx, event)
	a.onRunFinishedTelegramNotify(ctx, event)
}

func (a *App) onRunFinishedFrontendNotify(ctx context.Context, event scheduler.RunFinishedEvent) {
	if event.TaskStatus != domain.TaskDone && event.TaskStatus != domain.TaskFailed && event.TaskStatus != domain.TaskBlocked {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	notifyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	task, err := a.taskRepo.GetByID(notifyCtx, strings.TrimSpace(event.TaskID))
	if err != nil {
		return
	}
	run, err := a.runRepo.GetByID(notifyCtx, strings.TrimSpace(event.RunID))
	if err != nil {
		return
	}

	a.emitFrontendRunNotification(FrontendRunNotification{
		Kind:        "finished",
		ProjectID:   strings.TrimSpace(task.ProjectID),
		ProjectName: a.projectNameForFrontendNotify(notifyCtx, task),
		TaskID:      strings.TrimSpace(task.ID),
		TaskTitle:   strings.TrimSpace(task.Title),
		RunID:       strings.TrimSpace(run.ID),
		Status:      string(event.TaskStatus),
		RunStatus:   string(event.RunStatus),
		Provider:    normalizeFrontendRunProvider(task.Provider, ""),
		Summary:     strings.TrimSpace(event.Summary),
		Attempt:     run.Attempt,
	})
}

func (a *App) emitFrontendRunNotification(event FrontendRunNotification) {
	a.frontendRunNotifyMu.RLock()
	reporter := a.frontendRunReporter
	a.frontendRunNotifyMu.RUnlock()
	if reporter == nil {
		return
	}
	reporter(event)
}

func (a *App) projectNameForFrontendNotify(ctx context.Context, task *domain.Task) string {
	if task == nil {
		return ""
	}
	if name := strings.TrimSpace(task.ProjectName); name != "" {
		return name
	}
	projectID := strings.TrimSpace(task.ProjectID)
	if projectID == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	project, err := a.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(project.Name)
}

func normalizeFrontendRunProvider(primary, fallback string) string {
	if provider := strings.ToLower(strings.TrimSpace(primary)); provider != "" {
		return provider
	}
	return strings.ToLower(strings.TrimSpace(fallback))
}
