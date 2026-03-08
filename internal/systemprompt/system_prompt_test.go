package systemprompt

import (
	"strings"
	"testing"

	"auto-work/internal/domain"
)

func TestCompose_GlobalAndProject(t *testing.T) {
	t.Parallel()

	got := Compose("global rules", "project rules")
	if !strings.Contains(got, "Global System Prompt:") {
		t.Fatalf("expected global section header, got: %s", got)
	}
	if !strings.Contains(got, "Project System Prompt:") {
		t.Fatalf("expected project section header, got: %s", got)
	}
}

func TestRender_ReplacesTaskPlaceholders(t *testing.T) {
	t.Parallel()

	task := domain.Task{
		ID:          "task-1",
		Title:       "实现登录页",
		Description: "补齐交互",
		ProjectPath: "/tmp/project-x",
	}
	run := domain.Run{ID: "run-1"}

	got := Render("id={{task_id}},run={{run_id}},title={{task_title}},desc={{task_description}},path={{project_path}}", task, run)
	if got != "id=task-1,run=run-1,title=实现登录页,desc=补齐交互,path=/tmp/project-x" {
		t.Fatalf("expected placeholders replaced, got:\n%s", got)
	}
}

func TestRender_DefaultTemplateCommitMessageRule(t *testing.T) {
	t.Parallel()

	task := domain.Task{
		ID:          "task-1",
		Title:       "实现登录页",
		Description: "补齐交互",
		ProjectPath: "/tmp/project-x",
	}
	run := domain.Run{ID: "run-1"}

	got := Render(DefaultGlobalSystemPromptTemplate, task, run)
	if !strings.Contains(got, "Never stage or commit local task/context artifacts or browser automation outputs") {
		t.Fatalf("expected no-commit artifact instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "auto_work_current_task.md, current_task.md, curret_task.md, .playwright-cli/, output/playwright/, playwright-report/, test-results/") {
		t.Fatalf("expected explicit excluded artifact paths, got:\n%s", got)
	}
	if !strings.Contains(got, `git commit -m "<中文提交信息>"`) {
		t.Fatalf("expected dynamic chinese commit message instruction, got:\n%s", got)
	}
	if !strings.Contains(got, `git commit --allow-empty -m "空提交：本次任务无需修改代码"`) {
		t.Fatalf("expected chinese empty commit message instruction, got:\n%s", got)
	}
	if strings.Contains(got, "任务 task-1：实现登录页") {
		t.Fatalf("expected no task-id based fixed commit message, got:\n%s", got)
	}
}
