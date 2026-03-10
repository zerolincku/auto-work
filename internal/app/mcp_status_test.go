package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"auto-work/internal/config"
)

const (
	testMCPHTTPURL   = "http://127.0.0.1:39123/mcp"
	fakeClaudeBinary = "fake-claude"
	fakeCodexBinary  = "fake-codex"
)

var errExitStatus1 = errors.New("exit status 1")

type mcpCommandStep struct {
	wantTimeout time.Duration
	wantName    string
	wantArgs    []string
	output      string
	err         error
}

type mcpCommandRecorder struct {
	t     *testing.T
	steps []mcpCommandStep
	calls int
}

func newMCPStatusTestApp(t *testing.T, steps ...mcpCommandStep) (*App, *mcpCommandRecorder) {
	t.Helper()
	recorder := &mcpCommandRecorder{t: t, steps: steps}
	application := &App{
		cfg: config.Config{
			ClaudeBinary:      fakeClaudeBinary,
			CodexBinary:       fakeCodexBinary,
			EnableMCPCallback: true,
			MCPHTTPURL:        testMCPHTTPURL,
		},
		commandRunner:        recorder.run,
		mcpProvisionFailures: make(map[string]mcpProvisionFailure),
	}
	return application, recorder
}

func (r *mcpCommandRecorder) run(_ context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	r.t.Helper()
	if r.calls >= len(r.steps) {
		r.t.Fatalf("unexpected command #%d: %s %v", r.calls+1, name, args)
	}
	step := r.steps[r.calls]
	r.calls++
	if step.wantTimeout != 0 && timeout != step.wantTimeout {
		r.t.Fatalf("command #%d timeout = %s, want %s", r.calls, timeout, step.wantTimeout)
	}
	if step.wantName != "" && name != step.wantName {
		r.t.Fatalf("command #%d name = %q, want %q", r.calls, name, step.wantName)
	}
	if step.wantArgs != nil && !reflect.DeepEqual(args, step.wantArgs) {
		r.t.Fatalf("command #%d args = %v, want %v", r.calls, args, step.wantArgs)
	}
	return step.output, step.err
}

func (r *mcpCommandRecorder) assertDone() {
	r.t.Helper()
	if r.calls != len(r.steps) {
		r.t.Fatalf("command calls = %d, want %d", r.calls, len(r.steps))
	}
}

func assertMCPStatus(t *testing.T, status *MCPStatusView, wantState string, wantMessageParts ...string) {
	t.Helper()
	if status == nil {
		t.Fatal("status is nil")
	}
	if status.State != wantState {
		t.Fatalf("status state = %q, want %q (message=%q)", status.State, wantState, status.Message)
	}
	for _, part := range wantMessageParts {
		if !strings.Contains(status.Message, part) {
			t.Fatalf("status message = %q, want substring %q", status.Message, part)
		}
	}
	if status.UpdatedAt == nil {
		t.Fatal("status updated_at is nil")
	}
}

func TestMCPStatus_ClaudeAutoAddTimeoutIncludesReason(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs:    []string{"mcp", "get", autoWorkMCPServerName},
			output:      "No MCP server found with name: auto-work",
			err:         errExitStatus1,
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeClaudeBinary,
			wantArgs: []string{
				"mcp", "add", "--scope", "user", "--transport", "http", autoWorkMCPServerName, testMCPHTTPURL,
			},
			output: "context deadline exceeded while adding Claude MCP",
			err:    context.DeadlineExceeded,
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureClaudeMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("ensure claude mcp status: %v", err)
	}
	assertMCPStatus(t, status, "failed", "context deadline exceeded")
}

func TestMCPStatus_CodexAutoAddTimeoutIncludesReason(t *testing.T) {
	t.Parallel()
	application, recorder := newMCPStatusTestApp(t,
		mcpCommandStep{
			wantTimeout: mcpCheckTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "list", "--json"},
			output:      "[]",
		},
		mcpCommandStep{
			wantTimeout: mcpAddTimeout,
			wantName:    fakeCodexBinary,
			wantArgs:    []string{"mcp", "add", "--url", testMCPHTTPURL, autoWorkMCPServerName},
			output:      "context deadline exceeded while adding Codex MCP",
			err:         context.DeadlineExceeded,
		},
	)
	defer recorder.assertDone()

	status, err := application.ensureCodexMCPStatus(context.Background())
	if err != nil {
		t.Fatalf("ensure codex mcp status: %v", err)
	}
	assertMCPStatus(t, status, "failed", "context deadline exceeded")
}
