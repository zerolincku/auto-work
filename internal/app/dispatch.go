package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
	"auto-work/internal/systemprompt"
)

const defaultProjectAgentConcurrency = 4

func (a *App) DispatchOnce(ctx context.Context, agentID, projectID string) (*DispatchResponse, error) {
	_ = a.releaseDueRetryTasks(ctx, 50)
	agent, err := a.resolveDispatchAgent(ctx, agentID, projectID)
	if err != nil {
		a.log.Errorf("resolve dispatch agent failed agent_id=%s project_id=%s err=%v", strings.TrimSpace(agentID), strings.TrimSpace(projectID), err)
		return nil, err
	}
	if msg, blocked := a.dispatchDisabledMessage(*agent); blocked {
		return &DispatchResponse{Claimed: false, Message: msg}, nil
	}

	promptSnapshot := fmt.Sprintf("auto dispatch at %s", time.Now().UTC().Format(time.RFC3339))
	task, run, err := a.dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, agent.Provider, projectID, promptSnapshot)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoRunnableTask) {
			return &DispatchResponse{Claimed: false, Message: "当前项目没有可执行任务"}, nil
		}
		a.log.Errorf("claim next task failed agent_id=%s provider=%s project_id=%s err=%v", agent.ID, agent.Provider, strings.TrimSpace(projectID), err)
		return nil, err
	}
	return a.finalizeDispatchedTask(ctx, *agent, task, run)
}

func (a *App) DispatchTask(ctx context.Context, taskID string) (*DispatchResponse, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task id is required")
	}
	_ = a.releaseDueRetryTasks(ctx, 50)

	task, err := a.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskPending {
		return &DispatchResponse{
			Claimed: false,
			TaskID:  task.ID,
			Message: fmt.Sprintf("任务当前状态为 %s，仅 pending 任务可手动派发", task.Status),
		}, nil
	}

	agent, err := a.resolveDispatchAgent(ctx, "", task.ProjectID)
	if err != nil {
		a.log.Errorf("resolve dispatch agent failed task_id=%s project_id=%s err=%v", task.ID, task.ProjectID, err)
		return nil, err
	}
	if msg, blocked := a.dispatchDisabledMessage(*agent); blocked {
		return &DispatchResponse{
			Claimed: false,
			TaskID:  task.ID,
			Message: msg,
		}, nil
	}

	promptSnapshot := fmt.Sprintf("manual dispatch task=%s at %s", task.ID, time.Now().UTC().Format(time.RFC3339))
	claimedTask, run, err := a.dispatcher.ClaimTaskForAgent(ctx, agent.ID, agent.Provider, task.ID, promptSnapshot)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoRunnableTask) {
			return &DispatchResponse{
				Claimed: false,
				TaskID:  task.ID,
				Message: "任务当前不可派发，可能是被失败策略阻塞，或对应 agent 正忙",
			}, nil
		}
		a.log.Errorf("claim specific task failed task_id=%s agent_id=%s provider=%s err=%v", task.ID, agent.ID, agent.Provider, err)
		return nil, err
	}
	return a.finalizeDispatchedTask(ctx, *agent, claimedTask, run)
}

func (a *App) finalizeDispatchedTask(ctx context.Context, agent domain.Agent, task *domain.Task, run *domain.Run) (*DispatchResponse, error) {
	a.log.Infof("task claimed task_id=%s run_id=%s provider=%s project_id=%s", task.ID, run.ID, strings.ToLower(strings.TrimSpace(agent.Provider)), task.ProjectID)

	projectSystemPrompt := ""
	frontendScreenshotReportEnabled := false
	if task.ProjectID != "" {
		project, getErr := a.projectRepo.GetByID(ctx, task.ProjectID)
		if getErr == nil {
			task.ProjectName = strings.TrimSpace(project.Name)
			task.ProjectPath = project.Path
			task.Model = strings.TrimSpace(project.Model)
			projectSystemPrompt = strings.TrimSpace(project.SystemPrompt)
			frontendScreenshotReportEnabled = project.FrontendScreenshotReportEnabled
		}
	}
	settings, settingsErr := a.settingsRepo.Get(ctx)
	if settingsErr != nil {
		a.log.Errorf("load global settings failed run_id=%s err=%v", run.ID, settingsErr)
		return nil, fmt.Errorf("load global settings: %w", settingsErr)
	}
	task.SystemPrompt = systemprompt.Compose(settings.SystemPrompt, projectSystemPrompt)
	if frontendScreenshotReportEnabled {
		task.SystemPrompt = appendFrontendScreenshotPromptHint(task.SystemPrompt)
		a.recordRunFrontendBaseline(run.ID, task.ProjectPath)
	}
	pid, startErr := a.startProviderRun(ctx, agent, *run, *task)
	if startErr != nil {
		exitCode := 127
		summary := fmt.Sprintf("%s start failed", strings.ToLower(strings.TrimSpace(agent.Provider)))
		_ = a.appendRunFailureLog(run.ID, summary, startErr.Error())
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.ID, domain.RunFailed, domain.TaskFailed, summary, startErr.Error(), &exitCode)
		a.log.Errorf("start provider run failed task_id=%s run_id=%s provider=%s err=%v", task.ID, run.ID, agent.Provider, startErr)
		return &DispatchResponse{
			Claimed: true,
			RunID:   run.ID,
			TaskID:  task.ID,
			Message: "任务启动失败，已记录失败日志",
		}, nil
	}
	if err := a.runRepo.AttachProcess(ctx, run.ID, pid); err != nil {
		_ = a.stopProviderRun(context.Background(), agent.Provider, run.ID)
		exitCode := 126
		_ = a.appendRunFailureLog(run.ID, "attach process pid failed", err.Error())
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.ID, domain.RunFailed, domain.TaskFailed, "attach process pid failed", err.Error(), &exitCode)
		a.log.Errorf("attach process pid failed run_id=%s pid=%d err=%v", run.ID, pid, err)
		return &DispatchResponse{
			Claimed: true,
			RunID:   run.ID,
			TaskID:  task.ID,
			Message: "任务进程附加失败，已记录失败日志",
		}, nil
	}
	_ = a.eventRepo.Append(context.Background(), run.ID, "system.started", fmt.Sprintf("%s process started pid=%d", strings.ToLower(strings.TrimSpace(agent.Provider)), pid))
	a.log.Infof("provider run started task_id=%s run_id=%s pid=%d provider=%s", task.ID, run.ID, pid, strings.ToLower(strings.TrimSpace(agent.Provider)))
	a.notifyTaskStarted(task, run, agent)

	return &DispatchResponse{
		Claimed: true,
		RunID:   run.ID,
		TaskID:  task.ID,
		Message: "task claimed",
	}, nil
}

func (a *App) dispatchDisabledMessage(agent domain.Agent) (string, bool) {
	if strings.EqualFold(agent.Provider, "claude") && !a.cfg.RunClaudeOnDispatch {
		return "当前环境未启用 Claude 执行（AUTO_WORK_RUN_CLAUDE_ON_DISPATCH=false）", true
	}
	if strings.EqualFold(agent.Provider, "codex") && !a.cfg.RunCodexOnDispatch {
		return "当前环境未启用 Codex 执行（AUTO_WORK_RUN_CODEX_ON_DISPATCH=false）", true
	}
	return "", false
}

func (a *App) FinishRun(ctx context.Context, req FinishRunRequest) error {
	runStatus, taskStatus, err := validateFinishStatus(req.Status, req.TaskStatus)
	if err != nil {
		return err
	}
	return a.dispatcher.MarkRunFinished(ctx, req.RunID, runStatus, taskStatus, req.Summary, req.Details, req.ExitCode)
}

func (a *App) resolveDispatchAgent(ctx context.Context, agentID, projectID string) (*domain.Agent, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID != "" {
		return a.agentRepo.GetByID(ctx, agentID)
	}

	provider := "claude"
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		if project, getErr := a.projectRepo.GetByID(ctx, projectID); getErr == nil {
			if strings.TrimSpace(project.DefaultProvider) != "" {
				provider = strings.ToLower(strings.TrimSpace(project.DefaultProvider))
			}
			return a.ensureProjectDispatchAgent(ctx, provider, project.ID, project.Name)
		}
	}
	return a.agentRepo.GetByID(ctx, defaultAgentIDForProvider(provider))
}

func (a *App) ensureProjectDispatchAgent(ctx context.Context, provider, projectID, projectName string) (*domain.Agent, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "claude"
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return a.agentRepo.GetByID(ctx, defaultAgentIDForProvider(provider))
	}

	now := time.Now().UTC()
	label := strings.TrimSpace(projectName)
	if label == "" {
		label = projectID
	}
	agentID := projectDispatchAgentID(provider, projectID)
	concurrency := defaultProjectAgentConcurrency
	if existing, err := a.agentRepo.GetByID(ctx, agentID); err == nil {
		if existing.Concurrency > 0 {
			concurrency = existing.Concurrency
		}
	} else if !errors.Is(err, repository.ErrAgentNotFound) {
		return nil, err
	}
	agent := &domain.Agent{
		ID:          agentID,
		Name:        fmt.Sprintf("%s Project Agent (%s)", providerDisplayName(provider), label),
		Provider:    provider,
		Enabled:     true,
		Concurrency: concurrency,
		LastSeenAt:  &now,
	}
	if err := a.agentRepo.Upsert(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func projectDispatchAgentID(provider, projectID string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "claude"
	}
	return fmt.Sprintf("project:%s:%s", provider, strings.TrimSpace(projectID))
}

func providerDisplayName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return "Codex"
	default:
		return "Claude"
	}
}

func (a *App) startProviderRun(ctx context.Context, agent domain.Agent, run domain.Run, task domain.Task) (int, error) {
	switch strings.ToLower(strings.TrimSpace(agent.Provider)) {
	case "claude":
		return a.claudeRunner.Start(ctx, run, task, agent)
	case "codex":
		return a.codexRunner.Start(ctx, run, task, agent)
	default:
		return 0, fmt.Errorf("unsupported provider: %s", agent.Provider)
	}
}

func (a *App) stopProviderRun(ctx context.Context, provider, runID string) error {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude":
		return a.claudeRunner.Stop(ctx, runID)
	case "codex":
		return a.codexRunner.Stop(ctx, runID)
	default:
		return errors.New("unsupported provider")
	}
}

func (a *App) releaseDueRetryTasks(ctx context.Context, limit int) int {
	ids, err := a.taskRepo.ListDueRetryTaskIDs(ctx, time.Now().UTC(), limit)
	if err != nil {
		return 0
	}
	released := 0
	for _, taskID := range ids {
		if err := a.taskRepo.PromoteFailedRetryToPending(ctx, taskID); err != nil {
			if errors.Is(err, repository.ErrTaskNotFound) {
				continue
			}
			continue
		}
		released++
	}
	return released
}

func validateFinishStatus(runStatus, taskStatus string) (domain.RunStatus, domain.TaskStatus, error) {
	rs := domain.RunStatus(runStatus)
	ts := domain.TaskStatus(taskStatus)

	switch rs {
	case domain.RunDone, domain.RunFailed, domain.RunNeedsInput, domain.RunCancelled:
	default:
		return "", "", errors.New("invalid run status")
	}
	switch ts {
	case domain.TaskDone, domain.TaskFailed, domain.TaskBlocked:
	default:
		return "", "", errors.New("invalid task status")
	}
	return rs, ts, nil
}
