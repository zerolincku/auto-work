package repository_test

import (
	"slices"
	"testing"
	"time"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
)

func TestAgentRepository_UpsertRoundTripsAndListEnabled(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	lastSeenAt := time.Now().UTC().Truncate(time.Second)
	agent := &domain.Agent{
		ID:         "agent-primary",
		Name:       "Agent Primary",
		Provider:   "claude",
		Enabled:    true,
		LastSeenAt: &lastSeenAt,
	}

	if err := fixture.agentRepo.Upsert(fixture.ctx, agent); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}

	if agent.Concurrency != 1 {
		t.Fatalf("expected default concurrency 1, got %d", agent.Concurrency)
	}
	if agent.CreatedAt.IsZero() || agent.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be initialized, got created_at=%s updated_at=%s", agent.CreatedAt, agent.UpdatedAt)
	}

	persisted, err := fixture.agentRepo.GetByID(fixture.ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent by id: %v", err)
	}
	if !persisted.Enabled || persisted.Provider != "claude" {
		t.Fatalf("unexpected persisted agent: %+v", persisted)
	}
	if persisted.LastSeenAt == nil || !persisted.LastSeenAt.Equal(lastSeenAt) {
		t.Fatalf("expected last_seen_at=%s, got %v", lastSeenAt, persisted.LastSeenAt)
	}
	initialCreatedAt := persisted.CreatedAt

	agent.Name = "Agent Primary Updated"
	agent.Provider = "codex"
	agent.Enabled = false
	agent.Concurrency = 3
	if err := fixture.agentRepo.Upsert(fixture.ctx, agent); err != nil {
		t.Fatalf("upsert agent update: %v", err)
	}

	updated, err := fixture.agentRepo.GetByID(fixture.ctx, agent.ID)
	if err != nil {
		t.Fatalf("get updated agent: %v", err)
	}
	if updated.Name != "Agent Primary Updated" || updated.Provider != "codex" {
		t.Fatalf("unexpected updated agent fields: %+v", updated)
	}
	if updated.Enabled {
		t.Fatalf("expected updated agent to be disabled")
	}
	if updated.Concurrency != 3 {
		t.Fatalf("expected updated concurrency 3, got %d", updated.Concurrency)
	}
	if !updated.CreatedAt.Equal(initialCreatedAt) {
		t.Fatalf("expected created_at preserved, got %s want %s", updated.CreatedAt, initialCreatedAt)
	}
	if !updated.UpdatedAt.After(initialCreatedAt) {
		t.Fatalf("expected updated_at to advance beyond created_at, got %s", updated.UpdatedAt)
	}

	enabledAgent := &domain.Agent{ID: "agent-enabled", Name: "Agent Enabled", Provider: "claude", Enabled: true, Concurrency: 2}
	if err := fixture.agentRepo.Upsert(fixture.ctx, enabledAgent); err != nil {
		t.Fatalf("upsert enabled agent: %v", err)
	}

	enabled, err := fixture.agentRepo.ListEnabled(fixture.ctx)
	if err != nil {
		t.Fatalf("list enabled agents: %v", err)
	}
	gotIDs := make([]string, 0, len(enabled))
	for _, item := range enabled {
		gotIDs = append(gotIDs, item.ID)
	}
	if !slices.Equal(gotIDs, []string{"agent-enabled"}) {
		t.Fatalf("unexpected enabled agent ids: %v", gotIDs)
	}
}

func TestAgentRepository_GetByIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	if _, err := fixture.agentRepo.GetByID(fixture.ctx, "missing-agent"); err != repository.ErrAgentNotFound {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
}
