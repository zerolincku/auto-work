package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
)

func TestParseToolCallForLog(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"name":"auto-work.report_result",
		"arguments":{"status":"success","summary":"ok","details":"done"}
	}`)
	name, args := parseToolCallForLog(raw)
	if name != "auto-work.report_result" {
		t.Fatalf("unexpected tool name: %s", name)
	}
	if !strings.Contains(args, `"status":"success"`) {
		t.Fatalf("unexpected args: %s", args)
	}
}

func TestJsonParamsForLog_InvalidJSONFallback(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{invalid-json}`)
	out := jsonParamsForLog(raw, 100)
	if out == "" {
		t.Fatalf("expected fallback output")
	}
	if !strings.Contains(out, "{invalid-json}") {
		t.Fatalf("unexpected fallback output: %s", out)
	}
}

func TestHandleHealthz(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "", "", "/mcp", nil)

	rec := performRequest(t, server, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var payload map[string]any
	decodeJSON(t, rec.Body.Bytes(), &payload)
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", payload["ok"])
	}
	if endpoint, _ := payload["endpoint"].(string); endpoint != "/mcp" {
		t.Fatalf("expected endpoint /mcp, got %q", endpoint)
	}

	rec = performRequest(t, server, http.MethodPost, "/healthz", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("expected Allow header GET, HEAD, got %q", allow)
	}
}

func TestHandleMCP_OPTIONSAndGET(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "default-run", "default-task", "/mcp", nil)

	rec := performRequest(t, server, http.MethodOptions, "/mcp", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST, GET, HEAD, OPTIONS" {
		t.Fatalf("expected Allow header for MCP endpoint, got %q", allow)
	}

	rec = performRequest(t, server, http.MethodGet, "/mcp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var payload map[string]any
	decodeJSON(t, rec.Body.Bytes(), &payload)
	if msg, _ := payload["message"].(string); msg != "send JSON-RPC via POST" {
		t.Fatalf("unexpected GET message: %q", msg)
	}
	if endpoint, _ := payload["endpoint"].(string); endpoint != "/mcp" {
		t.Fatalf("expected endpoint /mcp, got %q", endpoint)
	}
}

func TestHandleMCP_POSTEmptyPayloadReturnsParseError(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "", "", "/mcp", nil)
	rec := performRequest(t, server, http.MethodPost, "/mcp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp rpcErrorResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp.Error.Code != -32700 {
		t.Fatalf("expected parse error code -32700, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "parse error" {
		t.Fatalf("expected parse error message, got %q", resp.Error.Message)
	}
	if resp.Error.Data != "empty payload" {
		t.Fatalf("expected empty payload detail, got %q", resp.Error.Data)
	}
}

func TestHandleMCP_POSTInvalidJSONReturnsParseError(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "", "", "/mcp", nil)
	rec := performRequest(t, server, http.MethodPost, "/mcp", `{invalid-json}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp rpcErrorResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp.Error.Code != -32700 {
		t.Fatalf("expected parse error code -32700, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "parse error" {
		t.Fatalf("expected parse error message, got %q", resp.Error.Message)
	}
	if resp.Error.Data == "" {
		t.Fatalf("expected parse error detail")
	}
}

func TestHandleMCP_POSTInitializeReturnsCapabilities(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "", "", "/mcp", nil)
	rec := performRequest(t, server, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tester"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp rpcSuccessResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if strings.TrimSpace(string(resp.ID)) != "1" {
		t.Fatalf("expected id 1, got %s", string(resp.ID))
	}

	var result initializeResult
	decodeJSON(t, resp.Result, &result)
	if result.ProtocolVersion != "2024-11-05" {
		t.Fatalf("unexpected protocol version: %q", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "auto-work-mcp" {
		t.Fatalf("unexpected server name: %q", result.ServerInfo.Name)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		t.Fatalf("expected tools capability in initialize result")
	}
}

func TestHandleMCP_POSTBatchReturnsMultipleResponses(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "", "", "/mcp", nil)
	body := `[
		{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tester"}}},
		{"jsonrpc":"2.0","id":2,"method":"tools/list"}
	]`
	rec := performRequest(t, server, http.MethodPost, "/mcp", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var responses []rpcSuccessResponse
	decodeJSON(t, rec.Body.Bytes(), &responses)
	if len(responses) != 2 {
		t.Fatalf("expected 2 batch responses, got %d", len(responses))
	}

	var initResult initializeResult
	decodeJSON(t, responses[0].Result, &initResult)
	if initResult.ProtocolVersion != "2024-11-05" {
		t.Fatalf("unexpected initialize protocol version: %q", initResult.ProtocolVersion)
	}

	var listResult toolListResult
	decodeJSON(t, responses[1].Result, &listResult)
	if len(listResult.Tools) == 0 {
		t.Fatalf("expected non-empty tools list")
	}
}

func TestResolveRunTask(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, nil, nil, nil, nil, "default-run", "default-task", "/mcp", nil)
	tests := []struct {
		name       string
		target     string
		headers    map[string]string
		wantRunID  string
		wantTaskID string
	}{
		{
			name:       "query overrides header and default",
			target:     "/mcp?run_id=query-run&task_id=query-task",
			headers:    map[string]string{"X-Auto-Work-Run-ID": "header-run", "X-Auto-Work-Task-ID": "header-task"},
			wantRunID:  "query-run",
			wantTaskID: "query-task",
		},
		{
			name:       "headers override default",
			target:     "/mcp",
			headers:    map[string]string{"X-Auto-Work-Run-ID": "header-run", "X-Auto-Work-Task-ID": "header-task"},
			wantRunID:  "header-run",
			wantTaskID: "header-task",
		},
		{
			name:       "defaults used when request has no context",
			target:     "/mcp",
			wantRunID:  "default-run",
			wantTaskID: "default-task",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, tt.target, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			runID, taskID := server.resolveRunTask(req)
			if runID != tt.wantRunID || taskID != tt.wantTaskID {
				t.Fatalf("expected run/task %s/%s, got %s/%s", tt.wantRunID, tt.wantTaskID, runID, taskID)
			}
		})
	}
}

func TestHandleMCP_POSTToolCallReportResultUpdatesRunAndTask(t *testing.T) {
	t.Parallel()

	fixture := setupHTTPServerFixture(t)
	rec := performRequest(t, fixture.server, http.MethodPost, "/mcp", `{
		"jsonrpc":"2.0",
		"id":1,
		"method":"tools/call",
		"params":{
			"name":"auto-work.report_result",
			"arguments":{"status":"success","summary":"ok","details":"done"}
		}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp rpcSuccessResponse
	decodeJSON(t, rec.Body.Bytes(), &resp)
	var result toolCallResult
	decodeJSON(t, resp.Result, &result)
	if result.IsError {
		t.Fatalf("expected successful tool call result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("unexpected tool call content: %+v", result.Content)
	}

	ctx := context.Background()
	gotRun, err := fixture.runRepo.GetByID(ctx, fixture.run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if gotRun.Status != domain.RunDone {
		t.Fatalf("expected run done, got %s", gotRun.Status)
	}

	gotTask, err := fixture.taskRepo.GetByID(ctx, fixture.task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status != domain.TaskDone {
		t.Fatalf("expected task done, got %s", gotTask.Status)
	}
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
}

type rpcSuccessResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type toolListResult struct {
	Tools []map[string]any `json:"tools"`
}

type toolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type httpServerFixture struct {
	server   *Server
	runRepo  *repository.RunRepository
	taskRepo *repository.TaskRepository
	project  *domain.Project
	run      *domain.Run
	task     *domain.Task
}

func setupHTTPServerFixture(t *testing.T) *httpServerFixture {
	t.Helper()

	ctx := context.Background()
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

	now := time.Now().UTC()
	project := &domain.Project{
		ID:        uuid.NewString(),
		Name:      "P",
		Path:      "/tmp/p",
		CreatedAt: now,
		UpdatedAt: now,
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
		CreatedAt:   now,
		UpdatedAt:   now,
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
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create running task: %v", err)
	}

	pendingTask := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   project.ID,
		Title:       "T2",
		Description: "D2",
		Priority:    101,
		Status:      domain.TaskPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := taskRepo.Create(ctx, pendingTask); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	run := &domain.Run{
		ID:             uuid.NewString(),
		TaskID:         task.ID,
		AgentID:        agent.ID,
		Attempt:        1,
		Status:         domain.RunRunning,
		StartedAt:      now,
		PromptSnapshot: "snapshot",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := NewServer(runRepo, projectRepo, taskRepo, eventRepo, dispatcher, run.ID, task.ID, "/mcp", nil)
	return &httpServerFixture{
		server:   server,
		runRepo:  runRepo,
		taskRepo: taskRepo,
		project:  project,
		run:      run,
		task:     task,
	}
}

func performRequest(t *testing.T, server *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody *strings.Reader
	if body != "" {
		requestBody = strings.NewReader(body)
	} else {
		requestBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, requestBody)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	newTestHandler(server).ServeHTTP(rec, req)
	return rec
}

func newTestHandler(server *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.HandleFunc(server.path, server.handleMCP)
	return mux
}

func decodeJSON(t *testing.T, payload []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(payload, out); err != nil {
		t.Fatalf("decode json: %v\npayload=%s", err, string(payload))
	}
}
