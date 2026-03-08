package telegrambot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/domain"
)

func TestParseAddProjectArgs(t *testing.T) {
	t.Parallel()

	name, path, provider, failurePolicy, err := parseAddProjectArgs("支付系统 | /srv/payments | codex | continue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "支付系统" || path != "/srv/payments" || provider != "codex" || failurePolicy != "continue" {
		t.Fatalf("unexpected parsed values: %q %q %q %q", name, path, provider, failurePolicy)
	}
}

func TestParseSetProviderArgs(t *testing.T) {
	t.Parallel()

	projectSelector, provider, err := parseSetProviderArgs("项目A | codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projectSelector != "项目A" || provider != "codex" {
		t.Fatalf("unexpected parsed values: %q %q", projectSelector, provider)
	}
}

func TestHandleCreateProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 0)

	msg := svc.handleCreateProject(ctx, "支付系统 | /srv/payments | codex | continue")
	for _, want := range []string{"已创建项目", "支付系统", "/srv/payments", "默认 Provider: codex", "失败策略: 失败后继续后续任务(continue)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected create project response contains %q, got:\n%s", want, msg)
		}
	}
}

func TestHandleSetProjectProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, projectID := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleSetProjectProvider(ctx, projectID+" | codex")
	for _, want := range []string{"已更新项目默认 Provider", "默认 Provider: codex", projectID} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected provider update response contains %q, got:\n%s", want, msg)
		}
	}
}

func TestHandleDispatchTask_UsesInjectedCallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)
	svc.dispatchTask = func(_ context.Context, taskID string) (*DispatchTaskResult, error) {
		return &DispatchTaskResult{
			Claimed: true,
			RunID:   "run-123",
			TaskID:  taskID,
			Message: "task claimed",
		}, nil
	}

	msg := svc.handleDispatchTask(ctx, "task-123")
	for _, want := range []string{"派发结果", "任务: task-123", "run: run-123", "claimed: true"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected dispatch response contains %q, got:\n%s", want, msg)
		}
	}
}

func TestHandleTaskLatestLogs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, taskRepo, projectID := setupAddTaskServiceFixture(t, ctx, 1)
	now := time.Now().UTC()
	task := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   projectID,
		Title:       "Task Logs",
		Description: "Desc",
		Priority:    100,
		Status:      domain.TaskDone,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	run := &domain.Run{
		ID:        "run-tasklogs",
		TaskID:    task.ID,
		AgentID:   "agent-1",
		Attempt:   1,
		Status:    domain.RunDone,
		StartedAt: now.Add(-time.Minute),
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	}
	if err := svc.runRepo.Create(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := svc.eventRepo.Append(ctx, run.ID, "claude.stdout", `{"type":"assistant","message":{"content":[{"type":"text","text":"任务日志输出"}]}}`); err != nil {
		t.Fatalf("append run event: %v", err)
	}

	msg := svc.handleTaskLatestLogs(ctx, task.ID+" 10")
	for _, want := range []string{"最新运行日志", "run=run-tasklogs", "任务日志输出"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected task latest logs response contains %q, got:\n%s", want, msg)
		}
	}
}
