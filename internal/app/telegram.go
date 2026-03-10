package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/integration/telegrambot"
	"auto-work/internal/service/scheduler"
)

func (a *App) stopTelegramService() {
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramSvc = nil
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	svc.Stop(stopCtx)
	cancel()
}

func (a *App) SetTelegramIncomingReporter(reporter func(telegrambot.IncomingMessage)) {
	a.telegramIncomingMu.Lock()
	a.telegramIncomingReporter = reporter
	a.telegramIncomingMu.Unlock()
}

func (a *App) handleTelegramIncomingMessage(message telegrambot.IncomingMessage) {
	a.telegramIncomingMu.RLock()
	reporter := a.telegramIncomingReporter
	a.telegramIncomingMu.RUnlock()
	if reporter == nil {
		return
	}
	reporter(message)
}

func (a *App) notifyTaskStarted(task *domain.Task, run *domain.Run, agent domain.Agent) {
	if task == nil || run == nil {
		return
	}
	a.notifyTaskStartedFrontend(task, run, agent)
	projectLabel := a.projectLabelForNotify(context.Background(), task.ProjectID)
	provider := strings.TrimSpace(task.Provider)
	if provider == "" {
		provider = strings.TrimSpace(agent.Provider)
	}
	if provider == "" {
		provider = "unknown"
	}
	msg := strings.TrimSpace(fmt.Sprintf(
		"🚀 任务启动\n项目: %s\n标题: %s\n任务ID: %s\nRun: %s (attempt=%d)\nProvider: %s\n优先级: %d\n时间: %s",
		projectLabel,
		trimTextForNotify(task.Title, 180),
		task.ID,
		run.ID,
		run.Attempt,
		strings.ToLower(strings.TrimSpace(provider)),
		task.Priority,
		time.Now().Local().Format("2006-01-02 15:04:05"),
	))
	a.sendTelegramNotification(msg)
}

func (a *App) onRunFinishedTelegramNotify(_ context.Context, event scheduler.RunFinishedEvent) {
	if event.TaskStatus != domain.TaskDone && event.TaskStatus != domain.TaskFailed && event.TaskStatus != domain.TaskBlocked {
		return
	}
	baselineConsumed := false
	defer func() {
		if !baselineConsumed {
			a.discardRunFrontendBaseline(event.RunID)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := a.taskRepo.GetByID(ctx, strings.TrimSpace(event.TaskID))
	if err != nil {
		return
	}
	run, err := a.runRepo.GetByID(ctx, strings.TrimSpace(event.RunID))
	if err != nil {
		return
	}

	projectLabel := a.projectLabelForNotify(ctx, task.ProjectID)
	stateLabel := "运行结束"
	icon := "ℹ️"
	switch event.TaskStatus {
	case domain.TaskDone:
		stateLabel = "任务完成"
		icon = "✅"
	case domain.TaskFailed:
		stateLabel = "任务失败"
		icon = "❌"
	case domain.TaskBlocked:
		stateLabel = "任务阻塞"
		icon = "⛔"
	}

	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		summary = "-"
	}
	msg := strings.TrimSpace(fmt.Sprintf(
		"%s %s\n项目: %s\n标题: %s\n任务ID: %s\nRun: %s (attempt=%d)\n状态: task=%s run=%s\n摘要: %s\n退出码: %s\n时间: %s",
		icon,
		stateLabel,
		projectLabel,
		trimTextForNotify(task.Title, 180),
		task.ID,
		run.ID,
		run.Attempt,
		task.Status,
		run.Status,
		trimTextForNotify(summary, 300),
		formatExitCodeForNotify(event.ExitCode),
		time.Now().Local().Format("2006-01-02 15:04:05"),
	))
	a.sendTelegramNotification(msg)

	if event.TaskStatus != domain.TaskDone {
		return
	}
	frontendChanges := a.consumeRunFrontendChanges(ctx, event.RunID, task.ProjectID)
	baselineConsumed = true
	if !a.projectFrontendScreenshotReportEnabled(ctx, task.ProjectID) {
		return
	}
	projectPath := strings.TrimSpace(task.ProjectPath)
	if projectPath == "" {
		projectPath = a.lookupProjectPath(ctx, task.ProjectID)
	}
	refs := extractAIScreenshotRefs(projectPath, event.Details, run.ResultDetails)
	if !decideScreenshotNotify(refs) {
		a.log.Debugf("skip screenshot notify: no screenshot refs run_id=%s task_id=%s frontend_changes=%d", run.ID, task.ID, len(frontendChanges))
		return
	}

	a.log.Infof("sending screenshot notification run_id=%s refs=%d local_refs=%d frontend_changes=%d", run.ID, len(refs.Ordered), len(refs.LocalPaths), len(frontendChanges))
	localPaths := existingImageFiles(refs.LocalPaths)
	if len(localPaths) > 0 {
		a.sendTelegramPhotos(buildFrontendScreenshotCaption(task, run, frontendChanges), localPaths)
		return
	}
	a.log.Warnf("screenshot refs found but no existing local image files run_id=%s", run.ID)
}

func (a *App) sendTelegramNotification(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	go svc.NotifyAll(text)
}

func (a *App) sendTelegramPhotos(caption string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	svc.NotifyAllPhotos(caption, filePaths)
}

func (a *App) projectLabelForNotify(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "-"
	}
	project, err := a.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return projectID
	}
	name := strings.TrimSpace(project.Name)
	if name == "" {
		return projectID
	}
	return fmt.Sprintf("%s(%s)", name, projectID)
}

func trimTextForNotify(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return text
	}
	runes := []rune(text)
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func formatExitCodeForNotify(exitCode *int) string {
	if exitCode == nil {
		return "-"
	}
	return strconv.Itoa(*exitCode)
}
