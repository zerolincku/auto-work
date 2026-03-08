package runsignal

import "strings"

// ExtractNeedsInputReason returns a non-empty reason when payload indicates
// the model is waiting for human input/confirmation.
func ExtractNeedsInputReason(payload string) string {
	line := strings.TrimSpace(payload)
	if line == "" {
		return ""
	}
	lower := strings.ToLower(line)

	// Codex stream event: command output may contain phrases like
	// "requires user input" from searched source code, which should not
	// be interpreted as the model asking for human interaction.
	if strings.Contains(lower, `"type":"command_execution"`) {
		return ""
	}

	if strings.Contains(line, `"name":"AskUserQuestion"`) || strings.Contains(lower, `"name":"askuserquestion"`) {
		return "AI 发起 AskUserQuestion，等待人工回复"
	}
	if strings.Contains(line, "危险操作检测到") {
		return "检测到危险操作确认，等待人工确认"
	}
	if strings.Contains(line, "请确认是否继续") || strings.Contains(line, "是否继续") || strings.Contains(line, "需要明确的是") {
		return "AI 等待用户确认是否继续"
	}
	if strings.Contains(lower, "please confirm") || strings.Contains(lower, "need your confirmation") {
		return "AI 等待用户确认是否继续"
	}
	if strings.Contains(lower, "awaiting user input") || strings.Contains(lower, "requires user input") {
		return "AI 等待用户输入"
	}
	return ""
}
