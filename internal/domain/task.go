package domain

import "time"

type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
	TaskBlocked TaskStatus = "blocked"
)

type Task struct {
	ID           string
	ProjectID    string
	ProjectName  string
	ProjectPath  string
	Model        string
	SystemPrompt string
	Title        string
	Description  string
	Priority     int
	Status       TaskStatus
	DependsOn    []string
	Provider     string
	RetryCount   int
	MaxRetries   int
	NextRetryAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
