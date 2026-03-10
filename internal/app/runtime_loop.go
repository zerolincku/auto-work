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
				a.runAutoDispatchCycle(context.Background())
			}
		}
	}()
}

const autoDispatchProjectBurstLimit = 16

func (a *App) runAutoDispatchCycle(ctx context.Context) {
	_ = a.recoverDeadRunningRuns(ctx, "recovered dead running run during auto-dispatch loop")
	projectIDs, err := a.projectRepo.ListAutoDispatchEnabledProjectIDs(ctx, 50)
	if err != nil || len(projectIDs) == 0 {
		return
	}
	for _, projectID := range projectIDs {
		claimed := a.dispatchProjectAvailableTasks(ctx, projectID, autoDispatchProjectBurstLimit)
		if claimed > 0 {
			a.log.Infof("auto-dispatch claimed %d task(s) for project_id=%s", claimed, strings.TrimSpace(projectID))
		}
	}
}

func (a *App) dispatchProjectAvailableTasks(ctx context.Context, projectID string, limit int) int {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0
	}
	if limit <= 0 {
		limit = 1
	}
	claimed := 0
	for i := 0; i < limit; i++ {
		resp, dispatchErr := a.DispatchOnce(ctx, "", projectID)
		if dispatchErr != nil {
			return claimed
		}
		if resp == nil || !resp.Claimed {
			return claimed
		}
		claimed++
	}
	return claimed
}

func (a *App) triggerAutoDispatch(projectID string) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	_ = a.dispatchProjectAvailableTasks(context.Background(), strings.TrimSpace(projectID), autoDispatchProjectBurstLimit)
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
