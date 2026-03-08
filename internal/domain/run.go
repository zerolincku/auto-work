package domain

import "time"

type RunStatus string

const (
	RunRunning    RunStatus = "running"
	RunDone       RunStatus = "done"
	RunFailed     RunStatus = "failed"
	RunNeedsInput RunStatus = "needs_input"
	RunCancelled  RunStatus = "cancelled"
)

type Run struct {
	ID                string
	TaskID            string
	AgentID           string
	Attempt           int
	Status            RunStatus
	PID               *int
	HeartbeatAt       *time.Time
	StartedAt         time.Time
	FinishedAt        *time.Time
	ExitCode          *int
	ProviderSessionID string
	PromptSnapshot    string
	ResultSummary     string
	ResultDetails     string
	IdempotencyKey    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
