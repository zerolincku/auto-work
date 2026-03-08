package codex

import (
	"strings"
	"testing"

	"auto-work/internal/domain"
)

func TestAppendMCPConfigOverrides_DefaultsToHTTP(t *testing.T) {
	t.Parallel()

	r := New(Options{
		MCPHTTPURL: "http://127.0.0.1:38080/mcp",
	})
	args := []string{}
	err := r.appendMCPConfigOverrides(&args, domain.Run{ID: "run-1"}, domain.Task{ID: "task-1"})
	if err != nil {
		t.Fatalf("append overrides: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("unexpected args len: %d", len(args))
	}
	if args[0] != "-c" {
		t.Fatalf("expected -c at 0, got %q", args[0])
	}
	if strings.Contains(args[1], `mcp_servers."auto-work".`) {
		t.Fatalf("server name should not include quotes: %q", args[1])
	}
	if !strings.HasPrefix(args[1], "mcp_servers.auto-work.url=") {
		t.Fatalf("missing url override, got %q", args[1])
	}
	if !strings.Contains(args[1], "run_id=run-1") || !strings.Contains(args[1], "task_id=task-1") {
		t.Fatalf("url override missing run/task query: %q", args[1])
	}
}

func TestAppendMCPConfigOverrides_HTTPTransport(t *testing.T) {
	t.Parallel()

	r := New(Options{
		MCPTransport: "http",
		MCPHTTPURL:   "http://127.0.0.1:38080/mcp",
	})
	args := []string{}
	err := r.appendMCPConfigOverrides(&args, domain.Run{ID: "run-1"}, domain.Task{ID: "task-1"})
	if err != nil {
		t.Fatalf("append overrides: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("unexpected args len: %d", len(args))
	}
	if args[0] != "-c" {
		t.Fatalf("expected -c at 0, got %q", args[0])
	}
	if !strings.HasPrefix(args[1], "mcp_servers.auto-work.url=") {
		t.Fatalf("missing url override, got %q", args[1])
	}
	if !strings.Contains(args[1], "run_id=run-1") || !strings.Contains(args[1], "task_id=task-1") {
		t.Fatalf("url override missing run/task query: %q", args[1])
	}
}

func TestBuildPrompt_IncludesProjectSystemPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildPrompt(domain.Task{
		ID:           "task-1",
		ProjectID:    "project-z",
		ProjectName:  "项目Z",
		Title:        "更新项目配置",
		Description:  "新增项目级系统提示词",
		ProjectPath:  "/tmp/project-z",
		SystemPrompt: "你是项目专属助手，请遵循项目规范。",
	}, domain.Run{
		ID: "run-1",
	})

	if !strings.Contains(prompt, "System Prompt:") {
		t.Fatalf("expected prompt contains system prompt header, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "project_id: project-z") {
		t.Fatalf("expected prompt contains project id, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "project_name: 项目Z") {
		t.Fatalf("expected prompt contains project name, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "auto_work_current_task.md") {
		t.Fatalf("expected prompt contains auto_work_current_task.md instruction, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "你是项目专属助手，请遵循项目规范。") {
		t.Fatalf("expected prompt contains project system prompt content, got:\n%s", prompt)
	}
}
