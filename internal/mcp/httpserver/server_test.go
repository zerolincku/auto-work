package httpserver

import (
	"encoding/json"
	"strings"
	"testing"
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
