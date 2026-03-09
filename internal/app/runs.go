package app

import (
	"context"
	"strings"
)

func (a *App) ListRunningRuns(ctx context.Context, projectID string, limit int) ([]RunningRunView, error) {
	items, err := a.runRepo.ListRunning(ctx, strings.TrimSpace(projectID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunningRunView, 0, len(items))
	for _, v := range items {
		out = append(out, RunningRunView{
			RunID:       v.RunID,
			TaskID:      v.TaskID,
			TaskTitle:   v.TaskTitle,
			ProjectID:   v.ProjectID,
			AgentID:     v.AgentID,
			PID:         v.PID,
			Status:      string(v.Status),
			StartedAt:   v.StartedAt,
			HeartbeatAt: v.HeartbeatAt,
		})
	}
	return out, nil
}

func (a *App) ListRunLogs(ctx context.Context, runID string, limit int) ([]RunLogEventView, error) {
	events, err := a.eventRepo.ListByRun(ctx, strings.TrimSpace(runID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunLogEventView, 0, len(events))
	for _, e := range events {
		out = append(out, RunLogEventView{
			ID:      e.ID,
			RunID:   e.RunID,
			TS:      e.TS,
			Kind:    e.Kind,
			Payload: e.Payload,
		})
	}
	return out, nil
}

func (a *App) ListSystemLogs(ctx context.Context, projectID string, limit int) ([]SystemLogView, error) {
	events, err := a.eventRepo.ListRecent(ctx, strings.TrimSpace(projectID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]SystemLogView, 0, len(events))
	for _, e := range events {
		out = append(out, SystemLogView{
			ID:        e.ID,
			RunID:     e.RunID,
			TaskID:    e.TaskID,
			TaskTitle: e.TaskTitle,
			ProjectID: e.ProjectID,
			TS:        e.TS,
			Kind:      e.Kind,
			Payload:   e.Payload,
		})
	}
	return out, nil
}

func (a *App) GetTaskLatestRun(ctx context.Context, taskID string) (*TaskLatestRunView, error) {
	runs, err := a.runRepo.ListByTask(ctx, strings.TrimSpace(taskID), 1)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	r := runs[0]
	return &TaskLatestRunView{
		RunID:         r.ID,
		Status:        string(r.Status),
		Attempt:       r.Attempt,
		StartedAt:     r.StartedAt,
		FinishedAt:    r.FinishedAt,
		ExitCode:      r.ExitCode,
		ResultSummary: r.ResultSummary,
		ResultDetails: r.ResultDetails,
	}, nil
}

func (a *App) GetTaskDetail(ctx context.Context, taskID string) (*TaskDetailView, error) {
	task, err := a.taskRepo.GetByID(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}

	runs, err := a.runRepo.ListByTask(ctx, task.ID, 50)
	if err != nil {
		return nil, err
	}

	items := make([]TaskRunHistoryView, 0, len(runs))
	for _, r := range runs {
		items = append(items, TaskRunHistoryView{
			RunID:         r.ID,
			Status:        string(r.Status),
			Attempt:       r.Attempt,
			StartedAt:     r.StartedAt,
			FinishedAt:    r.FinishedAt,
			ExitCode:      r.ExitCode,
			ResultSummary: r.ResultSummary,
			ResultDetails: r.ResultDetails,
		})
	}

	return &TaskDetailView{
		Task: task,
		Runs: items,
	}, nil
}
