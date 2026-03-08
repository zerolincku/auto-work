package telegrambot

import (
	"encoding/json"
	"fmt"
	"strings"
)

type streamEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	Message struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content"`
	} `json:"message"`
}

func compactEventPayload(kind, payload string) (string, bool) {
	p := strings.TrimSpace(payload)
	if p == "" {
		return "", false
	}

	if strings.HasPrefix(kind, "system.") || kind == "mcp.report_result.applied" || strings.HasPrefix(kind, "mcp.") {
		return trimText(p, 240), true
	}
	if !strings.HasPrefix(kind, "claude.") {
		return trimText(p, 240), true
	}
	if !strings.HasPrefix(p, "{") {
		return trimText(p, 220), true
	}

	var ev streamEnvelope
	if err := json.Unmarshal([]byte(p), &ev); err != nil {
		return trimText(p, 220), true
	}

	switch strings.TrimSpace(ev.Type) {
	case "assistant":
		text := extractAssistantText(ev)
		if text == "" {
			return "", false
		}
		return trimText(text, 220), true
	case "result":
		summary := strings.TrimSpace(ev.Subtype)
		if summary == "" {
			summary = "result"
		}
		if strings.TrimSpace(ev.Result) != "" {
			return fmt.Sprintf("%s: %s", summary, trimText(ev.Result, 220)), true
		}
		return summary, true
	case "system":
		sub := strings.TrimSpace(ev.Subtype)
		if sub == "" {
			sub = "system"
		}
		return fmt.Sprintf("%s: %s", sub, trimText(p, 160)), true
	default:
		return trimText(p, 220), true
	}
}

func extractAssistantText(ev streamEnvelope) string {
	parts := make([]string, 0, len(ev.Message.Content))
	for _, item := range ev.Message.Content {
		if strings.EqualFold(strings.TrimSpace(item.Type), "text") {
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func trimText(in string, maxLen int) string {
	val := strings.TrimSpace(in)
	if maxLen <= 0 {
		return ""
	}
	r := []rune(val)
	if len(r) <= maxLen {
		return val
	}
	return strings.TrimSpace(string(r[:maxLen])) + "...(truncated)"
}
