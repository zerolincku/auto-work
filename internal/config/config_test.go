package config_test

import (
	"reflect"
	"testing"

	"auto-work/internal/config"
)

func TestLoad_DefaultRunClaudeOnDispatchEnabled(t *testing.T) {
	t.Setenv("AUTO_WORK_RUN_CLAUDE_ON_DISPATCH", "")
	cfg := config.Load()
	if !cfg.RunClaudeOnDispatch {
		t.Fatalf("expected RunClaudeOnDispatch default true")
	}
}

func TestLoad_RunClaudeOnDispatchCanBeDisabled(t *testing.T) {
	t.Setenv("AUTO_WORK_RUN_CLAUDE_ON_DISPATCH", "0")
	cfg := config.Load()
	if cfg.RunClaudeOnDispatch {
		t.Fatalf("expected RunClaudeOnDispatch false when env=0")
	}

	t.Setenv("AUTO_WORK_RUN_CLAUDE_ON_DISPATCH", "false")
	cfg = config.Load()
	if cfg.RunClaudeOnDispatch {
		t.Fatalf("expected RunClaudeOnDispatch false when env=false")
	}
}

func TestLoad_ForceBypassPermissionsWhenMCPRequired(t *testing.T) {
	t.Setenv("AUTO_WORK_ENABLE_MCP_CALLBACK", "1")
	t.Setenv("AUTO_WORK_REQUIRE_MCP_CALLBACK", "1")
	t.Setenv("AUTO_WORK_CLAUDE_PERMISSION_MODE", "acceptEdits")

	cfg := config.Load()
	if cfg.ClaudePermissionMode != "bypassPermissions" {
		t.Fatalf("expected bypassPermissions when MCP callback required, got %s", cfg.ClaudePermissionMode)
	}
}

func TestLoad_TelegramDefaultsDisabledWithoutToken(t *testing.T) {
	t.Setenv("AUTO_WORK_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("AUTO_WORK_TELEGRAM_ENABLED", "")
	t.Setenv("AUTO_WORK_TELEGRAM_CHAT_IDS", "")
	t.Setenv("AUTO_WORK_TELEGRAM_POLL_TIMEOUT", "")

	cfg := config.Load()
	if cfg.TelegramEnabled {
		t.Fatalf("expected telegram disabled when token absent and enabled flag empty")
	}
	if cfg.TelegramBotToken != "" {
		t.Fatalf("expected empty telegram token")
	}
	if cfg.TelegramPollTimeout != 30 {
		t.Fatalf("expected default telegram poll timeout=30, got %d", cfg.TelegramPollTimeout)
	}
	if len(cfg.TelegramChatIDs) != 0 {
		t.Fatalf("expected empty chat ids")
	}
}

func TestLoad_TelegramEnabledAndParsesSettings(t *testing.T) {
	t.Setenv("AUTO_WORK_TELEGRAM_BOT_TOKEN", "  abc123  ")
	t.Setenv("AUTO_WORK_TELEGRAM_ENABLED", "")
	t.Setenv("AUTO_WORK_TELEGRAM_CHAT_IDS", "1001, ,abc,1002,-99")
	t.Setenv("AUTO_WORK_TELEGRAM_POLL_TIMEOUT", "45")

	cfg := config.Load()
	if !cfg.TelegramEnabled {
		t.Fatalf("expected telegram enabled when token present")
	}
	if cfg.TelegramBotToken != "abc123" {
		t.Fatalf("unexpected token: %q", cfg.TelegramBotToken)
	}
	expectIDs := []int64{1001, 1002, -99}
	if !reflect.DeepEqual(cfg.TelegramChatIDs, expectIDs) {
		t.Fatalf("unexpected chat ids: %#v", cfg.TelegramChatIDs)
	}
	if cfg.TelegramPollTimeout != 45 {
		t.Fatalf("expected poll timeout=45, got %d", cfg.TelegramPollTimeout)
	}
}

func TestLoad_TelegramEnabledFlagCanForceDisable(t *testing.T) {
	t.Setenv("AUTO_WORK_TELEGRAM_BOT_TOKEN", "abc123")
	t.Setenv("AUTO_WORK_TELEGRAM_ENABLED", "0")
	t.Setenv("AUTO_WORK_TELEGRAM_POLL_TIMEOUT", "999")

	cfg := config.Load()
	if cfg.TelegramEnabled {
		t.Fatalf("expected telegram disabled when AUTO_WORK_TELEGRAM_ENABLED=0")
	}
	if cfg.TelegramPollTimeout != 30 {
		t.Fatalf("expected invalid timeout fallback to 30, got %d", cfg.TelegramPollTimeout)
	}
}

func TestLoad_MCPTransportDefaultsToHTTP(t *testing.T) {
	t.Setenv("AUTO_WORK_MCP_TRANSPORT", "")
	t.Setenv("AUTO_WORK_MCP_HTTP_URL", "")

	cfg := config.Load()
	if cfg.MCPTransport != "http" {
		t.Fatalf("expected default mcp transport http, got %q", cfg.MCPTransport)
	}
	if cfg.MCPHTTPURL != "http://127.0.0.1:39123/mcp" {
		t.Fatalf("expected default mcp http url, got %q", cfg.MCPHTTPURL)
	}
}

func TestLoad_MCPTransportHTTP(t *testing.T) {
	t.Setenv("AUTO_WORK_MCP_TRANSPORT", "http")
	t.Setenv("AUTO_WORK_MCP_HTTP_URL", "http://127.0.0.1:38080/mcp")

	cfg := config.Load()
	if cfg.MCPTransport != "http" {
		t.Fatalf("expected mcp transport=http, got %q", cfg.MCPTransport)
	}
	if cfg.MCPHTTPURL != "http://127.0.0.1:38080/mcp" {
		t.Fatalf("unexpected mcp http url: %q", cfg.MCPHTTPURL)
	}
}
