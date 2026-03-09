package telegrambot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

func TestParseTrailingLimit(t *testing.T) {
	t.Parallel()

	selector, limit := parseTrailingLimit("", 5, 20)
	if selector != "" || limit != 5 {
		t.Fatalf("unexpected empty parse result: selector=%q limit=%d", selector, limit)
	}

	selector, limit = parseTrailingLimit("proj-1 9", 5, 20)
	if selector != "proj-1" || limit != 9 {
		t.Fatalf("unexpected parsed result: selector=%q limit=%d", selector, limit)
	}

	selector, limit = parseTrailingLimit("支付 系统", 5, 20)
	if selector != "支付 系统" || limit != 5 {
		t.Fatalf("unexpected selector-only result: selector=%q limit=%d", selector, limit)
	}
}

func TestHelpText_IncludesSupportedCommands(t *testing.T) {
	t.Parallel()

	text := helpText()
	for _, cmd := range []string{"/addproject", "/setprovider", "/autodisp", "/addtask", "/dispatch", "/tasklogs", "/projects", "/project", "/pending", "/failed", "/running"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("expected help text contains %s, got:\n%s", cmd, text)
		}
	}
	for _, removed := range []string{"/newproject", "/provider", "/queue", "/status", "/latestlogs", "/newtask", "/autodisp_on", "/autodisp_off"} {
		if strings.Contains(text, removed) {
			t.Fatalf("expected help text not contains removed alias %s, got:\n%s", removed, text)
		}
	}
}

func TestHandleProjects_ListsProjectSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleProjects(ctx, "5")
	if !strings.Contains(msg, "项目列表") {
		t.Fatalf("unexpected response: %s", msg)
	}
	if !strings.Contains(msg, "项目1") || !strings.Contains(msg, "项目2") {
		t.Fatalf("expected both projects in response: %s", msg)
	}
	if !strings.Contains(msg, "默认 Provider: claude") || !strings.Contains(msg, "失败策略: 失败后继续后续任务(continue)") {
		t.Fatalf("expected provider/failure policy summary in response: %s", msg)
	}
}

func TestHandleProject_ShowsProjectDetailsAndPreviews(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleProject(ctx, "proj-1")
	for _, want := range []string{"项目详情", "自动派发: 已开启", "运行中:", "待执行:", "失败/阻塞:", "等待上线", "支付失败重试", "run-1"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected project details contain %q, got:\n%s", want, msg)
		}
	}
}

func TestHandlePending_ProjectScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handlePending(ctx, "proj-1 5")
	if !strings.Contains(msg, "待执行任务（项目: 项目1(proj-1)，最多 5 条）") {
		t.Fatalf("unexpected header: %s", msg)
	}
	if !strings.Contains(msg, "等待上线") {
		t.Fatalf("expected pending task title in response: %s", msg)
	}
	if strings.Contains(msg, "支付失败重试") {
		t.Fatalf("failed task should not appear in pending list: %s", msg)
	}
}

func TestHandleFailed_Global(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleFailed(ctx, "5")
	for _, want := range []string{"失败/阻塞任务", "支付失败重试", "人工确认", "重试:"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected failed list contains %q, got:\n%s", want, msg)
		}
	}
}

func TestHandleRunning_Global(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleRunning(ctx, "5")
	for _, want := range []string{"运行中任务", "编译回归测试", "run-1", "项目: 项目1(proj-1)", "状态: 执行中(running)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected running list contains %q, got:\n%s", want, msg)
		}
	}
}

func setupQueryServiceFixture(t *testing.T, ctx context.Context) *Service {
	t.Helper()

	svc, taskRepo, _ := setupAddTaskServiceFixture(t, ctx, 2)

	if err := svc.projectRepo.SetAutoDispatchEnabled(ctx, "proj-1", true); err != nil {
		t.Fatalf("enable auto dispatch: %v", err)
	}
	if err := svc.projectRepo.UpdateAIConfig(ctx, "proj-2", "codex", "gpt-5", "", domain.ProjectFailurePolicyContinue); err != nil {
		t.Fatalf("update project2 ai config: %v", err)
	}

	now := time.Now().UTC()
	mustCreateTask := func(task domain.Task) {
		t.Helper()
		if err := taskRepo.Create(ctx, &task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	mustCreateTask(domain.Task{
		ID:          "task-pending-1",
		ProjectID:   "proj-1",
		Title:       "等待上线",
		Description: "等待生产发布窗口",
		Priority:    100,
		Status:      domain.TaskPending,
		CreatedAt:   now.Add(-10 * time.Minute),
		UpdatedAt:   now.Add(-10 * time.Minute),
	})
	mustCreateTask(domain.Task{
		ID:          "task-running-1",
		ProjectID:   "proj-1",
		Title:       "编译回归测试",
		Description: "执行完整回归",
		Priority:    101,
		Status:      domain.TaskRunning,
		CreatedAt:   now.Add(-8 * time.Minute),
		UpdatedAt:   now.Add(-3 * time.Minute),
	})
	mustCreateTask(domain.Task{
		ID:          "task-failed-1",
		ProjectID:   "proj-1",
		Title:       "支付失败重试",
		Description: "补偿失败订单",
		Priority:    102,
		Status:      domain.TaskFailed,
		RetryCount:  1,
		MaxRetries:  3,
		CreatedAt:   now.Add(-20 * time.Minute),
		UpdatedAt:   now.Add(-2 * time.Minute),
	})
	nextRetry := now.Add(30 * time.Minute)
	mustCreateTask(domain.Task{
		ID:          "task-blocked-1",
		ProjectID:   "proj-1",
		Title:       "人工确认",
		Description: "等待人工审批",
		Priority:    103,
		Status:      domain.TaskBlocked,
		RetryCount:  2,
		MaxRetries:  5,
		NextRetryAt: &nextRetry,
		CreatedAt:   now.Add(-25 * time.Minute),
		UpdatedAt:   now.Add(-1 * time.Minute),
	})
	mustCreateTask(domain.Task{
		ID:          "task-pending-2",
		ProjectID:   "proj-2",
		Title:       "项目2待执行",
		Description: "补一条项目2任务",
		Priority:    100,
		Status:      domain.TaskPending,
		CreatedAt:   now.Add(-15 * time.Minute),
		UpdatedAt:   now.Add(-15 * time.Minute),
	})

	pid := 2233
	heartbeat := now.Add(-30 * time.Second)
	if err := svc.runRepo.Create(ctx, &domain.Run{
		ID:          "run-1",
		TaskID:      "task-running-1",
		AgentID:     "agent-1",
		Attempt:     1,
		Status:      domain.RunRunning,
		PID:         &pid,
		HeartbeatAt: &heartbeat,
		StartedAt:   now.Add(-6 * time.Minute),
		CreatedAt:   now.Add(-6 * time.Minute),
		UpdatedAt:   now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}

	return svc
}

func TestHandleProject_UnknownProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleProject(ctx, "not-exists")
	if !strings.Contains(msg, "查询项目失败") || !strings.Contains(msg, "未找到项目") {
		t.Fatalf("unexpected response: %s", msg)
	}
}

func TestHandleRunning_NoRunningTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleRunning(ctx, "")
	if msg != "当前没有运行中的任务。" {
		t.Fatalf("unexpected empty-running response: %s", msg)
	}
}

func TestRunStatusLabel_NeedsInput(t *testing.T) {
	t.Parallel()

	if got := runStatusLabel(domain.RunNeedsInput); got != "等待输入(needs_input)" {
		t.Fatalf("unexpected run status label: %s", got)
	}
}

func TestSetupQueryServiceFixture_UsesExpectedProjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)
	projects, err := svc.projectRepo.List(ctx, 10)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestHandlePending_GlobalIncludesProjectLabels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handlePending(ctx, "5")
	for _, want := range []string{"项目: 项目1(proj-1)", "项目: 项目2(proj-2)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected pending response contains %q, got:\n%s", want, msg)
		}
	}
}

func TestHandleFailed_ProjectScopedEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleFailed(ctx, "proj-2")
	if msg != `项目 "proj-2" 暂无失败或阻塞任务。` {
		t.Fatalf("unexpected empty failed response: %s", msg)
	}
}

func TestHandleRunning_ProjectScopedUnknown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleRunning(ctx, "not-exists 5")
	if !strings.Contains(msg, "查询运行中任务失败") {
		t.Fatalf("unexpected running failure response: %s", msg)
	}
}

func TestHandleProjects_CapsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := setupQueryServiceFixture(t, ctx)

	msg := svc.handleProjects(ctx, "999")
	if !strings.Contains(msg, "项目列表（最多 20 个）") {
		t.Fatalf("expected capped project limit in response: %s", msg)
	}
}

func TestAppendRunningRunItems_ProjectLabelFallback(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	appendRunningRunItems(&b, []repository.RunningRunRecord{{
		RunID:     "run-x",
		TaskID:    "task-x",
		TaskTitle: "执行任务",
		ProjectID: "proj-x",
		Status:    domain.RunRunning,
		StartedAt: time.Now(),
	}}, true, nil)
	if !strings.Contains(b.String(), "项目: proj-x") {
		t.Fatalf("expected project id fallback, got:\n%s", b.String())
	}
}

func TestAppendTaskItems_ProjectLabelFallback(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	appendTaskItems(&b, []domain.Task{{
		ID:        uuid.NewString(),
		ProjectID: "proj-x",
		Title:     "待办",
		Status:    domain.TaskPending,
		UpdatedAt: time.Now(),
	}}, true, false, nil)
	if !strings.Contains(b.String(), "项目: proj-x") {
		t.Fatalf("expected project id fallback, got:\n%s", b.String())
	}
}
