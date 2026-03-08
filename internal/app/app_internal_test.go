package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto-work/internal/config"
	"auto-work/internal/db"
)

func TestFindMCPFailureInDebugFile_ConnectionFailed(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "debug.log")
	content := `2026-03-04T06:03:52.713Z [ERROR] MCP server "auto-work" Connection failed: MCP server "auto-work" connection timed out after 60000ms`
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	reason := findMCPFailureInDebugFile(path)
	if reason != `MCP server "auto-work" connection timed out after 60000ms` {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestFindMCPFailureInDebugFile_TimeoutLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "debug.log")
	content := `2026-03-04T06:03:52.681Z [DEBUG] MCP server "auto-work": Connection timeout triggered after 60017ms (limit: 60000ms)`
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	reason := findMCPFailureInDebugFile(path)
	if reason != "MCP server connection timeout" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestExtractMCPPermissionDeniedReason_ReportResult(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","permission_denials":[{"tool_name":"mcp__auto-work__auto_work_report_result","tool_use_id":"call_123"}]}`
	reason := extractMCPPermissionDeniedReason(line)
	if reason != "MCP tool auto-work.report_result permission denied by Claude Code" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestExtractMCPRuntimeFailureReason_UnknownTodoServer(t *testing.T) {
	t.Parallel()
	line := `{"type":"item.completed","item":{"id":"item_33","type":"mcp_tool_call","server":"todo","tool":"read_mcp_resource","arguments":{"server":"todo","uri":"todo://report_result"},"result":null,"error":{"message":"resources/read failed: unknown MCP server 'todo'"},"status":"failed"}}`
	reason := extractMCPRuntimeFailureReason(line)
	if reason != "unknown MCP server 'todo'（请使用 auto-work）" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestExtractMCPRuntimeFailureReason_InitFailedWithSpaces(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","subtype":"init","mcp_servers":[{"name": "auto-work", "status": "failed"}]}`
	reason := extractMCPRuntimeFailureReason(line)
	if reason != "auto-work MCP server status=failed during initialize" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestParseClaudeResultEvent_Success(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","subtype":"success","is_error":false,"result":"任务完成","permission_denials":[]}`
	evt, ok := parseClaudeResultEvent(line)
	if !ok {
		t.Fatalf("expected parse ok")
	}
	if !evt.isSuccess() {
		t.Fatalf("expected success event")
	}
	if evt.hasMCPPermissionDenied() {
		t.Fatalf("expected no mcp permission denied")
	}
	if evt.Result != "任务完成" {
		t.Fatalf("unexpected result: %s", evt.Result)
	}
}

func TestTryFinalizeRunWithoutMCP_UsesClaudeResultFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
		RequireMCPCallback:  true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "fallback-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "fallback-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("set task running: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-fallback", task.ID, defaultClaudeAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-fallback-result", "run-fallback", now, "claude.stdout",
		`{"type":"result","subtype":"success","is_error":false,"result":"最终结果文本","permission_denials":[]}`); err != nil {
		t.Fatalf("insert result event: %v", err)
	}

	if ok := application.tryFinalizeRunWithoutMCP("run-fallback", 0); !ok {
		t.Fatalf("expected fallback finalize success")
	}

	var runStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM runs WHERE id='run-fallback'`).Scan(&runStatus); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if runStatus != "done" {
		t.Fatalf("expected run done, got %s", runStatus)
	}

	var taskStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, task.ID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "done" {
		t.Fatalf("expected task done, got %s", taskStatus)
	}

	var fallbackCount int
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM run_events WHERE run_id='run-fallback' AND kind='system.mcp_fallback'`).Scan(&fallbackCount); err != nil {
		t.Fatalf("query fallback events: %v", err)
	}
	if fallbackCount != 1 {
		t.Fatalf("expected 1 fallback event, got %d", fallbackCount)
	}
}

func TestOnClaudeExit_WithNeedsInputEvent_MarksBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
		RequireMCPCallback:  true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "needs-input-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "needs-input-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("set task running: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-needs-input", task.ID, defaultClaudeAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-needs-input", "run-needs-input", now, "system.needs_input", "AI 发起 AskUserQuestion，等待人工回复"); err != nil {
		t.Fatalf("insert needs_input event: %v", err)
	}

	application.onClaudeExit("run-needs-input", 0, nil)

	var runStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM runs WHERE id='run-needs-input'`).Scan(&runStatus); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if runStatus != "needs_input" {
		t.Fatalf("expected run needs_input, got %s", runStatus)
	}

	var taskStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, task.ID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "blocked" {
		t.Fatalf("expected task blocked, got %s", taskStatus)
	}
}

func TestRecoverDeadRunningRuns_MarksRunAndTaskFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "恢复测试项目",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "恢复测试任务",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, task.ID, "running"); err != nil {
		t.Fatalf("set task running: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,pid,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"run-dead", task.ID, defaultClaudeAgentID, 1, "running", 999999, now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	if err := application.recoverDeadRunningRuns(ctx, "test recover dead run"); err != nil {
		t.Fatalf("recoverDeadRunningRuns: %v", err)
	}

	var runStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM runs WHERE id='run-dead'`).Scan(&runStatus); err != nil {
		t.Fatalf("query run status: %v", err)
	}
	if runStatus != "failed" {
		t.Fatalf("expected run failed, got %s", runStatus)
	}

	var taskStatus string
	if err := rawDB.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, task.ID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "failed" {
		t.Fatalf("expected task failed, got %s", taskStatus)
	}
}

func TestNormalizeProxyURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: "", wantErr: false},
		{name: "http", input: "http://127.0.0.1:7890", want: "http://127.0.0.1:7890", wantErr: false},
		{name: "socks5", input: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080", wantErr: false},
		{name: "bad scheme", input: "ftp://127.0.0.1:21", wantErr: true},
		{name: "missing host", input: "http://", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeProxyURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected err")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected value: %q", got)
			}
		})
	}
}

func TestReleaseDueRetryTasks_PromotesToPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "重试回捞项目",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "重试任务",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := application.UpdateTaskStatus(ctx, task.ID, "failed"); err != nil {
		t.Fatalf("set task failed: %v", err)
	}

	rawDB, err := db.OpenSQLite(application.cfg.DatabasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()
	if _, err := rawDB.ExecContext(ctx, `
UPDATE tasks
SET retry_count = 1,
    max_retries = 5,
    next_retry_at = ?
WHERE id = ?`, time.Now().UTC().Add(-time.Second), task.ID); err != nil {
		t.Fatalf("prepare due retry: %v", err)
	}

	released := application.releaseDueRetryTasks(ctx, 10)
	if released != 1 {
		t.Fatalf("expected released=1, got %d", released)
	}

	var (
		status      string
		nextRetryAt any
	)
	if err := rawDB.QueryRowContext(ctx, `SELECT status, next_retry_at FROM tasks WHERE id = ?`, task.ID).Scan(&status, &nextRetryAt); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "pending" {
		t.Fatalf("expected pending after release, got %s", status)
	}
	if nextRetryAt != nil {
		t.Fatalf("expected next_retry_at cleared after release")
	}
}

func TestMCPStatus_Disabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "disabled" {
		t.Fatalf("expected disabled, got %s", status.State)
	}
}

func TestMCPStatus_ConnectedByReportedEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-connected-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-connected-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,finished_at,prompt_snapshot,result_summary,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"run-mcp-connected", task.ID, defaultClaudeAgentID, 1, "done", now, now, "prompt", "ok", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-connected", "run-mcp-connected", now, "mcp.report_result.applied", `{"run_status":"done","task_status":"done"}`); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
	if status.RunID != "run-mcp-connected" {
		t.Fatalf("expected run id run-mcp-connected, got %s", status.RunID)
	}
}

func TestMCPStatus_FailedByMissingCallbackSummary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-failed-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-failed-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,finished_at,prompt_snapshot,result_summary,result_details,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"run-mcp-failed", task.ID, defaultClaudeAgentID, 1, "failed", now, now, "prompt",
		"claude exited without mcp report_result callback", "process exit 0 but no MCP report was received", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if status.RunID != "run-mcp-failed" {
		t.Fatalf("expected run id run-mcp-failed, got %s", status.RunID)
	}
}

func TestMCPStatus_ConnectedRunningInitStatusOK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-running-connected-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-running-connected-task",
		Description: "desc",
		Provider:    "codex",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-mcp-running-connected", task.ID, defaultCodexAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	initLine := `{"type":"result","subtype":"init","mcp_servers":[{"name":"auto-work","status":"ok"}]}`
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-running-connected", "run-mcp-running-connected", now, "codex.stdout", initLine); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
}

func TestMCPStatus_FailedRunningInitStatusWithSpaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-running-failed-init-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-running-failed-init-task",
		Description: "desc",
		Provider:    "codex",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-mcp-running-failed-init", task.ID, defaultCodexAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	initFailedLine := `{"type":"result","subtype":"init","mcp_servers":[{"name": "auto-work", "status": "failed"}]}`
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-running-failed-init", "run-mcp-running-failed-init", now, "codex.stdout", initFailedLine); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "status=failed") {
		t.Fatalf("expected init failed reason in message, got %q", status.Message)
	}
}

func TestMCPStatus_FailedRunningInitShowsDetailedReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-running-failed-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-running-failed-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-mcp-running-failed", task.ID, defaultClaudeAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	failedLine := `2026-03-04T06:03:52.713Z [ERROR] MCP server "auto-work" Connection failed: permission denied`
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-running-failed", "run-mcp-running-failed", now, "claude.stdout", failedLine); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "permission denied") {
		t.Fatalf("expected detailed reason in message, got %q", status.Message)
	}
}

func TestMCPStatus_FailedRunningUnknownServerShowsReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-unknown-server-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-unknown-server-task",
		Description: "desc",
		Provider:    "codex",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,prompt_snapshot,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		"run-mcp-unknown-server", task.ID, defaultCodexAgentID, 1, "running", now, "prompt", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	unknownLine := `{"type":"item.completed","item":{"id":"item_33","type":"mcp_tool_call","server":"todo","tool":"read_mcp_resource","arguments":{"server":"todo","uri":"todo://report_result"},"result":null,"error":{"message":"resources/read failed: unknown MCP server 'todo'"},"status":"failed"}}`
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-unknown-server", "run-mcp-unknown-server", now, "codex.stdout", unknownLine); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "unknown MCP server 'todo'") {
		t.Fatalf("expected unknown server reason in message, got %q", status.Message)
	}
}

func TestMCPStatus_FailedMissingCallbackIncludesReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	application, err := New(ctx, config.Config{
		DatabasePath:        dbPath,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name: "mcp-callback-failed-project",
		Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	task, err := application.CreateTask(ctx, CreateTaskRequest{
		ProjectID:   project.ID,
		Title:       "mcp-callback-failed-task",
		Description: "desc",
		Provider:    "claude",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	rawDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer rawDB.Close()

	now := time.Now().UTC()
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO runs(id,task_id,agent_id,attempt,status,started_at,finished_at,prompt_snapshot,result_summary,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"run-mcp-callback-failed", task.ID, defaultClaudeAgentID, 1, "failed", now, now, "prompt",
		"claude exited without mcp report_result callback", now, now); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	deniedLine := fmt.Sprintf(`{"type":"result","permission_denials":[{"tool_name":"mcp__auto-work__auto_work_report_result","tool_use_id":"call_123"}]}`)
	if _, err := rawDB.ExecContext(ctx, `
INSERT INTO run_events(id,run_id,ts,kind,payload)
VALUES(?,?,?,?,?)`,
		"evt-mcp-callback-failed", "run-mcp-callback-failed", now, "claude.stdout", deniedLine); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	status, err := application.MCPStatus(ctx)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "permission denied") {
		t.Fatalf("expected reason included in message, got %q", status.Message)
	}
}
