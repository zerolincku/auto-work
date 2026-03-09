package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"auto-work/internal/repository"
)

const (
	autoWorkMCPServerName        = "auto-work"
	mcpCheckTimeout              = 3 * time.Second
	mcpAddTimeout                = 5 * time.Second
	mcpAddFailureRetryWindow     = 30 * time.Second
	mcpStatusProjectRequiredMsg  = "请选择项目后再检查 MCP 状态"
	mcpStatusCallbackDisabledMsg = "MCP 回调未启用"
)

type mcpProvisionFailure struct {
	Message string
	At      time.Time
}

type codexMCPServerConfig struct {
	Name           string  `json:"name"`
	Enabled        bool    `json:"enabled"`
	DisabledReason *string `json:"disabled_reason"`
}

func (a *App) MCPStatus(ctx context.Context, projectID string) (*MCPStatusView, error) {
	if !a.cfg.EnableMCPCallback {
		return a.newMCPStatusView(false, "disabled", mcpStatusCallbackDisabledMsg), nil
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return a.newMCPStatusView(true, "unknown", mcpStatusProjectRequiredMsg), nil
	}
	project, err := a.projectRepo.GetByID(ctxOrBackground(ctx), projectID)
	if err != nil {
		if errors.Is(err, repository.ErrProjectNotFound) {
			return a.newMCPStatusView(true, "failed", "项目不存在，无法检查 MCP 状态"), nil
		}
		return nil, err
	}

	provider := strings.ToLower(strings.TrimSpace(project.DefaultProvider))
	if provider == "" {
		provider = "claude"
	}

	switch provider {
	case "claude":
		return a.ensureClaudeMCPStatus(ctx)
	case "codex":
		return a.ensureCodexMCPStatus(ctx)
	default:
		return a.newMCPStatusView(true, "failed", fmt.Sprintf("不支持的 provider：%s", provider)), nil
	}
}

func (a *App) ensureClaudeMCPStatus(ctx context.Context) (*MCPStatusView, error) {
	a.mcpCheckMu.Lock()
	defer a.mcpCheckMu.Unlock()

	output, err := a.runExternalCommand(ctx, mcpCheckTimeout, strings.TrimSpace(a.cfg.ClaudeBinary), "mcp", "get", autoWorkMCPServerName)
	if err == nil {
		a.clearMCPProvisionFailure(a.mcpProvisionKey("claude"))
		return a.newMCPStatusView(true, "connected", "Claude MCP 已配置"), nil
	}
	if !isClaudeMCPMissing(output, err) {
		return a.newMCPStatusView(true, "failed", fmt.Sprintf("Claude MCP 检查失败：%s", formatCommandFailure(output, err))), nil
	}
	if msg, ok := a.recentMCPProvisionFailure(a.mcpProvisionKey("claude")); ok {
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	baseURL := strings.TrimSpace(a.cfg.MCPHTTPURL)
	if baseURL == "" {
		msg := "Claude MCP 自动添加失败：MCP HTTP 地址为空"
		a.recordMCPProvisionFailure(a.mcpProvisionKey("claude"), msg)
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	output, err = a.runExternalCommand(
		ctx,
		mcpAddTimeout,
		strings.TrimSpace(a.cfg.ClaudeBinary),
		"mcp", "add", "--scope", "user", "--transport", "http", autoWorkMCPServerName, baseURL,
	)
	if err != nil {
		msg := fmt.Sprintf("Claude MCP 自动添加失败：%s", formatCommandFailure(output, err))
		a.recordMCPProvisionFailure(a.mcpProvisionKey("claude"), msg)
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	a.clearMCPProvisionFailure(a.mcpProvisionKey("claude"))
	return a.newMCPStatusView(true, "connected", "Claude MCP 缺失，已自动添加"), nil
}

func (a *App) ensureCodexMCPStatus(ctx context.Context) (*MCPStatusView, error) {
	a.mcpCheckMu.Lock()
	defer a.mcpCheckMu.Unlock()

	output, err := a.runExternalCommand(ctx, mcpCheckTimeout, strings.TrimSpace(a.cfg.CodexBinary), "mcp", "list", "--json")
	if err != nil {
		return a.newMCPStatusView(true, "failed", fmt.Sprintf("Codex MCP 检查失败：%s", formatCommandFailure(output, err))), nil
	}

	items, parseErr := parseCodexMCPList(output)
	if parseErr != nil {
		return a.newMCPStatusView(true, "failed", fmt.Sprintf("Codex MCP 检查失败：%s", parseErr.Error())), nil
	}
	for _, item := range items {
		if strings.TrimSpace(item.Name) != autoWorkMCPServerName {
			continue
		}
		a.clearMCPProvisionFailure(a.mcpProvisionKey("codex"))
		if !item.Enabled {
			reason := strings.TrimSpace(derefString(item.DisabledReason))
			if reason == "" {
				reason = "未提供禁用原因"
			}
			return a.newMCPStatusView(true, "failed", fmt.Sprintf("Codex MCP 已存在但不可用：%s", reason)), nil
		}
		return a.newMCPStatusView(true, "connected", "Codex MCP 已配置"), nil
	}

	if msg, ok := a.recentMCPProvisionFailure(a.mcpProvisionKey("codex")); ok {
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	baseURL := strings.TrimSpace(a.cfg.MCPHTTPURL)
	if baseURL == "" {
		msg := "Codex MCP 自动添加失败：MCP HTTP 地址为空"
		a.recordMCPProvisionFailure(a.mcpProvisionKey("codex"), msg)
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	output, err = a.runExternalCommand(
		ctx,
		mcpAddTimeout,
		strings.TrimSpace(a.cfg.CodexBinary),
		"mcp", "add", "--url", baseURL, autoWorkMCPServerName,
	)
	if err != nil {
		msg := fmt.Sprintf("Codex MCP 自动添加失败：%s", formatCommandFailure(output, err))
		a.recordMCPProvisionFailure(a.mcpProvisionKey("codex"), msg)
		return a.newMCPStatusView(true, "failed", msg), nil
	}

	a.clearMCPProvisionFailure(a.mcpProvisionKey("codex"))
	return a.newMCPStatusView(true, "connected", "Codex MCP 缺失，已自动添加"), nil
}

func (a *App) newMCPStatusView(enabled bool, state, message string) *MCPStatusView {
	now := time.Now().UTC()
	return &MCPStatusView{
		Enabled:   enabled,
		State:     strings.TrimSpace(state),
		Message:   strings.TrimSpace(message),
		UpdatedAt: &now,
	}
}

func (a *App) runExternalCommand(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("command is empty")
	}
	ctx := ctxOrBackground(parent)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		if output != "" {
			return output, fmt.Errorf("%w: %s", ctx.Err(), output)
		}
		return output, ctx.Err()
	}
	return output, err
}

func (a *App) mcpProvisionKey(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.TrimSpace(a.cfg.MCPHTTPURL)
}

func (a *App) recentMCPProvisionFailure(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	item, ok := a.mcpProvisionFailures[key]
	if !ok {
		return "", false
	}
	if time.Since(item.At) > mcpAddFailureRetryWindow {
		delete(a.mcpProvisionFailures, key)
		return "", false
	}
	return strings.TrimSpace(item.Message), true
}

func (a *App) recordMCPProvisionFailure(key, message string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if a.mcpProvisionFailures == nil {
		a.mcpProvisionFailures = make(map[string]mcpProvisionFailure)
	}
	a.mcpProvisionFailures[key] = mcpProvisionFailure{
		Message: strings.TrimSpace(message),
		At:      time.Now().UTC(),
	}
}

func (a *App) clearMCPProvisionFailure(key string) {
	key = strings.TrimSpace(key)
	if key == "" || a.mcpProvisionFailures == nil {
		return
	}
	delete(a.mcpProvisionFailures, key)
}

func parseCodexMCPList(raw string) ([]codexMCPServerConfig, error) {
	payload := strings.TrimSpace(extractJSONArray(raw))
	if payload == "" {
		return nil, errors.New("Codex MCP 列表为空")
	}
	var items []codexMCPServerConfig
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return nil, fmt.Errorf("无法解析 Codex MCP 列表：%w", err)
	}
	return items, nil
}

func extractJSONArray(raw string) string {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < start {
		return ""
	}
	return raw[start : end+1]
}

func isClaudeMCPMissing(output string, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(text, "no mcp server found with name") {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no mcp server found with name")
}

func formatCommandFailure(output string, err error) string {
	if msg := strings.TrimSpace(output); msg != "" {
		return msg
	}
	if err == nil {
		return "unknown error"
	}
	return strings.TrimSpace(err.Error())
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *App) runHasEvent(ctx context.Context, runID, kind string) (bool, error) {
	row := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM run_events WHERE run_id = ? AND kind = ?`, strings.TrimSpace(runID), strings.TrimSpace(kind))
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) latestEventPayload(ctx context.Context, runID, kind string) (string, bool, error) {
	row := a.db.QueryRowContext(ctx, `
SELECT payload
FROM run_events
WHERE run_id = ? AND kind = ?
ORDER BY ts DESC
LIMIT 1`, strings.TrimSpace(runID), strings.TrimSpace(kind))
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(payload), true, nil
}

func (a *App) runProvider(runID string) (string, bool) {
	row := a.db.QueryRowContext(context.Background(), `
SELECT t.provider
FROM runs r
JOIN tasks t ON t.id = r.task_id
WHERE r.id = ?
LIMIT 1`, strings.TrimSpace(runID))
	var provider string
	if err := row.Scan(&provider); err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(provider)), true
}

func isProviderStdoutKind(kind, provider string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return strings.HasSuffix(kind, ".stdout")
	}
	return kind == provider+".stdout"
}
