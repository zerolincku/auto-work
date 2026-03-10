package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/runsignal"
)

func (a *App) onClaudeLine(runID, stream, line string) {
	a.onRunnerLine("claude", runID, stream, line)
}

func (a *App) onCodexLine(runID, stream, line string) {
	a.onRunnerLine("codex", runID, stream, line)
}

func (a *App) onRunnerLine(provider, runID, stream, line string) {
	kindPrefix := strings.ToLower(strings.TrimSpace(provider))
	if kindPrefix == "" {
		kindPrefix = "runner"
	}
	_ = a.eventRepo.Append(context.Background(), runID, kindPrefix+"."+stream, line)
	if reason := runsignal.ExtractNeedsInputReason(line); strings.TrimSpace(reason) != "" {
		_ = a.eventRepo.Append(context.Background(), runID, "system.needs_input", reason)
		_ = a.stopProviderRun(context.Background(), provider, runID)
	}
	if reason := extractMCPRuntimeFailureReason(line); strings.TrimSpace(reason) != "" {
		_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
	}
	_ = a.runRepo.UpdateHeartbeat(context.Background(), runID, time.Now().UTC())
}

func (a *App) onClaudeExit(runID string, exitCode int, runErr error) {
	a.onRunnerExit("claude", runID, exitCode, runErr)
}

func (a *App) onCodexExit(runID string, exitCode int, runErr error) {
	a.onRunnerExit("codex", runID, exitCode, runErr)
}

func (a *App) onRunnerExit(provider, runID string, exitCode int, runErr error) {
	run, err := a.runRepo.GetByID(context.Background(), runID)
	if err != nil {
		a.log.Errorf("load run on exit failed run_id=%s err=%v", runID, err)
		return
	}
	if run.Status != domain.RunRunning {
		a.log.Debugf("ignore runner exit for non-running run run_id=%s status=%s", runID, run.Status)
		return
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		name = "runner"
	}
	if reason := strings.TrimSpace(a.findNeedsInputReason(runID)); reason != "" {
		summary := fmt.Sprintf("%s 需要人工输入", name)
		details := fmt.Sprintf("needs_input_reason=%s", reason)
		if err := a.dispatcher.MarkRunFinished(context.Background(), runID, domain.RunNeedsInput, domain.TaskBlocked, summary, details, &exitCode); err != nil {
			a.log.Errorf("mark run finished failed run_id=%s provider=%s run_status=%s task_status=%s err=%v", runID, name, domain.RunNeedsInput, domain.TaskBlocked, err)
		}
		a.log.Warnf("runner needs input run_id=%s provider=%s reason=%s", runID, name, reason)
		return
	}

	summary := fmt.Sprintf("%s process exited", name)
	details := fmt.Sprintf("exit_code=%d", exitCode)
	runStatus := domain.RunDone
	taskStatus := domain.TaskDone
	if runErr != nil || exitCode != 0 {
		runStatus = domain.RunFailed
		taskStatus = domain.TaskFailed
		summary = fmt.Sprintf("%s process failed", name)
		if runErr != nil {
			details = fmt.Sprintf("%s, err=%v", details, runErr)
		}
		_ = a.appendRunFailureLog(runID, summary, details)
		a.log.Errorf("runner process failed run_id=%s provider=%s exit_code=%d err=%v", runID, name, exitCode, runErr)
	} else if a.cfg.RequireMCPCallback && strings.EqualFold(name, "claude") {
		if a.tryFinalizeRunWithoutMCP(runID, exitCode) {
			return
		}
		runStatus = domain.RunFailed
		taskStatus = domain.TaskFailed
		summary = "claude exited without mcp report_result callback"
		details = "process exit 0 but no MCP report was received"
		if reason := a.findMCPFailureReason(runID); strings.TrimSpace(reason) != "" {
			_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
			details = fmt.Sprintf("%s; mcp_reason=%s", details, reason)
		}
		_ = a.appendRunFailureLog(runID, summary, details)
	} else if a.cfg.RequireMCPCallback {
		if ok, err := a.runHasEvent(context.Background(), runID, "mcp.report_result.applied"); err == nil && !ok {
			runStatus = domain.RunFailed
			taskStatus = domain.TaskFailed
			summary = fmt.Sprintf("%s exited without mcp report_result callback", name)
			details = "process exit 0 but no MCP report was received"
			if reason := a.findMCPFailureReason(runID); strings.TrimSpace(reason) != "" {
				_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
				details = fmt.Sprintf("%s; mcp_reason=%s", details, reason)
			}
			_ = a.appendRunFailureLog(runID, summary, details)
			a.log.Errorf("runner exited without mcp callback run_id=%s provider=%s", runID, name)
		}
	}
	if err := a.dispatcher.MarkRunFinished(context.Background(), runID, runStatus, taskStatus, summary, details, &exitCode); err != nil {
		a.log.Errorf("mark run finished failed run_id=%s provider=%s run_status=%s task_status=%s err=%v", runID, name, runStatus, taskStatus, err)
		return
	}
	a.log.Infof("runner process finished run_id=%s provider=%s run_status=%s task_status=%s exit_code=%d", runID, name, runStatus, taskStatus, exitCode)
}

func (a *App) tryFinalizeRunWithoutMCP(runID string, exitCode int) bool {
	result, ok := a.findLatestClaudeResultEvent(runID)
	if !ok || result.hasMCPPermissionDenied() || !result.isSuccess() {
		return false
	}
	text := strings.TrimSpace(result.Result)
	if text == "" {
		return false
	}

	summary := "MCP 回调缺失，已按 Claude 最终结果兜底完成"
	details := fmt.Sprintf("fallback_from=claude.result; exit_code=%d\n\n%s", exitCode, trimText(text, 12000))
	_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_fallback", "finalized from Claude result event without MCP callback")
	if err := a.dispatcher.MarkRunFinished(context.Background(), runID, domain.RunDone, domain.TaskDone, summary, details, &exitCode); err != nil {
		_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_fallback_error", err.Error())
		a.log.Errorf("mark run finished failed run_id=%s provider=%s run_status=%s task_status=%s err=%v", runID, "claude", domain.RunDone, domain.TaskDone, err)
		return false
	}
	return true
}

func (a *App) appendRunFailureLog(runID, summary, details string) error {
	payload := fmt.Sprintf("%s | %s", strings.TrimSpace(summary), strings.TrimSpace(details))
	if strings.TrimSpace(payload) == "|" {
		payload = "failure occurred"
	}
	return a.eventRepo.Append(context.Background(), runID, "system.failure", payload)
}

func (a *App) findMCPFailureReason(runID string) string {
	if provider, ok := a.runProvider(runID); ok && provider == "claude" {
		debugPath := a.claudeRunner.DebugLogPath(runID)
		if reason := findMCPFailureInDebugFile(debugPath); strings.TrimSpace(reason) != "" {
			return reason
		}
	}

	events, err := a.eventRepo.ListByRun(context.Background(), runID, 200)
	if err != nil {
		return ""
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		if strings.Contains(e.Payload, `MCP server "auto-work" Connection failed:`) {
			parts := strings.SplitN(e.Payload, `Connection failed:`, 2)
			if len(parts) == 2 {
				if reason := strings.TrimSpace(parts[1]); reason != "" {
					return reason
				}
			}
			return `MCP server "auto-work" connection failed`
		}
		if reason := extractMCPRuntimeFailureReason(e.Payload); strings.TrimSpace(reason) != "" {
			return reason
		}
		if status, ok := extractMCPInitServerStatus(e.Payload, "auto-work"); ok && isMCPServerFailedStatus(status) {
			return fmt.Sprintf("auto-work MCP server status=%s during initialize", status)
		}
	}
	return ""
}

func (a *App) findNeedsInputReason(runID string) string {
	reason, ok, err := a.latestEventPayload(context.Background(), runID, "system.needs_input")
	if err == nil && ok && strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}

	events, err := a.eventRepo.ListByRun(context.Background(), runID, 300)
	if err != nil {
		return ""
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		if reason := runsignal.ExtractNeedsInputReason(e.Payload); strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
	}
	return ""
}

type claudeResultEvent struct {
	Type              string `json:"type"`
	Subtype           string `json:"subtype"`
	IsError           bool   `json:"is_error"`
	Result            string `json:"result"`
	PermissionDenials []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
}

func (e claudeResultEvent) isSuccess() bool {
	return strings.EqualFold(strings.TrimSpace(e.Subtype), "success") && !e.IsError
}

func (e claudeResultEvent) hasMCPPermissionDenied() bool {
	for _, item := range e.PermissionDenials {
		if strings.HasPrefix(strings.TrimSpace(item.ToolName), "mcp__auto-work__") {
			return true
		}
	}
	return false
}

func (a *App) findLatestClaudeResultEvent(runID string) (claudeResultEvent, bool) {
	events, err := a.eventRepo.ListByRun(context.Background(), runID, 400)
	if err != nil {
		return claudeResultEvent{}, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != "claude.stdout" {
			continue
		}
		parsed, ok := parseClaudeResultEvent(events[i].Payload)
		if ok {
			return parsed, true
		}
	}
	return claudeResultEvent{}, false
}

func parseClaudeResultEvent(payload string) (claudeResultEvent, bool) {
	line := strings.TrimSpace(payload)
	if line == "" {
		return claudeResultEvent{}, false
	}
	var out claudeResultEvent
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		return claudeResultEvent{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(out.Type), "result") {
		return claudeResultEvent{}, false
	}
	return out, true
}

func extractMCPPermissionDeniedReason(payload string) string {
	p := strings.TrimSpace(payload)
	if p == "" || !strings.Contains(p, `"permission_denials"`) {
		return ""
	}
	if strings.Contains(p, `"tool_name":"mcp__auto-work__`) && strings.Contains(p, "report_result") {
		return "MCP tool auto-work.report_result permission denied by Claude Code"
	}
	if strings.Contains(p, `"mcp__auto-work__`) {
		return "MCP tool permission denied by Claude Code"
	}
	return ""
}

func extractMCPRuntimeFailureReason(payload string) string {
	p := strings.TrimSpace(payload)
	if p == "" {
		return ""
	}
	if reason := extractMCPPermissionDeniedReason(p); reason != "" {
		return reason
	}
	if strings.Contains(p, `MCP server "auto-work" Connection failed:`) {
		parts := strings.SplitN(p, `Connection failed:`, 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1])
		}
		return `MCP server "auto-work" connection failed`
	}
	if status, ok := extractMCPInitServerStatus(p, "auto-work"); ok && isMCPServerFailedStatus(status) {
		return fmt.Sprintf("auto-work MCP server status=%s during initialize", status)
	}
	if server, ok := parseUnknownMCPServerName(p); ok {
		if strings.EqualFold(server, "todo") {
			return "unknown MCP server 'todo'（请使用 auto-work）"
		}
		if strings.TrimSpace(server) != "" {
			return fmt.Sprintf("unknown MCP server '%s'", strings.TrimSpace(server))
		}
		return "unknown MCP server"
	}
	return ""
}

func parseUnknownMCPServerName(payload string) (string, bool) {
	line := strings.TrimSpace(payload)
	if line == "" {
		return "", false
	}
	lower := strings.ToLower(line)
	key := "unknown mcp server "
	idx := strings.Index(lower, key)
	if idx < 0 {
		return "", false
	}
	tail := strings.TrimSpace(line[idx+len(key):])
	if tail == "" {
		return "", true
	}
	if tail[0] == '\'' || tail[0] == '"' {
		quote := tail[0]
		if end := strings.IndexByte(tail[1:], quote); end >= 0 {
			return strings.TrimSpace(tail[1 : 1+end]), true
		}
	}
	parts := strings.Fields(tail)
	if len(parts) == 0 {
		return "", true
	}
	return strings.TrimSpace(strings.Trim(parts[0], `"'.,;:()[]{}<>`)), true
}

func extractMCPInitServerStatus(payload, serverName string) (string, bool) {
	line := strings.TrimSpace(payload)
	name := strings.ToLower(strings.TrimSpace(serverName))
	if line == "" || name == "" {
		return "", false
	}

	var root any
	if err := json.Unmarshal([]byte(line), &root); err != nil {
		return "", false
	}
	return findMCPServerStatus(root, name)
}

func findMCPServerStatus(node any, serverName string) (string, bool) {
	switch typed := node.(type) {
	case map[string]any:
		if rawServers, ok := typed["mcp_servers"]; ok {
			if status, ok := readMCPServerStatus(rawServers, serverName); ok {
				return status, true
			}
		}
		for _, child := range typed {
			if status, ok := findMCPServerStatus(child, serverName); ok {
				return status, true
			}
		}
	case []any:
		for _, item := range typed {
			if status, ok := findMCPServerStatus(item, serverName); ok {
				return status, true
			}
		}
	}
	return "", false
}

func readMCPServerStatus(raw any, serverName string) (string, bool) {
	servers, ok := raw.([]any)
	if !ok {
		return "", false
	}
	for _, item := range servers {
		server, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(anyToString(server["name"])))
		if name == "" || name != serverName {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(anyToString(server["status"])))
		if status != "" {
			return status, true
		}
	}
	return "", false
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return fmt.Sprint(v)
}

func isMCPServerConnectedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "connected", "ok", "ready":
		return true
	default:
		return false
	}
}

func isMCPServerFailedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "disconnected", "timeout":
		return true
	default:
		return false
	}
}

func trimText(in string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	val := strings.TrimSpace(in)
	r := []rune(val)
	if len(r) <= maxRunes {
		return val
	}
	return strings.TrimSpace(string(r[:maxRunes])) + "...(truncated)"
}
