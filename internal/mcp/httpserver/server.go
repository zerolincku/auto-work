package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	mcpreport "auto-work/internal/mcp/report"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
)

const (
	defaultEndpointPath = "/mcp"
	maxPayloadBytes     = 2 * 1024 * 1024
)

type Server struct {
	runRepo       *repository.RunRepository
	projectRepo   *repository.ProjectRepository
	taskRepo      *repository.TaskRepository
	eventRepo     *repository.RunEventRepository
	dispatcher    *scheduler.Dispatcher
	defaultRunID  string
	defaultTaskID string
	path          string
	logger        mcpLogger
}

type mcpLogger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

func NewServer(
	runRepo *repository.RunRepository,
	projectRepo *repository.ProjectRepository,
	taskRepo *repository.TaskRepository,
	eventRepo *repository.RunEventRepository,
	dispatcher *scheduler.Dispatcher,
	defaultRunID string,
	defaultTaskID string,
	endpointPath string,
	logger mcpLogger,
) *Server {
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = defaultEndpointPath
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	return &Server{
		runRepo:       runRepo,
		projectRepo:   projectRepo,
		taskRepo:      taskRepo,
		eventRepo:     eventRepo,
		dispatcher:    dispatcher,
		defaultRunID:  strings.TrimSpace(defaultRunID),
		defaultTaskID: strings.TrimSpace(defaultTaskID),
		path:          endpointPath,
		logger:        logger,
	}
}

func (s *Server) Serve(ctx context.Context, listenAddr string) error {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return errors.New("listen address is empty")
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	return s.ServeListener(ctx, ln)
}

func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		return errors.New("listener is nil")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc(s.path, s.handleMCP)

	srv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"name":     "auto-work-mcp",
		"endpoint": s.path,
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	runID, taskID := s.resolveRunTask(r)
	s.logDebugf("mcp http request method=%s path=%s run_id=%s task_id=%s", r.Method, r.URL.Path, runID, taskID)

	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Allow", "POST, GET, HEAD, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet, http.MethodHead:
		writeJSON(w, http.StatusOK, map[string]any{
			"name":     "auto-work-mcp",
			"endpoint": s.path,
			"message":  "send JSON-RPC via POST",
		})
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "POST, GET, HEAD, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxPayloadBytes))
	if err != nil {
		s.logErrorf("mcp read request failed run_id=%s task_id=%s err=%v", runID, taskID, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "read request failed",
		})
		return
	}
	responses, parseErr := s.processPayload(r.Context(), payload, runID, taskID)
	if parseErr != nil {
		s.logErrorf(
			"mcp parse payload failed run_id=%s task_id=%s err=%v payload=%s",
			runID,
			taskID,
			parseErr,
			truncateForLog(string(payload), 3000),
		)
		writeJSON(w, http.StatusOK, s.errorEnvelope(nil, -32700, "parse error", parseErr.Error()))
		return
	}

	switch len(responses) {
	case 0:
		w.WriteHeader(http.StatusNoContent)
	case 1:
		writeJSON(w, http.StatusOK, responses[0])
	default:
		writeJSON(w, http.StatusOK, responses)
	}
}

func (s *Server) resolveRunTask(r *http.Request) (string, string) {
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if runID == "" {
		runID = strings.TrimSpace(r.Header.Get("X-Auto-Work-Run-ID"))
	}
	if taskID == "" {
		taskID = strings.TrimSpace(r.Header.Get("X-Auto-Work-Task-ID"))
	}
	if runID == "" {
		runID = s.defaultRunID
	}
	if taskID == "" {
		taskID = s.defaultTaskID
	}
	return runID, taskID
}

func (s *Server) processPayload(ctx context.Context, payload []byte, runID, taskID string) ([]map[string]any, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil, errors.New("empty payload")
	}

	if payload[0] == '[' {
		var batch []rpcEnvelope
		if err := json.Unmarshal(payload, &batch); err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(batch))
		for _, req := range batch {
			if resp := s.handleRequest(ctx, req, runID, taskID); resp != nil {
				out = append(out, resp)
			}
		}
		return out, nil
	}

	var req rpcEnvelope
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	resp := s.handleRequest(ctx, req, runID, taskID)
	if resp == nil {
		return nil, nil
	}
	return []map[string]any{resp}, nil
}

func (s *Server) handleRequest(ctx context.Context, req rpcEnvelope, runID, taskID string) map[string]any {
	if req.Method == "" {
		s.logWarnf("mcp request missing method run_id=%s task_id=%s id=%s", runID, taskID, rpcIDForLog(req.ID))
		return nil
	}
	s.logInfof(
		"mcp rpc request method=%s id=%s run_id=%s task_id=%s params=%s",
		req.Method,
		rpcIDForLog(req.ID),
		runID,
		taskID,
		jsonParamsForLog(req.Params, 4000),
	)

	switch req.Method {
	case "initialize":
		if req.ID == nil {
			return nil
		}
		return s.resultEnvelope(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "auto-work-mcp",
				"version": "0.1.0",
			},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		if req.ID == nil {
			return nil
		}
		return s.resultEnvelope(req.ID, mcpreport.ToolListResult())
	case "tools/call":
		if req.ID == nil {
			return nil
		}
		toolName, toolArgs := parseToolCallForLog(req.Params)
		s.logInfof(
			"mcp tool call tool=%s run_id=%s task_id=%s args=%s",
			toolName,
			runID,
			taskID,
			toolArgs,
		)
		reporter := mcpreport.NewService(s.runRepo, s.projectRepo, s.taskRepo, s.eventRepo, s.dispatcher, runID, taskID)
		res, callErr := mcpreport.HandleToolCall(ctx, reporter, req.Params)
		if callErr != nil {
			s.logErrorf("mcp tool call failed tool=%s run_id=%s task_id=%s err=%v", toolName, runID, taskID, callErr)
			return s.resultEnvelope(req.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": callErr.Error()},
				},
				"isError": true,
			})
		}
		s.logInfof("mcp tool call success tool=%s run_id=%s task_id=%s result=%s", toolName, runID, taskID, truncateForLog(res, 1200))
		return s.resultEnvelope(req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": res},
			},
		})
	default:
		if req.ID == nil {
			return nil
		}
		return s.errorEnvelope(req.ID, -32601, "method not found", req.Method)
	}
}

func (s *Server) logDebugf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Debugf(format, args...)
	}
}

func (s *Server) logInfof(format string, args ...any) {
	if s.logger != nil {
		s.logger.Infof(format, args...)
	}
}

func (s *Server) logWarnf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Warnf(format, args...)
	}
}

func (s *Server) logErrorf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Errorf(format, args...)
	}
}

func rpcIDForLog(id json.RawMessage) string {
	val := strings.TrimSpace(string(id))
	if val == "" {
		return "null"
	}
	return truncateForLog(val, 120)
}

func jsonParamsForLog(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return "{}"
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncateForLog(trimmed, maxLen)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncateForLog(trimmed, maxLen)
	}
	return truncateForLog(string(b), maxLen)
}

type toolCallLog struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func parseToolCallForLog(raw json.RawMessage) (toolName string, args string) {
	toolName = "unknown"
	args = "{}"
	var call toolCallLog
	if err := json.Unmarshal(raw, &call); err != nil {
		return toolName, truncateForLog(strings.TrimSpace(string(raw)), 3000)
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		toolName = name
	}
	args = jsonParamsForLog(call.Arguments, 3000)
	return toolName, args
}

func truncateForLog(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return fmt.Sprintf("%s...(truncated %d chars)", text[:maxLen], len(text)-maxLen)
}

func (s *Server) resultEnvelope(id json.RawMessage, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	}
}

func (s *Server) errorEnvelope(id json.RawMessage, code int, message, data string) map[string]any {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
	if id != nil {
		msg["id"] = json.RawMessage(id)
	}
	return msg
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
