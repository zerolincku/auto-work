package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"auto-work/internal/domain"
)

type frontendRunBaseline struct {
	projectPath  string
	changedFiles map[string]struct{}
}

var frontendMarkers = []string{
	"/frontend/",
	"/src/",
	"/web/",
	"/ui/",
	"/pages/",
	"/components/",
	"/assets/",
	"/styles/",
	"/public/",
	"/views/",
	"/layouts/",
}

var frontendScriptConfigFiles = map[string]struct{}{
	"vite.config.js":         {},
	"vite.config.ts":         {},
	"next.config.js":         {},
	"next.config.mjs":        {},
	"nuxt.config.js":         {},
	"nuxt.config.ts":         {},
	"tailwind.config.js":     {},
	"tailwind.config.ts":     {},
	"postcss.config.js":      {},
	"postcss.config.cjs":     {},
	"postcss.config.mjs":     {},
	"svelte.config.js":       {},
	"astro.config.mjs":       {},
	"webpack.config.js":      {},
	"webpack.config.ts":      {},
	"rollup.config.js":       {},
	"rollup.config.mjs":      {},
	"rollup.config.ts":       {},
	"esbuild.config.js":      {},
	"esbuild.config.mjs":     {},
	"parcel.config.mjs":      {},
	"angular.json":           {},
	"tsconfig.app.json":      {},
	"tsconfig.web.json":      {},
	"tsconfig.frontend.json": {},
}

var frontendExtensions = map[string]struct{}{
	".tsx":    {},
	".jsx":    {},
	".vue":    {},
	".svelte": {},
	".astro":  {},
	".css":    {},
	".scss":   {},
	".sass":   {},
	".less":   {},
	".styl":   {},
	".html":   {},
	".htm":    {},
}

var screenshotMarkdownRefPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func (a *App) recordRunFrontendBaseline(runID, projectPath string) {
	runID = strings.TrimSpace(runID)
	projectPath = strings.TrimSpace(projectPath)
	if runID == "" || projectPath == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	changedFiles, err := listGitChangedFiles(ctx, projectPath)
	if err != nil {
		return
	}

	a.frontendChangeMu.Lock()
	if a.runFrontendBaseline == nil {
		a.runFrontendBaseline = make(map[string]frontendRunBaseline)
	}
	a.runFrontendBaseline[runID] = frontendRunBaseline{
		projectPath:  projectPath,
		changedFiles: changedFiles,
	}
	a.frontendChangeMu.Unlock()
}

func (a *App) discardRunFrontendBaseline(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	a.frontendChangeMu.Lock()
	delete(a.runFrontendBaseline, runID)
	a.frontendChangeMu.Unlock()
}

func (a *App) consumeRunFrontendChanges(ctx context.Context, runID, projectID string) []string {
	baseline, ok := a.popRunFrontendBaseline(runID)
	if !ok {
		return nil
	}

	projectPath := strings.TrimSpace(baseline.projectPath)
	if projectPath == "" {
		projectPath = a.lookupProjectPath(ctx, projectID)
	}
	if projectPath == "" {
		return nil
	}

	gitCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	after, err := listGitChangedFiles(gitCtx, projectPath)
	if err != nil {
		return nil
	}
	return frontendChangesSince(baseline.changedFiles, after)
}

func (a *App) popRunFrontendBaseline(runID string) (frontendRunBaseline, bool) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return frontendRunBaseline{}, false
	}
	a.frontendChangeMu.Lock()
	defer a.frontendChangeMu.Unlock()
	item, ok := a.runFrontendBaseline[runID]
	if ok {
		delete(a.runFrontendBaseline, runID)
	}
	return item, ok
}

func (a *App) lookupProjectPath(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	project, err := a.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(project.Path)
}

func (a *App) projectFrontendScreenshotReportEnabled(ctx context.Context, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	project, err := a.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return false
	}
	return project.FrontendScreenshotReportEnabled
}

func listGitChangedFiles(ctx context.Context, projectPath string) (map[string]struct{}, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, errors.New("project path is empty")
	}

	out := make(map[string]struct{})
	cmdArgs := [][]string{
		{"diff", "--name-only"},
		{"diff", "--name-only", "--cached"},
		{"ls-files", "--others", "--exclude-standard"},
	}
	for _, args := range cmdArgs {
		items, err := runGitFileList(ctx, projectPath, args...)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			normalized := normalizeGitFilePath(item)
			if normalized == "" {
				continue
			}
			out[normalized] = struct{}{}
		}
	}
	return out, nil
}

func runGitFileList(ctx context.Context, projectPath string, args ...string) ([]string, error) {
	cmdArgs := make([]string, 0, 2+len(args))
	cmdArgs = append(cmdArgs, "-C", strings.TrimSpace(projectPath))
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), msg)
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		item := strings.TrimSpace(line)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeGitFilePath(path string) string {
	path = strings.TrimSpace(filepath.ToSlash(path))
	path = strings.TrimPrefix(path, "./")
	for strings.HasPrefix(path, "/") {
		path = strings.TrimPrefix(path, "/")
	}
	if path == "." {
		return ""
	}
	return path
}

func frontendChangesSince(before, after map[string]struct{}) []string {
	if len(after) == 0 {
		return nil
	}
	out := make([]string, 0, len(after))
	for file := range after {
		if !isFrontendRelatedPath(file) {
			continue
		}
		if before != nil {
			if _, exists := before[file]; exists {
				continue
			}
		}
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func isFrontendRelatedPath(path string) bool {
	p := strings.ToLower(normalizeGitFilePath(path))
	if p == "" {
		return false
	}
	if _, ok := frontendScriptConfigFiles[filepath.Base(p)]; ok {
		return true
	}
	if strings.HasPrefix(p, "frontend/") || strings.Contains("/"+p, "/frontend/") {
		return true
	}
	ext := filepath.Ext(p)
	if _, ok := frontendExtensions[ext]; ok {
		return true
	}
	if ext == ".ts" || ext == ".js" || ext == ".mjs" || ext == ".cjs" {
		withSlash := "/" + p
		for _, marker := range frontendMarkers {
			if strings.Contains(withSlash, marker) {
				return true
			}
		}
	}
	return false
}

type screenshotRefs struct {
	Ordered    []string
	LocalPaths []string
	URLs       []string
}

// decideScreenshotNotify determines behavior on run-finished notification:
// send screenshots whenever report details contain valid screenshot refs.
func decideScreenshotNotify(refs screenshotRefs) bool {
	return len(refs.Ordered) > 0
}

func extractAIScreenshotRefs(projectPath string, texts ...string) screenshotRefs {
	projectPath = strings.TrimSpace(projectPath)

	ordered := make([]string, 0, 4)
	localPaths := make([]string, 0, 4)
	urls := make([]string, 0, 2)
	seenOrdered := make(map[string]struct{}, 8)
	seenLocal := make(map[string]struct{}, 8)
	seenURL := make(map[string]struct{}, 8)

	for _, text := range texts {
		for _, cand := range extractScreenshotCandidates(text) {
			ref, kind, ok := normalizeScreenshotRef(cand, projectPath)
			if !ok {
				continue
			}
			if _, ok := seenOrdered[ref]; !ok {
				seenOrdered[ref] = struct{}{}
				ordered = append(ordered, ref)
			}
			switch kind {
			case "local":
				if _, ok := seenLocal[ref]; !ok {
					seenLocal[ref] = struct{}{}
					localPaths = append(localPaths, ref)
				}
			case "url":
				if _, ok := seenURL[ref]; !ok {
					seenURL[ref] = struct{}{}
					urls = append(urls, ref)
				}
			}
		}
	}
	return screenshotRefs{
		Ordered:    ordered,
		LocalPaths: localPaths,
		URLs:       urls,
	}
}

func extractScreenshotCandidates(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	out := make([]string, 0, 8)
	for _, m := range screenshotMarkdownRefPattern.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			out = append(out, m[1])
		}
	}
	for _, field := range strings.Fields(text) {
		out = append(out, field)
	}
	return out
}

func normalizeScreenshotRef(raw, projectPath string) (normalized string, kind string, ok bool) {
	ref := trimScreenshotToken(raw)
	if ref == "" {
		return "", "", false
	}

	lower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		if !looksLikeImageRef(ref) {
			return "", "", false
		}
		return ref, "url", true
	case strings.HasPrefix(lower, "file://"):
		ref = strings.TrimPrefix(ref, "file://")
		ref = strings.TrimPrefix(ref, "localhost/")
		ref = "/" + strings.TrimLeft(ref, "/")
	}

	if !looksLikeImageRef(ref) {
		return "", "", false
	}

	switch {
	case filepath.IsAbs(ref):
		return filepath.Clean(ref), "local", true
	case projectPath != "":
		return filepath.Clean(filepath.Join(projectPath, ref)), "local", true
	default:
		return filepath.Clean(ref), "local", true
	}
}

func trimScreenshotToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "`'\"")
	token = strings.Trim(token, "[](){}<>")
	token = strings.Trim(token, ",;")
	token = strings.TrimPrefix(token, "-")
	token = strings.TrimPrefix(token, "*")
	return strings.TrimSpace(token)
}

func looksLikeImageRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	base := ref
	if idx := strings.IndexAny(base, "?#"); idx >= 0 {
		base = base[:idx]
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func existingImageFiles(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		if !looksLikeImageRef(p) {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func buildFrontendScreenshotCaption(task *domain.Task, _ *domain.Run, _ []string) string {
	title := "未命名任务"
	if task != nil {
		if t := strings.TrimSpace(task.Title); t != "" {
			title = trimTextForNotify(t, 180)
		}
	}
	return title
}

func buildFrontendScreenshotPromptHint() string {
	return strings.TrimSpace(`
16) If you changed frontend/UI files or user-facing behavior, you must run the frontend app/pages, verify changed pages, capture screenshots for each affected page/flow, and include screenshot file paths or URLs in auto-work.report_result details.
17) Screenshot count must follow changed pages/flows; do not use a fixed count.
18) In report details, add a section named "Screenshots" and list one address per line.`)
}

func appendFrontendScreenshotPromptHint(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	hint := buildFrontendScreenshotPromptHint()
	if hint == "" {
		return prompt
	}
	if prompt == "" {
		return hint
	}
	if strings.Contains(prompt, "section named \"Screenshots\"") {
		return prompt
	}
	return prompt + "\n\n" + hint
}
