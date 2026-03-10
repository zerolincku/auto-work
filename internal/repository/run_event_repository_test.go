package repository_test

import (
	"slices"
	"testing"
	"time"

	"auto-work/internal/domain"
)

func TestRunEventRepository_AppendAndListByRun(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	project := mustCreateProject(t, fixture, "project-events")
	agent := mustCreateAgent(t, fixture, "agent-events")
	mustCreateTask(t, fixture, &domain.Task{ID: "task-events", ProjectID: project.ID, Title: "Events", Description: "desc", Priority: 100, Status: domain.TaskPending})
	run := &domain.Run{ID: "run-events", TaskID: "task-events", AgentID: agent.ID, PromptSnapshot: "snapshot"}
	if err := fixture.runRepo.Create(fixture.ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := fixture.runEventRepo.Append(fixture.ctx, run.ID, "runner.stderr", "later line"); err != nil {
		t.Fatalf("append first run event: %v", err)
	}
	if err := fixture.runEventRepo.Append(fixture.ctx, run.ID, "runner.stdout", "earlier line"); err != nil {
		t.Fatalf("append second run event: %v", err)
	}

	eventIDs := listRunEventIDsByNumericOrder(t, fixture, run.ID)
	if !slices.Equal(eventIDs, []string{"1", "2"}) {
		t.Fatalf("expected run event ids to increment from 1, got %v", eventIDs)
	}

	earliest := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	later := earliest.Add(time.Minute)
	if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `UPDATE run_events SET ts = ? WHERE id = ?`, later, eventIDs[0]); err != nil {
		t.Fatalf("update first event timestamp: %v", err)
	}
	if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `UPDATE run_events SET ts = ? WHERE id = ?`, earliest, eventIDs[1]); err != nil {
		t.Fatalf("update second event timestamp: %v", err)
	}

	events, err := fixture.runEventRepo.ListByRun(fixture.ctx, run.ID, 10)
	if err != nil {
		t.Fatalf("list run events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 run events, got %d", len(events))
	}
	if !slices.Equal([]string{events[0].ID, events[1].ID}, []string{eventIDs[1], eventIDs[0]}) {
		t.Fatalf("expected run events sorted oldest-to-newest, got %+v", events)
	}
	if events[0].Payload != "earlier line" || events[1].Payload != "later line" {
		t.Fatalf("unexpected event payload order: %+v", events)
	}
}

func TestRunEventRepository_ListRecentFiltersProjectAndKinds(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	project := mustCreateProject(t, fixture, "project-recent")
	otherProject := mustCreateProject(t, fixture, "project-recent-other")
	agent := mustCreateAgent(t, fixture, "agent-recent")
	otherAgent := mustCreateAgent(t, fixture, "agent-recent-other")

	mustCreateTask(t, fixture, &domain.Task{ID: "task-recent", ProjectID: project.ID, Title: "Recent", Description: "desc", Priority: 100, Status: domain.TaskPending})
	mustCreateTask(t, fixture, &domain.Task{ID: "task-recent-other", ProjectID: otherProject.ID, Title: "Recent Other", Description: "desc", Priority: 100, Status: domain.TaskPending})
	run := &domain.Run{ID: "run-recent", TaskID: "task-recent", AgentID: agent.ID, PromptSnapshot: "snapshot"}
	otherRun := &domain.Run{ID: "run-recent-other", TaskID: "task-recent-other", AgentID: otherAgent.ID, PromptSnapshot: "snapshot"}
	if err := fixture.runRepo.Create(fixture.ctx, run); err != nil {
		t.Fatalf("create project run: %v", err)
	}
	if err := fixture.runRepo.Create(fixture.ctx, otherRun); err != nil {
		t.Fatalf("create other project run: %v", err)
	}

	appendRunEvent(t, fixture, run.ID, "runner.stdout", "project stdout")
	appendRunEvent(t, fixture, run.ID, "runner.meta", "project meta")
	appendRunEvent(t, fixture, run.ID, "runner.stderr", "project stderr")
	appendRunEvent(t, fixture, otherRun.ID, "runner.stdout", "other stdout")

	allIDs := listRunEventIDsByNumericOrder(t, fixture, "")
	timestamps := map[string]time.Time{
		allIDs[0]: time.Now().UTC().Add(-4 * time.Minute).Truncate(time.Second),
		allIDs[1]: time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Second),
		allIDs[2]: time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second),
		allIDs[3]: time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second),
	}
	for id, ts := range timestamps {
		if _, err := fixture.sqlDB.ExecContext(fixture.ctx, `UPDATE run_events SET ts = ? WHERE id = ?`, ts, id); err != nil {
			t.Fatalf("update event %s timestamp: %v", id, err)
		}
	}

	projectRecent, err := fixture.runEventRepo.ListRecent(fixture.ctx, project.ID, 10)
	if err != nil {
		t.Fatalf("list recent project logs: %v", err)
	}
	if len(projectRecent) != 2 {
		t.Fatalf("expected 2 project log events, got %d", len(projectRecent))
	}
	if !slices.Equal([]string{projectRecent[0].Payload, projectRecent[1].Payload}, []string{"project stderr", "project stdout"}) {
		t.Fatalf("unexpected project recent payload order: %+v", projectRecent)
	}
	for _, item := range projectRecent {
		if item.ProjectID != project.ID {
			t.Fatalf("expected project-scoped logs, got %+v", item)
		}
		if item.Kind != "runner.stdout" && item.Kind != "runner.stderr" {
			t.Fatalf("expected only stdout/stderr kinds, got %+v", item)
		}
	}

	globalRecent, err := fixture.runEventRepo.ListRecent(fixture.ctx, "", 10)
	if err != nil {
		t.Fatalf("list global recent logs: %v", err)
	}
	if !slices.Equal([]string{globalRecent[0].Payload, globalRecent[1].Payload, globalRecent[2].Payload}, []string{"other stdout", "project stderr", "project stdout"}) {
		t.Fatalf("unexpected global recent payload order: %+v", globalRecent)
	}
}

func appendRunEvent(t *testing.T, fixture taskRepositoryFixture, runID, kind, payload string) {
	t.Helper()
	if err := fixture.runEventRepo.Append(fixture.ctx, runID, kind, payload); err != nil {
		t.Fatalf("append run event %s: %v", kind, err)
	}
}

func listRunEventIDsByNumericOrder(t *testing.T, fixture taskRepositoryFixture, runID string) []string {
	t.Helper()

	query := `SELECT id FROM run_events`
	args := make([]any, 0, 1)
	if runID != "" {
		query += ` WHERE run_id = ?`
		args = append(args, runID)
	}
	query += ` ORDER BY CAST(id AS INTEGER) ASC`

	rows, err := fixture.sqlDB.QueryContext(fixture.ctx, query, args...)
	if err != nil {
		t.Fatalf("query run event ids: %v", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan run event id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate run event ids: %v", err)
	}
	return ids
}
