package repository_test

import (
	"slices"
	"testing"
	"time"

	"auto-work/internal/domain"
)

func TestRunRepository_CreateAndQueryRunningRuns(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	project := mustCreateProject(t, fixture, "project-runs")
	otherProject := mustCreateProject(t, fixture, "project-runs-other")
	agentOne := mustCreateAgent(t, fixture, "agent-run-one")
	agentTwo := mustCreateAgent(t, fixture, "agent-run-two")
	agentThree := mustCreateAgent(t, fixture, "agent-run-three")

	mustCreateTask(t, fixture, &domain.Task{ID: "task-run-main", ProjectID: project.ID, Title: "Main", Description: "desc", Priority: 100, Status: domain.TaskPending})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-run-other", ProjectID: otherProject.ID, Title: "Other", Description: "desc", Priority: 100, Status: domain.TaskPending})

	olderStartedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	olderFinishedAt := olderStartedAt.Add(20 * time.Minute)
	olderRun := &domain.Run{
		ID:             "run-older",
		TaskID:         "task-run-main",
		AgentID:        agentThree.ID,
		Attempt:        1,
		Status:         domain.RunDone,
		PromptSnapshot: "older snapshot",
		StartedAt:      olderStartedAt,
		FinishedAt:     &olderFinishedAt,
	}
	if err := fixture.runRepo.Create(fixture.ctx, olderRun); err != nil {
		t.Fatalf("create older run: %v", err)
	}

	heartbeatAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	pid := 4242
	run := &domain.Run{
		TaskID:         "task-run-main",
		AgentID:        agentOne.ID,
		PID:            &pid,
		HeartbeatAt:    &heartbeatAt,
		PromptSnapshot: "snapshot",
	}
	if err := fixture.runRepo.Create(fixture.ctx, run); err != nil {
		t.Fatalf("create running run: %v", err)
	}

	otherRunning := &domain.Run{
		TaskID:         "task-run-other",
		AgentID:        agentTwo.ID,
		PromptSnapshot: "other snapshot",
	}
	if err := fixture.runRepo.Create(fixture.ctx, otherRunning); err != nil {
		t.Fatalf("create other running run: %v", err)
	}

	if run.ID == "" {
		t.Fatalf("expected generated run id")
	}
	if run.Status != domain.RunRunning {
		t.Fatalf("expected default run status running, got %q", run.Status)
	}
	if run.Attempt != 1 {
		t.Fatalf("expected default run attempt 1, got %d", run.Attempt)
	}
	if run.StartedAt.IsZero() || run.CreatedAt.IsZero() || run.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be initialized, got started_at=%s created_at=%s updated_at=%s", run.StartedAt, run.CreatedAt, run.UpdatedAt)
	}

	persisted, err := fixture.runRepo.GetByID(fixture.ctx, run.ID)
	if err != nil {
		t.Fatalf("get run by id: %v", err)
	}
	if persisted.PID == nil || *persisted.PID != pid {
		t.Fatalf("expected persisted pid %d, got %v", pid, persisted.PID)
	}
	if persisted.HeartbeatAt == nil || !persisted.HeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("expected heartbeat_at=%s, got %v", heartbeatAt, persisted.HeartbeatAt)
	}

	runningByAgent, err := fixture.runRepo.GetRunningByAgent(fixture.ctx, agentOne.ID)
	if err != nil {
		t.Fatalf("get running run by agent: %v", err)
	}
	if runningByAgent.ID != run.ID {
		t.Fatalf("expected running run %s, got %s", run.ID, runningByAgent.ID)
	}

	projectRunning, err := fixture.runRepo.ListRunning(fixture.ctx, project.ID, 0)
	if err != nil {
		t.Fatalf("list running runs for project: %v", err)
	}
	if len(projectRunning) != 1 || projectRunning[0].RunID != run.ID {
		t.Fatalf("unexpected project running runs: %+v", projectRunning)
	}

	globalRunning, err := fixture.runRepo.ListRunning(fixture.ctx, "", 0)
	if err != nil {
		t.Fatalf("list running runs globally: %v", err)
	}
	gotRunningIDs := make([]string, 0, len(globalRunning))
	for _, item := range globalRunning {
		gotRunningIDs = append(gotRunningIDs, item.RunID)
	}
	if !slices.Equal(gotRunningIDs, []string{otherRunning.ID, run.ID}) && !slices.Equal(gotRunningIDs, []string{run.ID, otherRunning.ID}) {
		t.Fatalf("expected both running runs globally, got %v", gotRunningIDs)
	}

	runsByTask, err := fixture.runRepo.ListByTask(fixture.ctx, "task-run-main", 0)
	if err != nil {
		t.Fatalf("list runs by task: %v", err)
	}
	gotTaskRunIDs := make([]string, 0, len(runsByTask))
	for _, item := range runsByTask {
		gotTaskRunIDs = append(gotTaskRunIDs, item.ID)
	}
	if !slices.Equal(gotTaskRunIDs, []string{run.ID, olderRun.ID}) {
		t.Fatalf("expected task runs ordered by started_at desc, got %v", gotTaskRunIDs)
	}
}

func TestRunRepository_FinishAndRecoverOrphanRunningRuns(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	project := mustCreateProject(t, fixture, "project-run-finish")
	agentOne := mustCreateAgent(t, fixture, "agent-run-finish")
	agentTwo := mustCreateAgent(t, fixture, "agent-run-orphan")
	agentThree := mustCreateAgent(t, fixture, "agent-run-attached")

	mustCreateTask(t, fixture, &domain.Task{ID: "task-finish", ProjectID: project.ID, Title: "Finish", Description: "desc", Priority: 100, Status: domain.TaskPending})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-orphan", ProjectID: project.ID, Title: "Orphan", Description: "desc", Priority: 101, Status: domain.TaskRunning})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-attached", ProjectID: project.ID, Title: "Attached", Description: "desc", Priority: 102, Status: domain.TaskRunning})

	finishedRun := &domain.Run{ID: "run-finish", TaskID: "task-finish", AgentID: agentOne.ID, PromptSnapshot: "finish snapshot"}
	if err := fixture.runRepo.Create(fixture.ctx, finishedRun); err != nil {
		t.Fatalf("create finish run: %v", err)
	}
	orphanRun := &domain.Run{ID: "run-orphan", TaskID: "task-orphan", AgentID: agentTwo.ID, PromptSnapshot: "orphan snapshot"}
	if err := fixture.runRepo.Create(fixture.ctx, orphanRun); err != nil {
		t.Fatalf("create orphan run: %v", err)
	}
	pid := 8080
	attachedRun := &domain.Run{ID: "run-attached", TaskID: "task-attached", AgentID: agentThree.ID, PID: &pid, PromptSnapshot: "attached snapshot"}
	if err := fixture.runRepo.Create(fixture.ctx, attachedRun); err != nil {
		t.Fatalf("create attached run: %v", err)
	}

	exitCode := 17
	if err := fixture.runRepo.Finish(fixture.ctx, finishedRun.ID, domain.RunFailed, &exitCode, "runner failed", "stack trace"); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	persistedFinished, err := fixture.runRepo.GetByID(fixture.ctx, finishedRun.ID)
	if err != nil {
		t.Fatalf("get finished run: %v", err)
	}
	if persistedFinished.Status != domain.RunFailed {
		t.Fatalf("expected finished run status failed, got %q", persistedFinished.Status)
	}
	if persistedFinished.ExitCode == nil || *persistedFinished.ExitCode != exitCode {
		t.Fatalf("expected exit code %d, got %v", exitCode, persistedFinished.ExitCode)
	}
	if persistedFinished.ResultSummary != "runner failed" || persistedFinished.ResultDetails != "stack trace" {
		t.Fatalf("unexpected finish writeback: %+v", persistedFinished)
	}
	if persistedFinished.FinishedAt == nil {
		t.Fatalf("expected finished_at to be written")
	}

	affected, err := fixture.runRepo.RecoverOrphanRunningRuns(fixture.ctx, "worker exited before attach")
	if err != nil {
		t.Fatalf("recover orphan running runs: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected exactly one orphan run recovered, got %d", affected)
	}

	persistedOrphan, err := fixture.runRepo.GetByID(fixture.ctx, orphanRun.ID)
	if err != nil {
		t.Fatalf("get recovered orphan run: %v", err)
	}
	if persistedOrphan.Status != domain.RunFailed {
		t.Fatalf("expected orphan run status failed, got %q", persistedOrphan.Status)
	}
	if persistedOrphan.ResultSummary != "orphaned running run recovered" {
		t.Fatalf("expected orphan recovery summary, got %q", persistedOrphan.ResultSummary)
	}
	if persistedOrphan.ResultDetails != "worker exited before attach" {
		t.Fatalf("expected orphan recovery details, got %q", persistedOrphan.ResultDetails)
	}
	if persistedOrphan.FinishedAt == nil {
		t.Fatalf("expected orphan recovery to set finished_at")
	}

	orphanTask, err := fixture.taskRepo.GetByID(fixture.ctx, "task-orphan")
	if err != nil {
		t.Fatalf("get orphan task: %v", err)
	}
	if orphanTask.Status != domain.TaskFailed {
		t.Fatalf("expected orphan task status failed, got %q", orphanTask.Status)
	}
	attachedTask, err := fixture.taskRepo.GetByID(fixture.ctx, "task-attached")
	if err != nil {
		t.Fatalf("get attached task: %v", err)
	}
	if attachedTask.Status != domain.TaskRunning {
		t.Fatalf("expected attached task to remain running, got %q", attachedTask.Status)
	}

	persistedAttached, err := fixture.runRepo.GetByID(fixture.ctx, attachedRun.ID)
	if err != nil {
		t.Fatalf("get attached run: %v", err)
	}
	if persistedAttached.Status != domain.RunRunning {
		t.Fatalf("expected attached run to remain running, got %q", persistedAttached.Status)
	}
	if persistedAttached.ResultSummary != "" || persistedAttached.FinishedAt != nil {
		t.Fatalf("expected attached run to remain untouched, got %+v", persistedAttached)
	}
}
