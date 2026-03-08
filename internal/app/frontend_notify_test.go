package app

import (
	"reflect"
	"strings"
	"testing"

	"auto-work/internal/domain"
)

func TestIsFrontendRelatedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "frontend folder tsx",
			path: "frontend/src/App.tsx",
			want: true,
		},
		{
			name: "vue component",
			path: "src/components/user-card.vue",
			want: true,
		},
		{
			name: "frontend config",
			path: "vite.config.ts",
			want: true,
		},
		{
			name: "backend go file",
			path: "internal/app/app.go",
			want: false,
		},
		{
			name: "docs markdown",
			path: "docs/readme.md",
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isFrontendRelatedPath(tc.path)
			if got != tc.want {
				t.Fatalf("isFrontendRelatedPath(%q)=%v want=%v", tc.path, got, tc.want)
			}
		})
	}
}

func TestFrontendChangesSince(t *testing.T) {
	t.Parallel()

	before := map[string]struct{}{
		"frontend/src/App.tsx": {},
		"README.md":            {},
	}
	after := map[string]struct{}{
		"frontend/src/App.tsx":    {},
		"frontend/src/New.tsx":    {},
		"frontend/src/style.css":  {},
		"internal/service/app.go": {},
	}
	got := frontendChangesSince(before, after)
	want := []string{
		"frontend/src/New.tsx",
		"frontend/src/style.css",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frontendChangesSince mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestExtractAIScreenshotRefs(t *testing.T) {
	t.Parallel()

	refs := extractAIScreenshotRefs("/repo", `
Screenshots:
- ./output/playwright/home.png
- https://img.example.com/shot-1.png
- [settings](/tmp/settings.jpg)
`)

	if len(refs.Ordered) < 3 {
		t.Fatalf("expected at least 3 refs, got %#v", refs.Ordered)
	}
	hasRepoPath := false
	for _, ref := range refs.Ordered {
		if ref == "/repo/output/playwright/home.png" {
			hasRepoPath = true
			break
		}
	}
	if !hasRepoPath {
		t.Fatalf("expected repo relative screenshot ref, got %#v", refs.Ordered)
	}
	hasURL := false
	for _, u := range refs.URLs {
		if u == "https://img.example.com/shot-1.png" {
			hasURL = true
			break
		}
	}
	if !hasURL {
		t.Fatalf("expected url ref in %#v", refs.URLs)
	}
	hasLocal := false
	for _, p := range refs.LocalPaths {
		if p == "/tmp/settings.jpg" {
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		t.Fatalf("expected local ref in %#v", refs.LocalPaths)
	}
}

func TestAppendFrontendScreenshotPromptHint_Idempotent(t *testing.T) {
	t.Parallel()

	base := "rule-1"
	once := appendFrontendScreenshotPromptHint(base)
	twice := appendFrontendScreenshotPromptHint(once)
	if once != twice {
		t.Fatalf("expected idempotent append")
	}
	if !strings.Contains(once, "section named \"Screenshots\"") {
		t.Fatalf("missing screenshot hint: %s", once)
	}
}

func TestDecideScreenshotNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refs     screenshotRefs
		wantSend bool
	}{
		{
			name: "send when refs exist",
			refs: screenshotRefs{
				Ordered: []string{"/repo/output/playwright/home.png"},
			},
			wantSend: true,
		},
		{
			name:     "do not send when refs missing",
			refs:     screenshotRefs{},
			wantSend: false,
		},
		{
			name: "do not send when only local paths is empty ordered",
			refs: screenshotRefs{
				LocalPaths: []string{"/repo/output/playwright/home.png"},
			},
			wantSend: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotSend := decideScreenshotNotify(tc.refs)
			if gotSend != tc.wantSend {
				t.Fatalf("decideScreenshotNotify()=%v, want=%v", gotSend, tc.wantSend)
			}
		})
	}
}

func TestBuildFrontendScreenshotCaption_OnlyTaskTitle(t *testing.T) {
	t.Parallel()

	task := &domain.Task{
		Title: "UX-P0 建立设计令牌与 8pt 栅格规范",
	}
	caption := buildFrontendScreenshotCaption(task, nil, []string{"frontend/src/App.tsx"})
	if caption != task.Title {
		t.Fatalf("unexpected caption: %q", caption)
	}
	if strings.Contains(caption, "Run:") || strings.Contains(caption, "前端改动") {
		t.Fatalf("caption should only include task title, got: %q", caption)
	}
}

func TestBuildFrontendScreenshotCaption_Fallback(t *testing.T) {
	t.Parallel()

	caption := buildFrontendScreenshotCaption(&domain.Task{Title: "  "}, nil, nil)
	if caption != "未命名任务" {
		t.Fatalf("unexpected fallback caption: %q", caption)
	}
}
