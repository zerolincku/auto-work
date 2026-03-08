package domain

import "time"

type ProjectFailurePolicy string

const (
	ProjectFailurePolicyBlock    ProjectFailurePolicy = "block"
	ProjectFailurePolicyContinue ProjectFailurePolicy = "continue"
)

type Project struct {
	ID                  string
	Name                string
	Path                string
	DefaultProvider     string
	Model               string
	SystemPrompt        string
	FailurePolicy       ProjectFailurePolicy
	AutoDispatchEnabled bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
