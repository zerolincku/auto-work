package telegrambot

import "testing"

func TestParseLeadingInt(t *testing.T) {
	if got := parseLeadingInt("", 5, 20); got != 5 {
		t.Fatalf("expected fallback 5, got %d", got)
	}
	if got := parseLeadingInt("8", 5, 20); got != 8 {
		t.Fatalf("expected 8, got %d", got)
	}
	if got := parseLeadingInt("999", 5, 20); got != 20 {
		t.Fatalf("expected capped 20, got %d", got)
	}
	if got := parseLeadingInt("xx", 5, 20); got != 5 {
		t.Fatalf("expected fallback 5 for invalid int, got %d", got)
	}
}

func TestCompactEventPayload_SkipsThinkingOnly(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"internal"}]}}`
	out, ok := compactEventPayload("claude.stdout", line)
	if ok {
		t.Fatalf("expected thinking-only event ignored, got %q", out)
	}
}

func TestCompactEventPayload_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"任务已完成"}]}}`
	out, ok := compactEventPayload("claude.stdout", line)
	if !ok {
		t.Fatalf("expected assistant text event")
	}
	if out != "任务已完成" {
		t.Fatalf("unexpected compact text: %q", out)
	}
}

func TestCompactEventPayload_ResultEvent(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done all steps"}`
	out, ok := compactEventPayload("claude.stdout", line)
	if !ok {
		t.Fatalf("expected result event")
	}
	if out != "success: done all steps" {
		t.Fatalf("unexpected compact result: %q", out)
	}
}
