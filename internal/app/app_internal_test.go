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
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs:    []string{"mcp", "get", autoWorkMCPServerName},
			output:      autoWorkMCPServerName,
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureClaudeMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "connected", "Claude MCP 已配置")
}

func TestMCPStatus_ClaudeMissingAutoAdds(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs:    []string{"mcp", "get", autoWorkMCPServerName},
			output:      "No MCP server found with name: auto-work",
			err:         errExitStatus1,
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs: []string{
				"mcp", "add", "--scope", "user", "--transport", "http", autoWorkMCPServerName, testMCPHTTPURL,
			},
			output: "added",
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureClaudeMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "connected", "已自动添加")
}

func TestMCPStatus_ClaudeAutoAddFailureIncludesReason(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs:    []string{"mcp", "get", autoWorkMCPServerName},
			output:      "No MCP server found with name: auto-work",
			err:         errExitStatus1,
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs: []string{
				"mcp", "add", "--scope", "user", "--transport", "http", autoWorkMCPServerName, testMCPHTTPURL,
			},
			output: "permission denied",
			err:    errExitStatus1,
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureClaudeMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "failed", "permission denied")
}

func TestMCPStatus_CodexConnectedByConfig(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "list", "--json"},
			output:      "WARNING: proceeding, even though we could not update PATH: Operation not permitted (os error 1)\n[{\"name\":\"auto-work\",\"enabled\":true,\"disabled_reason\":null}]",
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureCodexMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "connected", "Codex MCP 已配置")
}

func TestMCPStatus_CodexMissingAutoAdds(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "list", "--json"},
			output:      "[]",
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "add", "--url", testMCPHTTPURL, autoWorkMCPServerName},
			output:      "added",
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureCodexMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "connected", "已自动添加")
}

func TestMCPStatus_CodexAutoAddFailureIncludesReason(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "list", "--json"},
			output:      "[]",
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "add", "--url", testMCPHTTPURL, autoWorkMCPServerName},
			output:      "permission denied",
			err:         errExitStatus1,
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureCodexMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	assertMCPStatus(t, status, "failed", "permission denied")
}

func TestDispatchOnce_SameProviderDifferentProjectsDoNotBlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeRunnerBinary(t, dir, "fake-claude-runner.sh", 3*time.Second)

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: true,
		RequireMCPCallback:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	projectADir := filepath.Join(dir, "project-a")
	projectBDir := filepath.Join(dir, "project-b")
	if err := os.MkdirAll(projectADir, 0o755); err != nil {
		t.Fatalf("mkdir project a: %v", err)
	}
	if err := os.MkdirAll(projectBDir, 0o755); err != nil {
		t.Fatalf("mkdir project b: %v", err)
	}

	projectA, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "project-a",
		Path:            projectADir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project a: %v", err)
	}
	projectB, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "project-b",
		Path:            projectBDir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project b: %v", err)
	}

	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: projectA.ID, Title: "task-a", Description: "desc-a"}); err != nil {
		t.Fatalf("create task a: %v", err)
	}
	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: projectB.ID, Title: "task-b", Description: "desc-b"}); err != nil {
		t.Fatalf("create task b: %v", err)
	}

	respA, err := application.DispatchOnce(ctx, "", projectA.ID)
	if err != nil {
		t.Fatalf("dispatch project a: %v", err)
	}
	if respA == nil || !respA.Claimed {
		t.Fatalf("expected project a claimed, got %#v", respA)
	}

	respB, err := application.DispatchOnce(ctx, "", projectB.ID)
	if err != nil {
		t.Fatalf("dispatch project b: %v", err)
	}
	if respB == nil || !respB.Claimed {
		t.Fatalf("expected project b claimed, got %#v", respB)
	}

	running, err := application.ListRunningRuns(ctx, "", 10)
	if err != nil {
		t.Fatalf("list running runs: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("expected 2 running runs, got %d", len(running))
	}

	agentByProject := map[string]string{}
	for _, item := range running {
		agentByProject[item.ProjectID] = item.AgentID
	}
	if got := agentByProject[projectA.ID]; got != projectDispatchAgentID("claude", projectA.ID) {
		t.Fatalf("unexpected project a agent id: %q", got)
	}
	if got := agentByProject[projectB.ID]; got != projectDispatchAgentID("claude", projectB.ID) {
		t.Fatalf("unexpected project b agent id: %q", got)
	}
	if agentByProject[projectA.ID] == agentByProject[projectB.ID] {
		t.Fatalf("expected different agents per project, got same agent %q", agentByProject[projectA.ID])
	}
}

func TestDispatchProjectAvailableTasks_SameProviderProjectsClaimIndependently(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeRunnerBinary(t, dir, "fake-claude-auto-runner.sh", 3*time.Second)

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: true,
		RequireMCPCallback:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	projectADir := filepath.Join(dir, "auto-project-a")
	projectBDir := filepath.Join(dir, "auto-project-b")
	if err := os.MkdirAll(projectADir, 0o755); err != nil {
		t.Fatalf("mkdir auto project a: %v", err)
	}
	if err := os.MkdirAll(projectBDir, 0o755); err != nil {
		t.Fatalf("mkdir auto project b: %v", err)
	}

	projectA, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "auto-project-a",
		Path:            projectADir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create auto project a: %v", err)
	}
	projectB, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "auto-project-b",
		Path:            projectBDir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create auto project b: %v", err)
	}

	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: projectA.ID, Title: "auto-task-a", Description: "desc-a"}); err != nil {
		t.Fatalf("create auto task a: %v", err)
	}
	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: projectB.ID, Title: "auto-task-b", Description: "desc-b"}); err != nil {
		t.Fatalf("create auto task b: %v", err)
	}

	claimedA := application.dispatchProjectAvailableTasks(ctx, projectA.ID, autoDispatchProjectBurstLimit)
	claimedB := application.dispatchProjectAvailableTasks(ctx, projectB.ID, autoDispatchProjectBurstLimit)
	if claimedA != 1 || claimedB != 1 {
		t.Fatalf("expected each project helper to claim 1 task, got projectA=%d projectB=%d", claimedA, claimedB)
	}

	running, err := application.ListRunningRuns(ctx, "", 10)
	if err != nil {
		t.Fatalf("list running runs: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("expected 2 running runs across projects, got %d", len(running))
	}
	for _, item := range running {
		expectedAgentID := projectDispatchAgentID("claude", item.ProjectID)
		if item.AgentID != expectedAgentID {
			t.Fatalf("unexpected agent id for project %s: got %q want %q", item.ProjectID, item.AgentID, expectedAgentID)
		}
	}
}

func TestDispatchProjectAvailableTasks_SameProjectSamePriorityClaimsConcurrently(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	claudeBinary := writeFakeRunnerBinary(t, dir, "fake-claude-same-project-runner.sh", 3*time.Second)

	application, err := New(ctx, config.Config{
		DatabasePath:        filepath.Join(dir, "test.db"),
		ClaudeBinary:        claudeBinary,
		RunClaudeOnDispatch: true,
		RequireMCPCallback:  false,
	})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })

	projectDir := filepath.Join(dir, "same-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir same project dir: %v", err)
	}
	project, err := application.CreateProject(ctx, CreateProjectRequest{
		Name:            "same-project",
		Path:            projectDir,
		DefaultProvider: "claude",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: project.ID, Title: "task-a", Description: "desc-a", Priority: 100}); err != nil {
		t.Fatalf("create task a: %v", err)
	}
	if _, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: project.ID, Title: "task-b", Description: "desc-b", Priority: 100}); err != nil {
		t.Fatalf("create task b: %v", err)
	}
	third, err := application.CreateTask(ctx, CreateTaskRequest{ProjectID: project.ID, Title: "task-c", Description: "desc-c", Priority: 200})
	if err != nil {
		t.Fatalf("create task c: %v", err)
	}

	claimed := application.dispatchProjectAvailableTasks(ctx, project.ID, autoDispatchProjectBurstLimit)
	if claimed != 2 {
		t.Fatalf("expected helper to claim 2 same-priority tasks, got %d", claimed)
	}

	running, err := application.ListRunningRuns(ctx, project.ID, 10)
	if err != nil {
		t.Fatalf("list running runs: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("expected 2 same-priority running runs, got %d", len(running))
	}
	for _, item := range running {
		if item.AgentID != projectDispatchAgentID("claude", project.ID) {
			t.Fatalf("unexpected agent id: %q", item.AgentID)
		}
	}

	items, err := application.ListTasks(ctx, "", "", project.ID, 10)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	statusByID := map[string]string{}
	for _, item := range items {
		statusByID[item.ID] = string(item.Status)
	}
	if statusByID[third.ID] != "pending" {
		t.Fatalf("expected lower-priority task pending while same-priority batch running, got %q", statusByID[third.ID])
	}
}

func writeFakeRunnerBinary(t *testing.T, dir, name string, sleepFor time.Duration) string {
	t.Helper()
	seconds := int(sleepFor / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
sleep %d
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok","permission_denials":[]}'
`, seconds)
	return writeExecutableScript(t, dir, name, script)
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
