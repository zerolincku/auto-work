package telegrambot

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/repository"
	projectservice "auto-work/internal/service/project"
)

func TestParseAddTaskArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		in             string
		wantProjectSel string
		wantTitle      string
		wantDesc       string
		wantProvider   string
		wantSpecified  bool
		wantErr        bool
	}{
		{
			name:         "single project default provider",
			in:           "修复登录超时 | 排查并补测试",
			wantTitle:    "修复登录超时",
			wantDesc:     "排查并补测试",
			wantProvider: "",
		},
		{
			name:           "with explicit project and provider",
			in:             "p:proj-1 | 增加健康检查 | 补 /healthz | codex",
			wantProjectSel: "proj-1",
			wantTitle:      "增加健康检查",
			wantDesc:       "补 /healthz",
			wantProvider:   "codex",
			wantSpecified:  true,
		},
		{
			name:    "invalid format",
			in:      "just-title-only",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotProjectSel, gotTitle, gotDesc, gotProvider, gotSpecified, err := parseAddTaskArgs(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotProjectSel != tc.wantProjectSel {
				t.Fatalf("project selector mismatch: got=%q want=%q", gotProjectSel, tc.wantProjectSel)
			}
			if gotTitle != tc.wantTitle {
				t.Fatalf("title mismatch: got=%q want=%q", gotTitle, tc.wantTitle)
			}
			if gotDesc != tc.wantDesc {
				t.Fatalf("desc mismatch: got=%q want=%q", gotDesc, tc.wantDesc)
			}
			if gotProvider != tc.wantProvider {
				t.Fatalf("provider mismatch: got=%q want=%q", gotProvider, tc.wantProvider)
			}
			if gotSpecified != tc.wantSpecified {
				t.Fatalf("provider specified mismatch: got=%v want=%v", gotSpecified, tc.wantSpecified)
			}
		})
	}
}

func TestHandleCreateTask_SingleProjectAutoSelect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, taskRepo, projectID := setupAddTaskServiceFixture(t, ctx, 1)
	msg := svc.handleCreateTask(ctx, "补齐单测 | 为登录模块补单测")
	if !strings.Contains(msg, "已创建任务") {
		t.Fatalf("unexpected response: %s", msg)
	}

	items, err := taskRepo.List(ctx, repository.TaskListFilter{
		ProjectID: projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(items))
	}
	if items[0].Priority != 100 {
		t.Fatalf("expected priority 100, got %d", items[0].Priority)
	}
	if items[0].Provider != "" {
		t.Fatalf("expected empty provider while pending, got %q", items[0].Provider)
	}
}

func TestHandleCreateTask_ExplicitProjectAppendPriority(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, taskRepo, projectID := setupAddTaskServiceFixture(t, ctx, 2)
	now := time.Now().UTC()
	if err := taskRepo.Create(ctx, &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   projectID,
		Title:       "existing",
		Description: "existing",
		Priority:    120,
		Status:      domain.TaskPending,
		Provider:    "claude",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	msg := svc.handleCreateTask(ctx, "p:"+projectID+" | 新任务 | 通过 tg 添加 | codex")
	if !strings.Contains(msg, "已创建任务") {
		t.Fatalf("unexpected response: %s", msg)
	}

	items, err := taskRepo.List(ctx, repository.TaskListFilter{
		ProjectID: projectID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(items))
	}
	if items[1].Priority != 121 {
		t.Fatalf("expected appended priority 121, got %d", items[1].Priority)
	}
	if items[1].Provider != "" {
		t.Fatalf("expected empty provider while pending, got %q", items[1].Provider)
	}
}

func TestHandleCreateTask_MultiProjectRequiresSelector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc, _, _ := setupAddTaskServiceFixture(t, ctx, 2)
	msg := svc.handleCreateTask(ctx, "未指定项目任务 | 测试描述")
	if !strings.Contains(msg, "存在多个项目，请在命令里指定项目") {
		t.Fatalf("unexpected response: %s", msg)
	}
}

func setupAddTaskServiceFixture(t *testing.T, ctx context.Context, projectCount int) (*Service, *repository.TaskRepository, string) {
	t.Helper()

	sqlDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	projectRepo := repository.NewProjectRepository(sqlDB)
	taskRepo := repository.NewTaskRepository(sqlDB)
	runRepo := repository.NewRunRepository(sqlDB)
	eventRepo := repository.NewRunEventRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)

	var firstProjectID string
	now := time.Now().UTC()
	for i := 1; i <= projectCount; i++ {
		p := &domain.Project{
			ID:              "proj-" + strconv.Itoa(i),
			Name:            "项目" + strconv.Itoa(i),
			Path:            "/tmp/proj-" + strconv.Itoa(i),
			DefaultProvider: "claude",
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := projectRepo.Create(ctx, p); err != nil {
			t.Fatalf("create project: %v", err)
		}
		if firstProjectID == "" {
			firstProjectID = p.ID
		}
	}

	if err := agentRepo.Upsert(ctx, &domain.Agent{
		ID:          "agent-1",
		Name:        "Agent 1",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	svc := &Service{
		taskRepo:    taskRepo,
		runRepo:     runRepo,
		eventRepo:   eventRepo,
		projectRepo: projectRepo,
		projectSvc:  projectservice.NewService(projectRepo),
		errorf:      func(string, ...any) {},
	}
	return svc, taskRepo, firstProjectID
}
