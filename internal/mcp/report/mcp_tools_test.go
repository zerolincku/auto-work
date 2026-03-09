package report

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolListResult_DoesNotExposeLegacyTodoAliases(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(ToolListResult())
	if err != nil {
		t.Fatalf("marshal tool list: %v", err)
	}
	if strings.Contains(string(raw), "todo.") {
		t.Fatalf("expected no legacy todo aliases in tool list, got %s", string(raw))
	}
	if !strings.Contains(string(raw), "auto-work.update_task") {
		t.Fatalf("expected update_task exposed in tool list, got %s", string(raw))
	}
	if !strings.Contains(string(raw), "auto-work.delete_task") {
		t.Fatalf("expected delete_task exposed in tool list, got %s", string(raw))
	}
}

func TestHandleToolCall_RejectsLegacyTodoAlias(t *testing.T) {
	t.Parallel()

	_, err := HandleToolCall(context.Background(), nil, json.RawMessage(`{
		"name":"todo.report_result",
		"arguments":{"status":"success","summary":"ok","details":"done"}
	}`))
	if err == nil {
		t.Fatalf("expected legacy todo alias rejected")
	}
	if !strings.Contains(err.Error(), "unsupported tool: todo.report_result") {
		t.Fatalf("unexpected error: %v", err)
	}
}
