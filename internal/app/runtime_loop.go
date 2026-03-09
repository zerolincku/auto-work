package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"auto-work/internal/domain"
)

func (a *App) startAutoDispatchLoop() {
	loopCtx, cancel := context.WithCancel(context.Background())
	a.loopCancel = cancel
	a.loopDone = make(chan struct{})

	go func() {
		defer close(a.loopDone)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				_ = a.recoverDeadRunningRuns(context.Background(), "recovered dead running run during auto-dispatch loop")
				projectIDs, err := a.projectRepo.ListAutoDispatchEnabledProjectIDs(context.Background(), 50)
				if err != nil || len(projectIDs) == 0 {
					continue
				}
				for _, projectID := range projectIDs {
					resp, dispatchErr := a.DispatchOnce(context.Background(), "", projectID)
					if dispatchErr != nil {
						continue
					}
					if resp != nil && resp.Claimed {
						break
					}
				}
			}
		}
	}()
}

func (a *App) triggerAutoDispatch(projectID string) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	_, _ = a.DispatchOnce(context.Background(), "", strings.TrimSpace(projectID))
}

func (a *App) recoverDeadRunningRuns(ctx context.Context, reason string) error {
	items, err := a.runRepo.ListRunning(ctx, "", 500)
	if err != nil {
		a.log.Errorf("list running runs for recovery failed: %v", err)
		return err
	}
	for _, run := range items {
		if run.PID == nil || *run.PID <= 0 {
			exitCode := 137
			_ = a.appendRunFailureLog(run.RunID, "orphan running run recovered", reason)
			_ = a.dispatcher.MarkRunFinished(context.Background(), run.RunID, domain.RunFailed, domain.TaskFailed, "orphan running run recovered", reason, &exitCode)
			a.log.Warnf("recover orphan running run run_id=%s reason=%s", run.RunID, strings.TrimSpace(reason))
			continue
		}
		if processExists(*run.PID) {
			continue
		}
		details := fmt.Sprintf("%s; pid=%d not alive", strings.TrimSpace(reason), *run.PID)
		exitCode := 137
		_ = a.appendRunFailureLog(run.RunID, "dead running run recovered", details)
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.RunID, domain.RunFailed, domain.TaskFailed, "dead running run recovered", details, &exitCode)
		a.log.Warnf("recover dead running run run_id=%s pid=%d", run.RunID, *run.PID)
	}
	return nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil || p == nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func findMCPFailureInDebugFile(path string) string {
	f, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	defer f.Close()

	var reason string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `MCP server "auto-work" Connection failed:`) {
			parts := strings.SplitN(line, `Connection failed:`, 2)
			if len(parts) == 2 {
				reason = strings.TrimSpace(parts[1])
			}
		}
		if strings.Contains(line, `MCP server "auto-work": Connection timeout triggered`) {
			reason = "MCP server connection timeout"
		}
	}
	return strings.TrimSpace(reason)
}
