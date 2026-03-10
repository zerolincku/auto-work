package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"auto-work/internal/domain"
	mcpcallback "auto-work/internal/mcp/callback"
	"auto-work/internal/runner"
	"auto-work/internal/systemprompt"
)

var ErrRunNotFound = errors.New("run not found in codex runner")

type Options struct {
	Binary            string
	Model             string
	Workdir           string
	DebugDir          string
	ExtraArgs         []string
	EnableMCPCallback bool
	MCPHTTPURL        string
	OnLine            func(runID, stream, line string)
	OnExit            func(runID string, exitCode int, runErr error)
}

type processEntry struct {
	cmd       *exec.Cmd
	startedAt time.Time
}

type Runner struct {
	opts Options
	mu   sync.RWMutex
	proc map[string]processEntry
}

func New(opts Options) *Runner {
	if strings.TrimSpace(opts.Binary) == "" {
		opts.Binary = "codex"
	}
	if strings.TrimSpace(opts.DebugDir) == "" {
		opts.DebugDir = filepath.Join(os.TempDir(), "auto-work", "codex-debug")
	}
	return &Runner{
		opts: opts,
		proc: make(map[string]processEntry),
	}
}

func (r *Runner) Start(ctx context.Context, run domain.Run, task domain.Task, agent domain.Agent) (int, error) {
	prompt := buildPrompt(task, run)
	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
	}

	workdir := strings.TrimSpace(task.ProjectPath)
	if workdir == "" {
		workdir = strings.TrimSpace(r.opts.Workdir)
	}
	if workdir != "" {
		args = append(args, "-C", workdir)
	}

	model := strings.TrimSpace(task.Model)
	if model == "" {
		model = strings.TrimSpace(r.opts.Model)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if r.opts.EnableMCPCallback {
		if err := r.appendMCPConfigOverrides(&args, run, task); err != nil {
			return 0, err
		}
	}

	args = append(args, r.opts.ExtraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, r.opts.Binary, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	r.mu.Lock()
	r.proc[run.ID] = processEntry{
		cmd:       cmd,
		startedAt: time.Now().UTC(),
	}
	r.mu.Unlock()

	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go r.consumeOutput(run.ID, "stdout", stdout, &outputWG)
	go r.consumeOutput(run.ID, "stderr", stderr, &outputWG)
	go r.waitExit(run.ID, cmd, &outputWG)

	return cmd.Process.Pid, nil
}

func (r *Runner) Stop(ctx context.Context, runID string) error {
	r.mu.RLock()
	entry, ok := r.proc[runID]
	r.mu.RUnlock()
	if !ok {
		return ErrRunNotFound
	}
	if entry.cmd.Process == nil {
		return ErrRunNotFound
	}

	if err := entry.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

func (r *Runner) Probe(ctx context.Context, runID string) (runner.RunHealth, error) {
	r.mu.RLock()
	entry, ok := r.proc[runID]
	r.mu.RUnlock()
	if !ok {
		return runner.RunHealth{Alive: false, HeartbeatOK: false, Message: "not running"}, nil
	}

	select {
	case <-ctx.Done():
		return runner.RunHealth{}, ctx.Err()
	default:
	}

	alive := entry.cmd.ProcessState == nil
	return runner.RunHealth{
		Alive:       alive,
		HeartbeatOK: alive,
		Message:     fmt.Sprintf("started_at=%s", entry.startedAt.Format(time.RFC3339)),
	}, nil
}

func (r *Runner) consumeOutput(runID, stream string, reader io.ReadCloser, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	defer reader.Close()

	const readBufSize = 32 * 1024
	const maxCarryBytes = 4 * 1024 * 1024

	emit := func(raw []byte) {
		if r.opts.OnLine == nil {
			return
		}
		line := strings.TrimRight(string(raw), "\r")
		r.opts.OnLine(runID, stream, line)
	}

	buf := make([]byte, readBufSize)
	carry := make([]byte, 0, readBufSize)

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			for len(chunk) > 0 {
				idx := bytes.IndexByte(chunk, '\n')
				if idx < 0 {
					carry = append(carry, chunk...)
					if len(carry) > maxCarryBytes {
						emit(carry)
						carry = carry[:0]
					}
					break
				}
				line := append(carry, chunk[:idx]...)
				emit(line)
				carry = carry[:0]
				chunk = chunk[idx+1:]
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(carry) > 0 {
					emit(carry)
				}
				return
			}
			emit([]byte(fmt.Sprintf("[auto-work] output stream read error: %v", err)))
			return
		}
	}
}

func (r *Runner) waitExit(runID string, cmd *exec.Cmd, outputWG *sync.WaitGroup) {
	err := cmd.Wait()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if outputWG != nil {
		outputWG.Wait()
	}

	r.mu.Lock()
	delete(r.proc, runID)
	r.mu.Unlock()

	if r.opts.OnExit != nil {
		r.opts.OnExit(runID, exitCode, err)
	}
}

func (r *Runner) appendMCPConfigOverrides(args *[]string, run domain.Run, task domain.Task) error {
	url, err := mcpcallback.BuildRunScopedURL(r.opts.MCPHTTPURL, run.ID, task.ID)
	if err != nil {
		return fmt.Errorf("mcp http config invalid: %w", err)
	}
	overrides := []string{fmt.Sprintf(`mcp_servers.auto-work.url=%s`, strconv.Quote(url))}
	for _, item := range overrides {
		*args = append(*args, "-c", item)
	}
	return nil
}

func buildPrompt(task domain.Task, run domain.Run) string {
	projectPath := strings.TrimSpace(task.ProjectPath)
	if projectPath == "" {
		projectPath = "(not set)"
	}
	projectName := strings.TrimSpace(task.ProjectName)
	if projectName == "" {
		projectName = "(not set)"
	}
	base := fmt.Sprintf(
		"Task Context:\n- task_id: %s\n- run_id: %s\n- project_id: %s\n- project_name: %s\n- project_path: %s\n- title: %s\n- description:\n%s\n\nBefore coding, write task_id/run_id/project_id/project_name/title/description/start_time/status=running into auto_work_current_task.md.",
		task.ID, run.ID, task.ProjectID, projectName, projectPath, task.Title, task.Description,
	)
	systemPrompt := systemprompt.Render(task.SystemPrompt, task, run)
	if systemPrompt == "" {
		return base
	}
	return fmt.Sprintf("System Prompt:\n%s\n\n%s", systemPrompt, base)
}
