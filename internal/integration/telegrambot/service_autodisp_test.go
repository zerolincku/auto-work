package telegrambot

import (
	"context"
	"strings"
	"testing"
)

func TestHandleSetAutoDispatch_EnableByProjectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, projectID := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleSetAutoDispatch(ctx, projectID, true)
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

func TestHandleSetAutoDispatch_DisableByProjectName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, projectID := setupAddTaskServiceFixture(t, ctx, 1)
	if err := svc.projectRepo.SetAutoDispatchEnabled(ctx, projectID, true); err != nil {
		t.Fatalf("seed auto dispatch enabled: %v", err)
	}

	msg := svc.handleSetAutoDispatch(ctx, "项目1", false)
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

func TestHandleSetAutoDispatch_MissingSelector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)

	onMsg := svc.handleSetAutoDispatch(ctx, "", true)
	if !strings.Contains(onMsg, "/autodisp_on <项目ID或项目名>") {
		t.Fatalf("unexpected on usage response: %s", onMsg)
	}
	offMsg := svc.handleSetAutoDispatch(ctx, "", false)
	if !strings.Contains(offMsg, "/autodisp_off <项目ID或项目名>") {
		t.Fatalf("unexpected off usage response: %s", offMsg)
	}
}

func TestHandleSetAutoDispatch_UnknownProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 1)

	msg := svc.handleSetAutoDispatch(ctx, "not-exists", true)
	if !strings.Contains(msg, "设置自动派发失败") || !strings.Contains(msg, "未找到项目") {
		t.Fatalf("unexpected response: %s", msg)
	}
}
