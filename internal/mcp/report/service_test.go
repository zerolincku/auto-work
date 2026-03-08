package report_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	mcpreport "auto-work/internal/mcp/report"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
)

func TestReportResult_MarksRunAndTaskDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, taskRepo, runRepo, _, svc, run, task := setupFixture(t, ctx)
	_, err := svc.ReportResult(ctx, mcpreport.ResultInput{
		Status:  "success",
		Summary: "ok",
		Details: "all good",
	})
	if err != nil {
		t.Fatalf("report result: %v", err)
	}

	gotRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if gotRun.Status != domain.RunDone {
		t.Fatalf("expected run done, got %s", gotRun.Status)
	}

	gotTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status != domain.TaskDone {
		t.Fatalf("expected task done, got %s", gotTask.Status)
	}
}

func TestCreateTasks_Batch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, taskRepo, _, _, svc, _, task := setupFixture(t, ctx)
	created, err := svc.CreateTasks(ctx, mcpreport.CreateTasksInput{
		Items: []mcpreport.CreateTaskItem{
			{
				Title:       "后续任务A",
				Description: "补充测试覆盖",
				Priority:    180,
			},
			{
				Title:       "后续任务B",
				Description: "更新文档",
				DependsOn:   []string{"x", "x"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 created tasks, got %d", len(created))
	}

	createdTask, err := taskRepo.GetByID(ctx, created[0].TaskID)
	if err != nil {
		t.Fatalf("get created task: %v", err)
	}
	if createdTask.ProjectID != task.ProjectID {
		t.Fatalf("expected same project id, got %s", createdTask.ProjectID)
	}
	if createdTask.Status != domain.TaskPending {
		t.Fatalf("expected pending status, got %s", createdTask.Status)
	}
	secondTask, err := taskRepo.GetByID(ctx, created[1].TaskID)
	if err != nil {
		t.Fatalf("get second created task: %v", err)
	}
	if createdTask.Priority != 101 {
		t.Fatalf("expected first auto-appended priority=101, got %d", createdTask.Priority)
	}
	if secondTask.Priority != 102 {
		t.Fatalf("expected second auto-appended priority=102, got %d", secondTask.Priority)
	}
}

func TestListPendingAndHistoryTasks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, taskRepo, _, _, svc, _, sourceTask := setupFixture(t, ctx)

	now := time.Now().UTC()
	pending := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   sourceTask.ProjectID,
		Title:       "待办A",
		Description: "pending",
		Priority:    120,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, pending); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	done := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   sourceTask.ProjectID,
		Title:       "历史A",
		Description: "done",
		Priority:    100,
		Status:      domain.TaskDone,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, done); err != nil {
		t.Fatalf("create history task: %v", err)
	}
	nextRetryAt := now.Add(5 * time.Minute)
	failed := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   sourceTask.ProjectID,
		Title:       "失败任务A",
		Description: "failed",
		Priority:    110,
		Status:      domain.TaskFailed,
		Provider:    "claude",
		RetryCount:  2,
		MaxRetries:  5,
		NextRetryAt: &nextRetryAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, failed); err != nil {
		t.Fatalf("create failed task: %v", err)
	}

	pendingItems, err := svc.ListPendingTasks(ctx, 20, mcpreport.ProjectSelectorInput{})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pendingItems) == 0 {
		t.Fatalf("expected pending tasks")
	}

	historyItems, err := svc.ListHistoryTasks(ctx, 20, mcpreport.ProjectSelectorInput{})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(historyItems) == 0 {
		t.Fatalf("expected history tasks")
	}
	var failedItem *mcpreport.TaskSummaryItem
	for i := range historyItems {
		if historyItems[i].TaskID == failed.ID {
			failedItem = &historyItems[i]
			break
		}
	}
	if failedItem == nil {
		t.Fatalf("expected failed task in history items")
	}
	if failedItem.RetryCount != 2 {
		t.Fatalf("expected retry_count=2, got %d", failedItem.RetryCount)
	}
	if failedItem.NextRetryAt == nil {
		t.Fatalf("expected next_retry_at for failed task")
	}
	if !failedItem.NextRetryAt.Equal(nextRetryAt) {
		t.Fatalf("unexpected next_retry_at: got=%s want=%s", failedItem.NextRetryAt.Format(time.RFC3339Nano), nextRetryAt.Format(time.RFC3339Nano))
	}
}

func TestGetTaskDetail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, _, _, _, svc, _, sourceTask := setupFixture(t, ctx)

	detail, err := svc.GetTaskDetail(ctx, sourceTask.ID, mcpreport.ProjectSelectorInput{})
	if err != nil {
		t.Fatalf("get task detail: %v", err)
	}
	if detail.Task.TaskID != sourceTask.ID {
		t.Fatalf("unexpected task id: %s", detail.Task.TaskID)
	}
}

func TestCreateTasks_ProjectSelectorPriorityWithoutRunContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	projectRepo, taskRepo, runRepo, eventRepo, _, _, _ := setupFixture(t, ctx)
	now := time.Now().UTC()
	project2 := &domain.Project{
		ID:        uuid.NewString(),
		Name:      "P2",
		Path:      "/tmp/p2",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := projectRepo.Create(ctx, project2); err != nil {
		t.Fatalf("create second project: %v", err)
	}

	svc := mcpreport.NewService(runRepo, projectRepo, taskRepo, eventRepo, nil, "", "")
	created, err := svc.CreateTasks(ctx, mcpreport.CreateTasksInput{
		ProjectID:   project2.ID,
		ProjectName: "P",
		ProjectPath: "/tmp/p",
		Items: []mcpreport.CreateTaskItem{
			{Title: "cross-ctx", Description: "created without run context"},
		},
	})
	if err != nil {
		t.Fatalf("create tasks without run context: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created task, got %d", len(created))
	}
	if created[0].ProjectID != project2.ID {
		t.Fatalf("expected project_id selector to take priority, got %s", created[0].ProjectID)
	}
	if created[0].ProjectName != project2.Name {
		t.Fatalf("expected project_name in output, got %q", created[0].ProjectName)
	}
}

func TestListPendingTasks_ByProjectPathWithoutRunContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	projectRepo, taskRepo, runRepo, eventRepo, _, _, sourceTask := setupFixture(t, ctx)
	now := time.Now().UTC()
	otherProject := &domain.Project{
		ID:        uuid.NewString(),
		Name:      "Other",
		Path:      "/tmp/other",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := projectRepo.Create(ctx, otherProject); err != nil {
		t.Fatalf("create other project: %v", err)
	}
	pendingA := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   sourceTask.ProjectID,
		Title:       "A",
		Description: "pending A",
		Priority:    101,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, pendingA); err != nil {
		t.Fatalf("create pending task A: %v", err)
	}
	pendingB := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   otherProject.ID,
		Title:       "B",
		Description: "pending B",
		Priority:    101,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, pendingB); err != nil {
		t.Fatalf("create pending task B: %v", err)
	}

	svc := mcpreport.NewService(runRepo, projectRepo, taskRepo, eventRepo, nil, "", "")
	items, err := svc.ListPendingTasks(ctx, 20, mcpreport.ProjectSelectorInput{
		ProjectPath: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("list pending by project path: %v", err)
	}
	if len(items) != 1 || items[0].TaskID != pendingA.ID {
		t.Fatalf("expected only pending task in /tmp/p, got %#v", items)
	}
}

func TestReportResult_SuccessOverriddenToNeedsInputWhenHumanConfirmationDetected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, taskRepo, runRepo, eventRepo, svc, run, task := setupFixture(t, ctx)
	if err := eventRepo.Append(ctx, run.ID, "claude.stdout", "⚠️ 危险操作检测到！请确认是否继续"); err != nil {
		t.Fatalf("append event: %v", err)
	}

	_, err := svc.ReportResult(ctx, mcpreport.ResultInput{
		Status:  "success",
		Summary: "ok",
		Details: "all good",
	})
	if err != nil {
		t.Fatalf("report result: %v", err)
	}

	gotRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if gotRun.Status != domain.RunNeedsInput {
		t.Fatalf("expected run needs_input, got %s", gotRun.Status)
	}

	gotTask, err := taskRepo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status != domain.TaskBlocked {
		t.Fatalf("expected task blocked, got %s", gotTask.Status)
	}
}

func setupFixture(t *testing.T, ctx context.Context) (*repository.ProjectRepository, *repository.TaskRepository, *repository.RunRepository, *repository.RunEventRepository, *mcpreport.Service, *domain.Run, *domain.Task) {
	t.Helper()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	projectRepo := repository.NewProjectRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	taskRepo := repository.NewTaskRepository(sqlDB)
	runRepo := repository.NewRunRepository(sqlDB)
	eventRepo := repository.NewRunEventRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	project := &domain.Project{
		ID:        uuid.NewString(),
		Name:      "P",
		Path:      "/tmp/p",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	agent := &domain.Agent{
		ID:          "agent-1",
		Name:        "agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := agentRepo.Upsert(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	task := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   project.ID,
		Title:       "T1",
		Description: "D1",
		Priority:    100,
		Status:      domain.TaskRunning,
		Provider:    "claude",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	run := &domain.Run{
		ID:             uuid.NewString(),
		TaskID:         task.ID,
		AgentID:        agent.ID,
		Attempt:        1,
		Status:         domain.RunRunning,
		StartedAt:      time.Now().UTC(),
		PromptSnapshot: "x",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	svc := mcpreport.NewService(runRepo, projectRepo, taskRepo, eventRepo, dispatcher, run.ID, task.ID)
	return projectRepo, taskRepo, runRepo, eventRepo, svc, run, task
}
