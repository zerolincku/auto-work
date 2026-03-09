package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"auto-work/internal/domain"
	"auto-work/internal/systemprompt"
)

func TestBuildPrompt_ContainsCodeBasedDoneShortCircuit(t *testing.T) {
	t.Parallel()

	task := domain.Task{
		ID:           "task-123",
		ProjectID:    "project-xyz",
		ProjectName:  "示例项目",
		Title:        "实现登录页",
		Description:  "补齐登录页交互并联调接口",
		ProjectPath:  "/tmp/project-x",
		SystemPrompt: systemprompt.DefaultGlobalSystemPromptTemplate,
	}
	run := domain.Run{
		ID: "run-456",
	}

	prompt := buildPrompt(task, run)

	assertContains(t, prompt, "task_id: task-123")
	assertContains(t, prompt, "run_id: run-456")
	assertContains(t, prompt, "project_id: project-xyz")
	assertContains(t, prompt, "project_name: 示例项目")
	assertContains(t, prompt, "auto_work_current_task.md")
	assertContains(t, prompt, "System Prompt:")
	assertContains(t, prompt, "create or update project-root file auto_work_current_task.md")
	assertContains(t, prompt, "Inspect actual repository files/code/tests related to this task (not task status metadata)")
	assertContains(t, prompt, "If the task is already satisfied in code/files, do not repeat changes; update auto_work_current_task.md status=success")
	assertContains(t, prompt, "Do not ask user questions. Do not wait for user confirmation.")
	assertContains(t, prompt, "If required information is truly missing and task cannot be completed reliably, call auto-work.report_result with status=failed")
	assertContains(t, prompt, "auto-work.update_task")
	assertContains(t, prompt, "auto-work.delete_task")
	assertContains(t, prompt, "Never stage or commit local task/context artifacts or browser automation outputs")
	assertContains(t, prompt, "exclude auto_work_current_task.md, current_task.md, curret_task.md, .playwright-cli/, output/playwright/, playwright-report/, test-results/")
	assertContains(t, prompt, "Before calling auto-work.report_result, you must run git commit for this task")
	assertContains(t, prompt, "remove any staged task-context or Playwright-generated artifacts")
	assertContains(t, prompt, "write a concise Chinese commit message based on the actual code/file changes")
	assertContains(t, prompt, "do not include task_id/run_id or fixed prefixes")
	assertContains(t, prompt, "git commit -m \"<中文提交信息>\"")
	assertContains(t, prompt, "git commit --allow-empty -m \"空提交：本次任务无需修改代码\"")
	assertContains(t, prompt, "Put git commit hash into report details.")
	assertContains(t, prompt, "auto-work.report_result fields: status(success|failed), summary, details.")
	assertContains(t, prompt, "Before exit, call MCP tool auto-work.report_result exactly once on server auto-work.")
}

func TestBuildPrompt_IncludesProjectSystemPrompt(t *testing.T) {
	t.Parallel()

	task := domain.Task{
		ID:           "task-abc",
		Title:        "修复设置页",
		Description:  "将模型配置移到项目设置",
		ProjectPath:  "/tmp/project-y",
		SystemPrompt: "你是项目级助手，先阅读代码再修改。",
	}
	run := domain.Run{
		ID: "run-def",
	}

	prompt := buildPrompt(task, run)

	assertContains(t, prompt, "System Prompt:")
	assertContains(t, prompt, "你是项目级助手，先阅读代码再修改。")
}

func TestBuildMCPConfig(t *testing.T) {
	t.Parallel()

	r := New(Options{
		MCPHTTPURL: "http://127.0.0.1:38080/mcp",
	})
	cfgJSON, err := r.buildMCPConfig(domain.Run{ID: "run-1"}, domain.Task{ID: "task-1"})
	if err != nil {
		t.Fatalf("build mcp config: %v", err)
	}

	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}
	server, ok := cfg.MCPServers["auto-work"]
	if !ok {
		t.Fatalf("auto-work mcp server not found: %#v", cfg.MCPServers)
	}
	typ, _ := server["type"].(string)
	if typ != "http" {
		t.Fatalf("expected type=http, got %q", typ)
	}
	url, _ := server["url"].(string)
	if url != "http://127.0.0.1:38080/mcp?run_id=run-1&task_id=task-1" {
		t.Fatalf("unexpected mcp url: %q", url)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected prompt contains %q, got:\n%s", want, got)
	}
}
