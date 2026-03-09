package telegrambot

import (
	"context"
	"strings"
	"testing"
)

func TestParseAutoDispatchArgs(t *testing.T) {
	t.Parallel()

	projectSelector, enabled, err := parseAutoDispatchArgs("项目1 | on")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projectSelector != "项目1" || !enabled {
		t.Fatalf("unexpected parsed values: %q %v", projectSelector, enabled)
	}
}

func TestHandleAutoDispatch_EnableByProjectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, projectID := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleAutoDispatch(ctx, projectID+" | on")
	if !strings.Contains(msg, "已开启项目自动派发") {
		t.Fatalf("unexpected response: %s", msg)
	}
	p, err := svc.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if !p.AutoDispatchEnabled {
		t.Fatalf("expected auto dispatch enabled")
	}
}

func TestHandleAutoDispatch_DisableByProjectName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, projectID := setupAddTaskServiceFixture(t, ctx, 1)
	if err := svc.projectRepo.SetAutoDispatchEnabled(ctx, projectID, true); err != nil {
		t.Fatalf("seed auto dispatch enabled: %v", err)
	}

	msg := svc.handleAutoDispatch(ctx, "项目1 | off")
	if !strings.Contains(msg, "已关闭项目自动派发") {
		t.Fatalf("unexpected response: %s", msg)
	}
	p, err := svc.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if p.AutoDispatchEnabled {
		t.Fatalf("expected auto dispatch disabled")
	}
}

func TestHandleAutoDispatch_InvalidUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleAutoDispatch(ctx, "项目1")
	if !strings.Contains(msg, "/autodisp <项目ID或项目名> | on|off") {
		t.Fatalf("unexpected usage response: %s", msg)
	}
}

func TestHandleAutoDispatch_UnknownProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleAutoDispatch(ctx, "not-exists | on")
	if !strings.Contains(msg, "设置自动派发失败") || !strings.Contains(msg, "未找到项目") {
		t.Fatalf("unexpected response: %s", msg)
	}
}
