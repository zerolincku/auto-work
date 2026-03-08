package runner

import (
	"context"

	"auto-work/internal/domain"
)

type RunHealth struct {
	Alive       bool
	HeartbeatOK bool
	Message     string
}

type ProviderRunner interface {
	Start(ctx context.Context, run domain.Run, task domain.Task, agent domain.Agent) (pid int, err error)
	Stop(ctx context.Context, runID string) error
	Probe(ctx context.Context, runID string) (RunHealth, error)
}
