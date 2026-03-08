package runsignal

import "testing"

func TestExtractNeedsInputReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		line    string
		matched bool
	}{
		{
			name:    "ask-user-question-tool",
			line:    `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"AskUserQuestion"}]}}`,
			matched: true,
		},
		{
			name:    "danger-confirm",
			line:    `⚠️ 危险操作检测到！请确认是否继续`,
			matched: true,
		},
		{
			name:    "english-confirm",
			line:    `Please confirm before I continue`,
			matched: true,
		},
		{
			name:    "normal-result",
			line:    `{"type":"result","subtype":"success","result":"任务已完成"}`,
			matched: false,
		},
		{
			name:    "codex-command-output-contains-requires-user-input",
			line:    `{"type":"item.completed","item":{"id":"item_11","type":"command_execution","aggregated_output":"internal/runsignal/needs_input.go:26:\tif strings.Contains(lower, \"awaiting user input\") || strings.Contains(lower, \"requires user input\") {"}}`,
			matched: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractNeedsInputReason(tc.line)
			if tc.matched && got == "" {
				t.Fatalf("expected matched reason, got empty")
			}
			if !tc.matched && got != "" {
				t.Fatalf("expected empty reason, got %q", got)
			}
		})
	}
}
