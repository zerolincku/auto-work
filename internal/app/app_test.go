package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto-work/internal/app"
	"auto-work/internal/config"
	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/systemprompt"
)

func TestApp_CreateListDispatchFinish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbDir := t.TempDir()
	logDir := t.TempDir()
	projectDir := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        filepath.Join(dbDir, "test.db"),
		AppLogPath:          filepath.Join(logDir, "auto-work.log"),
		RunClaudeOnDispatch: false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	created, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   "",
		Title:       "Task 1",
		Description: "Desc 1",
		Priority:    200,
		Provider:    "claude",
	})
	if err == nil {
		t.Fatalf("expected create task without project fail")
	}

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目A",
		Path: filepath.Join(projectDir, "project-a"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.FailurePolicy != "block" {
		t.Fatalf("expected default failure policy block, got %q", project.FailurePolicy)
	}

	created, err = application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task 1",
		Description: "Desc 1",
		Priority:    200,
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task with project: %v", err)
	}

	items, err := application.ListTasks(ctx, "pending", "", project.ID, 10)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if application.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("expected auto run disabled in this test config")
	}

	dispatch, err := application.DispatchOnce(ctx, "", project.ID)
	if err != nil {
		t.Fatalf("dispatch once: %v", err)
	}
	if dispatch.Claimed {
		t.Fatalf("expected dispatch blocked when claude execution disabled")
	}
	if !strings.Contains(dispatch.Message, "未启用 Claude 执行") {
		t.Fatalf("expected disabled-execution message, got %q", dispatch.Message)
	}

	pendingItems, err := application.ListTasks(ctx, "pending", "", project.ID, 10)
	if err != nil {
		t.Fatalf("list pending tasks: %v", err)
	}
	if len(pendingItems) != 1 {
		t.Fatalf("expected task remains pending, got %d", len(pendingItems))
	}
	if pendingItems[0].ID != created.ID {
		t.Fatalf("pending task mismatch")
	}

	if !application.SetAutoRunEnabled(ctx, project.ID, true) {
		t.Fatalf("set auto run true failed")
	}
	if !application.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("expected auto run true after set")
	}
	if application.SetAutoRunEnabled(ctx, project.ID, false) {
		t.Fatalf("set auto run false should return false")
	}
	if application.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("expected auto run false after set")
	}
}

func TestApp_CreateTask_AppendPriorityWhenPriorityEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目B",
		Path: filepath.Join(t.TempDir(), "project-b"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	_, err = application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Seed",
		Description: "Seed",
		Priority:    120,
	})
	if err != nil {
		t.Fatalf("create seed task: %v", err)
	}

	created, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Auto",
		Description: "Auto priority",
		Priority:    0,
	})
	if err != nil {
		t.Fatalf("create auto-priority task: %v", err)
	}
	if created.Priority != 121 {
		t.Fatalf("expected priority 121, got %d", created.Priority)
	}
}

func TestApp_UpdateTask_NonRunningEditable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目任务更新测试",
		Path: filepath.Join(t.TempDir(), "project-update-task"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	created, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Origin",
		Description: "Desc Origin",
		Priority:    120,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	updated, err := application.UpdateTask(ctx, app.UpdateTaskRequest{
		TaskID:      created.ID,
		Title:       "Task Updated",
		Description: "Desc Updated",
		Priority:    90,
	})
	if err != nil {
		t.Fatalf("update pending task: %v", err)
	}
	if updated.Title != "Task Updated" {
		t.Fatalf("unexpected title: %s", updated.Title)
	}
	if updated.Description != "Desc Updated" {
		t.Fatalf("unexpected description: %s", updated.Description)
	}
	if updated.Priority != 90 {
		t.Fatalf("unexpected priority: %d", updated.Priority)
	}

	if err := application.UpdateTaskStatus(ctx, created.ID, "done"); err != nil {
		t.Fatalf("set task done: %v", err)
	}
	updatedDone, err := application.UpdateTask(ctx, app.UpdateTaskRequest{
		TaskID:      created.ID,
		Title:       "Task Done Updated",
		Description: "Desc Done Updated",
		Priority:    70,
	})
	if err != nil {
		t.Fatalf("update done task: %v", err)
	}
	if updatedDone.Title != "Task Done Updated" {
		t.Fatalf("unexpected done title: %s", updatedDone.Title)
	}

	if err := application.UpdateTaskStatus(ctx, created.ID, "running"); err != nil {
		t.Fatalf("set task running: %v", err)
	}
	if _, err := application.UpdateTask(ctx, app.UpdateTaskRequest{
		TaskID:      created.ID,
		Title:       "Task Updated Again",
		Description: "Desc Updated Again",
		Priority:    60,
	}); err == nil {
		t.Fatalf("expected update running task to fail")
	} else if !strings.Contains(err.Error(), "task is not editable while running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApp_DeleteTask_OnlyNonRunningDeletable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目任务删除测试",
		Path: filepath.Join(t.TempDir(), "project-delete-task"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	deletableTask, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Deletable",
		Description: "Desc Deletable",
		Priority:    120,
	})
	if err != nil {
		t.Fatalf("create deletable task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, deletableTask.ID, "failed"); err != nil {
		t.Fatalf("set task failed: %v", err)
	}
	if err := application.DeleteTask(ctx, deletableTask.ID); err != nil {
		t.Fatalf("delete failed task: %v", err)
	}
	taskList, err := application.ListTasks(ctx, "", "", project.ID, 20)
	if err != nil {
		t.Fatalf("list tasks after delete: %v", err)
	}
	for _, item := range taskList {
		if item.ID == deletableTask.ID {
			t.Fatalf("expected deleted task removed from list")
		}
	}

	runningTask, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Running",
		Description: "Desc Running",
		Priority:    121,
	})
	if err != nil {
		t.Fatalf("create running task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, runningTask.ID, "running"); err != nil {
		t.Fatalf("set task running: %v", err)
	}
	if err := application.DeleteTask(ctx, runningTask.ID); err == nil {
		t.Fatalf("expected delete running task to fail")
	} else if !strings.Contains(err.Error(), "task is not deletable while running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApp_GlobalSettingsCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	current, err := application.GetGlobalSettings(ctx)
	if err != nil {
		t.Fatalf("get global settings: %v", err)
	}
	if current.TelegramEnabled {
		t.Fatalf("expected default telegram disabled")
	}
	if current.TelegramPollTimeout != 30 {
		t.Fatalf("expected default poll timeout=30, got %d", current.TelegramPollTimeout)
	}
	if current.SystemPrompt != systemprompt.DefaultGlobalSystemPromptTemplate {
		t.Fatalf("unexpected default system prompt: %q", current.SystemPrompt)
	}
	defaultPrompt := current.SystemPrompt

	if _, err := application.UpdateGlobalSettings(ctx, app.UpdateGlobalSettingsRequest{
		TelegramEnabled:  true,
		TelegramBotToken: "",
	}); err == nil {
		t.Fatalf("expected enable telegram without token error")
	}

	updated, err := application.UpdateGlobalSettings(ctx, app.UpdateGlobalSettingsRequest{
		TelegramEnabled:     false,
		TelegramBotToken:    "token-1",
		TelegramChatIDs:     "1001,1002, 1001",
		TelegramPollTimeout: 50,
		TelegramProxyURL:    "http://127.0.0.1:7890",
		SystemPrompt:        "你是一个严谨的软件工程师。\n请先阅读代码再修改。",
	})
	if err != nil {
		t.Fatalf("update global settings: %v", err)
	}
	if updated.TelegramChatIDs != "1001,1002" {
		t.Fatalf("unexpected chat ids: %s", updated.TelegramChatIDs)
	}
	if updated.TelegramPollTimeout != 50 {
		t.Fatalf("unexpected poll timeout: %d", updated.TelegramPollTimeout)
	}
	if updated.TelegramProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy url: %s", updated.TelegramProxyURL)
	}
	if updated.SystemPrompt != defaultPrompt {
		t.Fatalf("unexpected system prompt: %q", updated.SystemPrompt)
	}

	persisted, err := application.GetGlobalSettings(ctx)
	if err != nil {
		t.Fatalf("get global settings after update: %v", err)
	}
	if persisted.SystemPrompt != defaultPrompt {
		t.Fatalf("unexpected persisted system prompt: %q", persisted.SystemPrompt)
	}
}

func TestApp_ProjectAIConfigCRUD_WithSystemPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name:                            "项目C",
		Path:                            filepath.Join(t.TempDir(), "project-c"),
		DefaultProvider:                 "codex",
		Model:                           "gpt-5.3-codex",
		SystemPrompt:                    "  你是项目专属助手，请先阅读 README。  ",
		FailurePolicy:                   "continue",
		FrontendScreenshotReportEnabled: true,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.SystemPrompt != "你是项目专属助手，请先阅读 README。" {
		t.Fatalf("unexpected create system prompt: %q", project.SystemPrompt)
	}
	if project.FailurePolicy != "continue" {
		t.Fatalf("unexpected create failure policy: %q", project.FailurePolicy)
	}
	if !project.FrontendScreenshotReportEnabled {
		t.Fatalf("expected create frontend screenshot report enabled")
	}

	updated, err := application.UpdateProjectAIConfig(ctx, app.UpdateProjectAIConfigRequest{
		ProjectID:       project.ID,
		DefaultProvider: "claude",
		Model:           "claude-sonnet-4-6",
		SystemPrompt:    "  更新后的项目提示词  ",
		FailurePolicy:   "block",
	})
	if err != nil {
		t.Fatalf("update project ai config: %v", err)
	}
	if updated.DefaultProvider != "claude" {
		t.Fatalf("unexpected default provider: %q", updated.DefaultProvider)
	}
	if updated.Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected model: %q", updated.Model)
	}
	if updated.SystemPrompt != "更新后的项目提示词" {
		t.Fatalf("unexpected updated system prompt: %q", updated.SystemPrompt)
	}
	if updated.FailurePolicy != "block" {
		t.Fatalf("unexpected updated failure policy: %q", updated.FailurePolicy)
	}
	if !updated.FrontendScreenshotReportEnabled {
		t.Fatalf("expected frontend screenshot report setting unchanged after AI config update")
	}

	projects, err := application.ListProjects(ctx, 20)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	found := false
	for _, item := range projects {
		if item.ID != project.ID {
			continue
		}
		found = true
		if item.SystemPrompt != "更新后的项目提示词" {
			t.Fatalf("unexpected persisted project system prompt: %q", item.SystemPrompt)
		}
		if item.FailurePolicy != "block" {
			t.Fatalf("unexpected persisted failure policy: %q", item.FailurePolicy)
		}
		if !item.FrontendScreenshotReportEnabled {
			t.Fatalf("unexpected persisted frontend screenshot report setting: %v", item.FrontendScreenshotReportEnabled)
		}
	}
	if !found {
		t.Fatalf("project not found in list")
	}
}

func TestApp_UpdateProjectAndDeleteWithRelatedData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目删除测试",
		Path: filepath.Join(t.TempDir(), "project-delete"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Delete",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,finished_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"run-delete", task.ID, "agent-claude-default", 1, "done", now.Add(-time.Minute), now.Add(-30*time.Second), "prompt", now.Add(-time.Minute), now.Add(-30*time.Second)); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"event-delete", "run-delete", now, "system.done", "done"); err != nil {
		t.Fatalf("insert run event: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO artifacts(id,run_id,kind,value,created_at)
VALUES(?,?,?,?,?)`,
		"artifact-delete", "run-delete", "log", "artifact", now); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}

	updated, err := application.UpdateProject(ctx, app.UpdateProjectRequest{
		ProjectID:                       project.ID,
		Name:                            "项目删除测试-更新",
		DefaultProvider:                 "codex",
		Model:                           "gpt-5.3-codex",
		SystemPrompt:                    "  更新后的项目级提示词  ",
		FailurePolicy:                   "continue",
		FrontendScreenshotReportEnabled: true,
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.Name != "项目删除测试-更新" {
		t.Fatalf("unexpected updated project name: %s", updated.Name)
	}
	if updated.DefaultProvider != "codex" {
		t.Fatalf("unexpected default provider: %s", updated.DefaultProvider)
	}
	if updated.Model != "gpt-5.3-codex" {
		t.Fatalf("unexpected model: %s", updated.Model)
	}
	if updated.SystemPrompt != "更新后的项目级提示词" {
		t.Fatalf("unexpected system prompt: %s", updated.SystemPrompt)
	}
	if updated.FailurePolicy != "continue" {
		t.Fatalf("unexpected failure policy: %s", updated.FailurePolicy)
	}
	if !updated.FrontendScreenshotReportEnabled {
		t.Fatalf("unexpected frontend screenshot report setting: %v", updated.FrontendScreenshotReportEnabled)
	}

	if err := application.DeleteProject(ctx, project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	projects, err := application.ListProjects(ctx, 20)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	for _, item := range projects {
		if item.ID == project.ID {
			t.Fatalf("expected deleted project not in list")
		}
	}

	tasks, err := application.ListTasks(ctx, "", "", project.ID, 20)
	if err != nil {
		t.Fatalf("list tasks after delete: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks for deleted project, got %d", len(tasks))
	}

	var (
		runCount      int
		runEventCount int
		artifactCount int
		taskCount     int
	)
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM runs WHERE id = ?`, "run-delete").Scan(&runCount); err != nil {
		t.Fatalf("query run count: %v", err)
	}
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM run_events WHERE id = ?`, "event-delete").Scan(&runEventCount); err != nil {
		t.Fatalf("query run event count: %v", err)
	}
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM artifacts WHERE id = ?`, "artifact-delete").Scan(&artifactCount); err != nil {
		t.Fatalf("query artifact count: %v", err)
	}
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks WHERE id = ?`, task.ID).Scan(&taskCount); err != nil {
		t.Fatalf("query task count: %v", err)
	}
	if runCount != 0 || runEventCount != 0 || artifactCount != 0 || taskCount != 0 {
		t.Fatalf("expected related data deleted, got run=%d event=%d artifact=%d task=%d", runCount, runEventCount, artifactCount, taskCount)
	}
}

func TestApp_StartupRecoversOrphanRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	sqlDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO projects(id,name,path,created_at,updated_at)
VALUES('p1','P1','/tmp/p1',?,?)`, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO agents(id,name,provider,enabled,concurrency,created_at,updated_at)
VALUES('a1','A1','claude',1,1,?,?)`, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO tasks(id,project_id,title,description,priority,status,provider,created_at,updated_at)
VALUES('t1','p1','T1','D1',100,'running','claude',?,?)`, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES('r1','t1','a1',1,'running',?,?,?,?)`, time.Now().UTC(), "x", time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	_ = sqlDB.Close()

	application, err := app.New(ctx, config.Config{
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	// verify recovered
	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	var runStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM runs WHERE id='r1'`).Scan(&runStatus); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if runStatus != "failed" {
		t.Fatalf("expected recovered run failed, got %s", runStatus)
	}
	var taskStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id='t1'`).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "failed" {
		t.Fatalf("expected recovered task failed, got %s", taskStatus)
	}
}

func TestApp_RetryTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := app.New(ctx, config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目B",
		Path: filepath.Join(t.TempDir(), "project-b"),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Retry",
		Description: "Desc Retry",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, task.ID, "failed"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if err := application.RetryTask(ctx, task.ID); err != nil {
		t.Fatalf("retry task: %v", err)
	}
	pending, err := application.ListTasks(ctx, "pending", "", project.ID, 20)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != task.ID {
		t.Fatalf("expected retried task in pending list")
	}
}

func TestApp_GetTaskLatestRun_WithNullOptionalFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	projectPath := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目C",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Latest Run",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"r-null", task.ID, "agent-claude-default", 1, "failed", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	latest, err := application.GetTaskLatestRun(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskLatestRun: %v", err)
	}
	if latest == nil {
		t.Fatalf("expected latest run")
	}
	if latest.RunID != "r-null" {
		t.Fatalf("unexpected run id: %s", latest.RunID)
	}
}

func TestApp_GetTaskDetail_WithRunHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	projectPath := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目详情测试",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Detail",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,finished_at,prompt_snapshot,result_summary,result_details,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"run-1", task.ID, "agent-claude-default", 1, "failed", now.Add(-2*time.Minute), now.Add(-90*time.Second), "prompt1", "failed 1", "detail 1", now.Add(-2*time.Minute), now.Add(-90*time.Second)); err != nil {
		t.Fatalf("insert run1: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-2", task.ID, "agent-claude-default", 2, "running", now.Add(-30*time.Second), "prompt2", now.Add(-30*time.Second), now.Add(-30*time.Second)); err != nil {
		t.Fatalf("insert run2: %v", err)
	}

	detail, err := application.GetTaskDetail(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskDetail: %v", err)
	}
	if detail == nil || detail.Task == nil {
		t.Fatalf("expected detail with task")
	}
	if detail.Task.ID != task.ID {
		t.Fatalf("unexpected task id: %s", detail.Task.ID)
	}
	if len(detail.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(detail.Runs))
	}
	if detail.Runs[0].RunID != "run-2" || detail.Runs[1].RunID != "run-1" {
		t.Fatalf("unexpected run order: %#v", detail.Runs)
	}
}

func TestApp_ListSystemLogs_FilterByProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	projectA, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目A",
		Path: filepath.Join(t.TempDir(), "project-a"),
	})
	if err != nil {
		t.Fatalf("create projectA: %v", err)
	}
	projectB, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目B",
		Path: filepath.Join(t.TempDir(), "project-b"),
	})
	if err != nil {
		t.Fatalf("create projectB: %v", err)
	}

	taskA, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   projectA.ID,
		Title:       "Task A",
		Description: "Desc A",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create taskA: %v", err)
	}
	taskB, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   projectB.ID,
		Title:       "Task B",
		Description: "Desc B",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create taskB: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-a", taskA.ID, "agent-claude-default", 1, "done", now.Add(-3*time.Minute), "prompt-a", now.Add(-3*time.Minute), now.Add(-3*time.Minute)); err != nil {
		t.Fatalf("insert run-a: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-b", taskB.ID, "agent-claude-default", 1, "done", now.Add(-2*time.Minute), "prompt-b", now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("insert run-b: %v", err)
	}

	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-a1", "run-a", now.Add(-2*time.Minute), "claude.stdout", "started-a"); err != nil {
		t.Fatalf("insert event evt-a1: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-b1", "run-b", now.Add(-1*time.Minute), "codex.stderr", "started-b"); err != nil {
		t.Fatalf("insert event evt-b1: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-a2", "run-a", now, "system.done", "done-a"); err != nil {
		t.Fatalf("insert event evt-a2: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-a3", "run-a", now.Add(-30*time.Second), "codex.stdout", "done-a-output"); err != nil {
		t.Fatalf("insert event evt-a3: %v", err)
	}

	projectALogs, err := application.ListSystemLogs(ctx, projectA.ID, 20)
	if err != nil {
		t.Fatalf("ListSystemLogs(projectA): %v", err)
	}
	if len(projectALogs) != 2 {
		t.Fatalf("expected 2 logs for projectA, got %d", len(projectALogs))
	}
	if projectALogs[0].ID != "evt-a3" || projectALogs[1].ID != "evt-a1" {
		t.Fatalf("unexpected projectA log order: %#v", projectALogs)
	}
	for _, item := range projectALogs {
		if item.ProjectID != projectA.ID {
			t.Fatalf("unexpected project id in filtered logs: %s", item.ProjectID)
		}
		if !strings.HasSuffix(item.Kind, ".stdout") && !strings.HasSuffix(item.Kind, ".stderr") {
			t.Fatalf("unexpected log kind in filtered result: %s", item.Kind)
		}
	}

	allLogs, err := application.ListSystemLogs(ctx, "", 20)
	if err != nil {
		t.Fatalf("ListSystemLogs(all): %v", err)
	}
	if len(allLogs) != 3 {
		t.Fatalf("expected 3 logs in all list, got %d", len(allLogs))
	}
	if allLogs[0].ID != "evt-a3" || allLogs[1].ID != "evt-b1" || allLogs[2].ID != "evt-a1" {
		t.Fatalf("unexpected all log order: %#v", allLogs)
	}
	for _, item := range allLogs {
		if !strings.HasSuffix(item.Kind, ".stdout") && !strings.HasSuffix(item.Kind, ".stderr") {
			t.Fatalf("unexpected log kind in all result: %s", item.Kind)
		}
	}
}

func TestApp_DispatchOnce_ClaudeStartFailureReturnsRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectPath := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		ClaudeBinary:        filepath.Join(projectPath, "missing-claude"),
		RunClaudeOnDispatch: true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目D",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Start Failure",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	resp, err := application.DispatchOnce(ctx, "", project.ID)
	if err != nil {
		t.Fatalf("dispatch once should not return error on start failure: %v", err)
	}
	if !resp.Claimed || resp.RunID == "" || resp.TaskID != task.ID {
		t.Fatalf("unexpected dispatch response: %+v", resp)
	}
	if !strings.Contains(resp.Message, "失败") {
		t.Fatalf("unexpected response message: %s", resp.Message)
	}

	items, err := application.ListTasks(ctx, "failed", "claude", project.ID, 20)
	if err != nil {
		t.Fatalf("list failed tasks: %v", err)
	}
	if len(items) != 1 || items[0].ID != task.ID {
		t.Fatalf("expected failed task after start failure")
	}
	if items[0].RetryCount != 1 {
		t.Fatalf("expected retry_count=1 after first failure, got %d", items[0].RetryCount)
	}
	if items[0].NextRetryAt == nil {
		t.Fatalf("expected next_retry_at after first failure")
	}
}

func TestApp_DispatchOnce_UsesProjectProviderInsteadOfTaskProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: true,
		RunCodexOnDispatch:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name:            "项目Provider选择",
		Path:            filepath.Join(t.TempDir(), "project-provider"),
		DefaultProvider: "codex",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Task Provider",
		Description: "Desc",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	if _, err := rawDB.ExecContext(ctx, `UPDATE tasks SET provider = 'claude' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("force task provider: %v", err)
	}

	resp, err := application.DispatchOnce(ctx, "", project.ID)
	if err != nil {
		t.Fatalf("dispatch once: %v", err)
	}
	if resp.Claimed {
		t.Fatalf("expected dispatch blocked because codex execution disabled, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "未启用 Codex 执行") {
		t.Fatalf("expected codex-disabled message, got %q", resp.Message)
	}
}

func TestApp_DispatchOnce_ManualWorksWhenAutoRunDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectPath := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		ClaudeBinary:        filepath.Join(projectPath, "missing-claude"),
		RunClaudeOnDispatch: true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目E",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Manual Dispatch Task",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if application.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("expected new project auto dispatch default disabled")
	}

	resp, err := application.DispatchOnce(ctx, "", project.ID)
	if err != nil {
		t.Fatalf("dispatch once should not return error: %v", err)
	}
	if !resp.Claimed || resp.RunID == "" || resp.TaskID != task.ID {
		t.Fatalf("expected manual dispatch claimed even when auto-run disabled, got %+v", resp)
	}
}

func TestApp_DispatchTask_ClaimsSpecifiedTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectPath := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		ClaudeBinary:        filepath.Join(projectPath, "missing-claude"),
		RunClaudeOnDispatch: true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = application.Close()
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目F",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	first, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "First Task",
		Description: "Desc",
		Priority:    100,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Second Task",
		Description: "Desc",
		Priority:    200,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}

	resp, err := application.DispatchTask(ctx, second.ID)
	if err != nil {
		t.Fatalf("dispatch specific task: %v", err)
	}
	if !resp.Claimed || resp.TaskID != second.ID || resp.RunID == "" {
		t.Fatalf("expected second task claimed, got %+v", resp)
	}

	firstTask, err := application.GetTaskDetail(ctx, first.ID)
	if err != nil {
		t.Fatalf("get first task detail: %v", err)
	}
	if firstTask.Task.Status != "pending" {
		t.Fatalf("expected first task still pending, got %s", firstTask.Task.Status)
	}
}

func TestApp_DispatchTask_UsesProjectProviderInsteadOfTaskProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: true,
		RunCodexOnDispatch:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name:            "项目G",
		Path:            filepath.Join(t.TempDir(), "project-provider-task"),
		DefaultProvider: "codex",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Specific Provider Task",
		Description: "Desc",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite raw: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	if _, err := rawDB.ExecContext(ctx, `UPDATE tasks SET provider = 'claude' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("force task provider: %v", err)
	}

	resp, err := application.DispatchTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("dispatch specific task: %v", err)
	}
	if resp.Claimed {
		t.Fatalf("expected specific dispatch blocked because codex execution disabled, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "未启用 Codex 执行") {
		t.Fatalf("expected codex-disabled message, got %q", resp.Message)
	}
}

func TestApp_AutoDispatchLoop_DispatchesPendingTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	projectPath := t.TempDir()

	application, err := app.New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		RunClaudeOnDispatch: true,
		ClaudeBinary:        "/bin/echo",
		WorkspacePath:       projectPath,
		RequireMCPCallback:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "自动派发项目",
		Path: projectPath,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if !application.SetAutoRunEnabled(ctx, project.ID, true) {
		t.Fatalf("enable project auto dispatch failed")
	}

	task, err := application.CreateTask(ctx, app.CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "Auto Dispatch Task",
		Description: "Desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		items, listErr := application.ListTasks(ctx, "", "", project.ID, 20)
		if listErr != nil {
			t.Fatalf("list tasks: %v", listErr)
		}
		if len(items) == 1 && items[0].ID == task.ID && items[0].Status != "pending" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("task still pending after auto-dispatch timeout")
}

func TestApp_ProjectAutoDispatchSetting_PersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	project, err := application.CreateProject(ctx, app.CreateProjectRequest{
		Name: "项目持久化",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if application.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("new project should default auto dispatch disabled")
	}
	if !application.SetAutoRunEnabled(ctx, project.ID, true) {
		t.Fatalf("enable project auto dispatch failed")
	}
	if err := application.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}

	reopened, err := app.New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: true,
	})
	if err != nil {
		t.Fatalf("reopen app: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if !reopened.AutoRunEnabled(ctx, project.ID) {
		t.Fatalf("expected auto dispatch enabled persisted after restart")
	}
}
