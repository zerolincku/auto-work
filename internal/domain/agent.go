package domain

import "time"

type Agent struct {
	ID          string
	Name        string
	Provider    string
	Enabled     bool
	Concurrency int
	LastSeenAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
