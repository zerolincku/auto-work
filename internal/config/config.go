package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultDatabasePath = "./data/auto-work.db"
	defaultMCPHTTPURL   = "http://127.0.0.1:39123/mcp"
)

type Config struct {
	DatabasePath         string
	WorkspacePath        string
	AppLogPath           string
	AppLogMaxSizeMB      int
	AppLogMaxBackups     int
	ClaudeBinary         string
	ClaudeModel          string
	ClaudeAllowedTools   string
	ClaudePermissionMode string
	RunClaudeOnDispatch  bool
	CodexBinary          string
	CodexModel           string
	RunCodexOnDispatch   bool
	EnableMCPCallback    bool
	RequireMCPCallback   bool
	MCPHTTPURL           string
	TelegramEnabled      bool
	TelegramBotToken     string
	TelegramChatIDs      []int64
	TelegramPollTimeout  int
}

func Load() Config {
	dbPath := os.Getenv("AUTO_WORK_DB_PATH")
	if dbPath == "" {
		dbPath = defaultDatabasePath
	}
	workspacePath := os.Getenv("AUTO_WORK_WORKSPACE")
	if workspacePath == "" {
		workspacePath, _ = os.Getwd()
	}
	appLogPath := strings.TrimSpace(os.Getenv("AUTO_WORK_APP_LOG_PATH"))
	if appLogPath == "" {
		appLogPath = defaultAppLogPath()
	}
	appLogMaxSizeMB := parseEnvIntWithDefault("AUTO_WORK_APP_LOG_MAX_SIZE_MB", 20)
	if appLogMaxSizeMB <= 0 {
		appLogMaxSizeMB = 20
	}
	appLogMaxBackups := parseEnvIntWithDefault("AUTO_WORK_APP_LOG_MAX_BACKUPS", 5)
	if appLogMaxBackups <= 0 {
		appLogMaxBackups = 5
	}

	claudeBinary := os.Getenv("AUTO_WORK_CLAUDE_BIN")
	if claudeBinary == "" {
		claudeBinary = "claude"
	}
	claudeModel := strings.TrimSpace(os.Getenv("AUTO_WORK_CLAUDE_MODEL"))

	codexBinary := os.Getenv("AUTO_WORK_CODEX_BIN")
	if codexBinary == "" {
		codexBinary = "codex"
	}
	codexModel := strings.TrimSpace(os.Getenv("AUTO_WORK_CODEX_MODEL"))

	allowedTools := os.Getenv("AUTO_WORK_CLAUDE_ALLOWED_TOOLS")

	enableMCPCallback := os.Getenv("AUTO_WORK_ENABLE_MCP_CALLBACK") != "0"
	requireMCPCallback := os.Getenv("AUTO_WORK_REQUIRE_MCP_CALLBACK") != "0"
	mcpHTTPURL := strings.TrimSpace(os.Getenv("AUTO_WORK_MCP_HTTP_URL"))
	if mcpHTTPURL == "" {
		mcpHTTPURL = defaultMCPHTTPURL
	}

	permissionMode := os.Getenv("AUTO_WORK_CLAUDE_PERMISSION_MODE")
	if permissionMode == "" {
		// Headless automation needs deterministic tool execution.
		// Use bypassPermissions by default to avoid MCP callback being blocked.
		permissionMode = "bypassPermissions"
	}
	// If callback is required, force bypass permissions to avoid interactive denials.
	if enableMCPCallback && requireMCPCallback {
		permissionMode = "bypassPermissions"
	}

	runClaudeOnDispatch := true
	runSwitch := os.Getenv("AUTO_WORK_RUN_CLAUDE_ON_DISPATCH")
	if runSwitch == "0" || strings.EqualFold(runSwitch, "false") {
		runClaudeOnDispatch = false
	}
	runCodexOnDispatch := true
	codexRunSwitch := os.Getenv("AUTO_WORK_RUN_CODEX_ON_DISPATCH")
	if codexRunSwitch == "0" || strings.EqualFold(codexRunSwitch, "false") {
		runCodexOnDispatch = false
	}

	token := telegramToken()
	return Config{
		DatabasePath:         dbPath,
		WorkspacePath:        workspacePath,
		AppLogPath:           appLogPath,
		AppLogMaxSizeMB:      appLogMaxSizeMB,
		AppLogMaxBackups:     appLogMaxBackups,
		ClaudeBinary:         claudeBinary,
		ClaudeModel:          claudeModel,
		ClaudeAllowedTools:   allowedTools,
		ClaudePermissionMode: permissionMode,
		RunClaudeOnDispatch:  runClaudeOnDispatch,
		CodexBinary:          codexBinary,
		CodexModel:           codexModel,
		RunCodexOnDispatch:   runCodexOnDispatch,
		EnableMCPCallback:    enableMCPCallback,
		RequireMCPCallback:   requireMCPCallback,
		MCPHTTPURL:           mcpHTTPURL,
		TelegramEnabled:      telegramEnabled(token),
		TelegramBotToken:     token,
		TelegramChatIDs:      parseTelegramChatIDs(os.Getenv("AUTO_WORK_TELEGRAM_CHAT_IDS")),
		TelegramPollTimeout:  telegramPollTimeout(),
	}
}

func defaultAppLogPath() string {
	return filepath.Join(".", "data", "log", "auto-work.log")
}

func parseEnvIntWithDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return val
}

func telegramToken() string {
	return strings.TrimSpace(os.Getenv("AUTO_WORK_TELEGRAM_BOT_TOKEN"))
}

func telegramEnabled(token string) bool {
	v := strings.TrimSpace(os.Getenv("AUTO_WORK_TELEGRAM_ENABLED"))
	if v == "" {
		return token != ""
	}
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

func telegramPollTimeout() int {
	v := strings.TrimSpace(os.Getenv("AUTO_WORK_TELEGRAM_POLL_TIMEOUT"))
	if v == "" {
		return 30
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 120 {
		return n
	}
	return 30
}

func parseTelegramChatIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
