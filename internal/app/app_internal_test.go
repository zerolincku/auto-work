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

	status, err := application.MCPStatus(ctx, "")
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "disabled" {
		t.Fatalf("expected disabled, got %s", status.State)
	}
}

func TestMCPStatus_ProjectRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(t.TempDir(), "test.db"),
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	status, err := application.MCPStatus(ctx, "")
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "unknown" {
		t.Fatalf("expected unknown, got %s", status.State)
	}
	if !strings.Contains(status.Message, "请选择项目") {
		t.Fatalf("expected project required message, got %q", status.Message)
	}
}

func TestMCPStatus_ClaudeConnectedByConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeClaudeBinary(t, dir, fakeClaudeOptions{
		HasAutoWork: true,
	})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "claude-mcp-project",
		Path:            dir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
	if !strings.Contains(status.Message, "Claude MCP 已配置") {
		t.Fatalf("expected configured message, got %q", status.Message)
	}
}

func TestMCPStatus_ClaudeMissingAutoAdds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeClaudeBinary(t, dir, fakeClaudeOptions{})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "claude-auto-add-project",
		Path:            dir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
	if !strings.Contains(status.Message, "已自动添加") {
		t.Fatalf("expected auto-add message, got %q", status.Message)
	}
}

func TestMCPStatus_ClaudeAutoAddFailureIncludesReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeClaudeBinary(t, dir, fakeClaudeOptions{
		AddFails:  true,
		AddReason: "permission denied",
	})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "claude-auto-add-failed-project",
		Path:            dir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "permission denied") {
		t.Fatalf("expected add failure reason, got %q", status.Message)
	}
}

func TestMCPStatus_CodexConnectedByConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	codexBinary := writeFakeCodexBinary(t, dir, fakeCodexOptions{
		HasAutoWork: true,
		WarnOnList:  true,
	})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		CodexBinary:         codexBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "codex-configured-project",
		Path:            dir,
		DefaultProvider: "codex",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
	if !strings.Contains(status.Message, "Codex MCP 已配置") {
		t.Fatalf("expected configured message, got %q", status.Message)
	}
}

func TestMCPStatus_CodexMissingAutoAdds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	codexBinary := writeFakeCodexBinary(t, dir, fakeCodexOptions{})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		CodexBinary:         codexBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "codex-auto-add-project",
		Path:            dir,
		DefaultProvider: "codex",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "connected" {
		t.Fatalf("expected connected, got %s", status.State)
	}
	if !strings.Contains(status.Message, "已自动添加") {
		t.Fatalf("expected auto-add message, got %q", status.Message)
	}
}

func TestMCPStatus_CodexAutoAddFailureIncludesReason(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	codexBinary := writeFakeCodexBinary(t, dir, fakeCodexOptions{
		AddFails:  true,
		AddReason: "permission denied",
	})

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		CodexBinary:         codexBinary,
		RunClaudeOnDispatch: false,
		MCPHTTPURL:          "http://127.0.0.1:0/mcp",
		EnableMCPCallback:   true,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "codex-auto-add-failed-project",
		Path:            dir,
		DefaultProvider: "codex",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	status, err := application.MCPStatus(ctx, project.ID)
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if status.State != "failed" {
		t.Fatalf("expected failed, got %s", status.State)
	}
	if !strings.Contains(status.Message, "permission denied") {
		t.Fatalf("expected add failure reason, got %q", status.Message)
	}
}

type fakeClaudeOptions struct {
	HasAutoWork bool
	AddFails    bool
	AddReason   string
}

type fakeCodexOptions struct {
	HasAutoWork bool
	AddFails    bool
	AddReason   string
	WarnOnList  bool
}

func writeFakeClaudeBinary(t *testing.T, dir string, opts fakeClaudeOptions) string {
	t.Helper()
	stateFile := filepath.Join(dir, "claude-auto-work.state")
	if opts.HasAutoWork {
		if err := os.WriteFile(stateFile, []byte("present\n"), 0o644); err != nil {
			t.Fatalf("write claude state file: %v", err)
		}
	}
	addReason := opts.AddReason
	if addReason == "" {
		addReason = "permission denied"
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
STATE_FILE=%q
ADD_FAILS=%q
ADD_REASON=%q

if [ "$1" != "mcp" ]; then
  echo "unsupported command: $1" >&2
  exit 1
fi
shift

case "$1" in
  get)
    shift
    if [ "$1" != "auto-work" ]; then
      echo "unexpected server: $1" >&2
      exit 1
    fi
    if [ -f "$STATE_FILE" ]; then
      echo "auto-work"
      exit 0
    fi
    echo "No MCP server found with name: auto-work" >&2
    exit 1
    ;;
  add)
    shift
    if [ "$1" = "--scope" ]; then
      shift 2
    fi
    if [ "$1" = "--transport" ]; then
      shift 2
    fi
    if [ "$1" != "auto-work" ]; then
      echo "unexpected server: $1" >&2
      exit 1
    fi
    if [ "$ADD_FAILS" = "1" ]; then
      echo "$ADD_REASON" >&2
      exit 1
    fi
    : > "$STATE_FILE"
    echo "added"
    ;;
  *)
    echo "unsupported mcp subcommand: $1" >&2
    exit 1
    ;;
esac
`, stateFile, boolString(opts.AddFails), addReason)
	return writeExecutableScript(t, dir, "fake-claude.sh", script)
}

func writeFakeCodexBinary(t *testing.T, dir string, opts fakeCodexOptions) string {
	t.Helper()
	stateFile := filepath.Join(dir, "codex-auto-work.state")
	if opts.HasAutoWork {
		if err := os.WriteFile(stateFile, []byte("present\n"), 0o644); err != nil {
			t.Fatalf("write codex state file: %v", err)
		}
	}
	addReason := opts.AddReason
	if addReason == "" {
		addReason = "permission denied"
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
STATE_FILE=%q
ADD_FAILS=%q
ADD_REASON=%q
WARN_ON_LIST=%q

if [ "$1" != "mcp" ]; then
  echo "unsupported command: $1" >&2
  exit 1
fi
shift

case "$1" in
  list)
    if [ "$WARN_ON_LIST" = "1" ]; then
      echo "WARNING: proceeding, even though we could not update PATH: Operation not permitted (os error 1)" >&2
    fi
    if [ -f "$STATE_FILE" ]; then
      printf '%%s\n' '[{"name":"auto-work","enabled":true,"disabled_reason":null}]'
    else
      printf '%%s\n' '[]'
    fi
    ;;
  add)
    shift
    if [ "$1" != "--url" ]; then
      echo "missing --url" >&2
      exit 1
    fi
    shift
    if [ "$1" = "" ]; then
      echo "missing url value" >&2
      exit 1
    fi
    shift
    if [ "$1" != "auto-work" ]; then
      echo "unexpected server: $1" >&2
      exit 1
    fi
    if [ "$ADD_FAILS" = "1" ]; then
      echo "$ADD_REASON" >&2
      exit 1
    fi
    : > "$STATE_FILE"
    echo "added"
    ;;
  *)
    echo "unsupported mcp subcommand: $1" >&2
    exit 1
    ;;
esac
`, stateFile, boolString(opts.AddFails), addReason, boolString(opts.WarnOnList))
	return writeExecutableScript(t, dir, "fake-codex.sh", script)
}

func writeExecutableScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
