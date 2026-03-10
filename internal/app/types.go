package app

import (
	"time"

	"auto-work/internal/domain"
)

type CreateTaskRequest struct {
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Provider    string `json:"provider"`
}

type UpdateTaskRequest struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
}

type CreateProjectRequest struct {
	Name                            string `json:"name"`
	Path                            string `json:"path"`
	DefaultProvider                 string `json:"default_provider"`
	Model                           string `json:"model"`
	SystemPrompt                    string `json:"system_prompt"`
	FailurePolicy                   string `json:"failure_policy"`
	FrontendScreenshotReportEnabled bool   `json:"frontend_screenshot_report_enabled"`
}

type UpdateProjectAIConfigRequest struct {
	ProjectID       string `json:"project_id"`
	DefaultProvider string `json:"default_provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"system_prompt"`
	FailurePolicy   string `json:"failure_policy"`
}

type UpdateProjectRequest struct {
	ProjectID                       string `json:"project_id"`
	Name                            string `json:"name"`
	DefaultProvider                 string `json:"default_provider"`
	Model                           string `json:"model"`
	SystemPrompt                    string `json:"system_prompt"`
	FailurePolicy                   string `json:"failure_policy"`
	FrontendScreenshotReportEnabled bool   `json:"frontend_screenshot_report_enabled"`
}

type DispatchResponse struct {
	Claimed bool   `json:"claimed"`
	RunID   string `json:"run_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type FrontendRunNotification struct {
	Kind        string `json:"kind"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	TaskTitle   string `json:"task_title,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Status      string `json:"status,omitempty"`
	RunStatus   string `json:"run_status,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
}

type FinishRunRequest struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Details    string `json:"details"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	TaskStatus string `json:"task_status"`
}

type RunningRunView struct {
	RunID       string     `json:"run_id"`
	TaskID      string     `json:"task_id"`
	TaskTitle   string     `json:"task_title"`
	ProjectID   string     `json:"project_id"`
	AgentID     string     `json:"agent_id"`
	PID         *int       `json:"pid,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`
}

type RunLogEventView struct {
	ID      string    `json:"id"`
	RunID   string    `json:"run_id"`
	TS      time.Time `json:"ts"`
	Kind    string    `json:"kind"`
	Payload string    `json:"payload"`
}

type SystemLogView struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	TaskID    string    `json:"task_id"`
	TaskTitle string    `json:"task_title"`
	ProjectID string    `json:"project_id"`
	TS        time.Time `json:"ts"`
	Kind      string    `json:"kind"`
	Payload   string    `json:"payload"`
}

type TaskLatestRunView struct {
	RunID         string     `json:"run_id"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty"`
	ResultSummary string     `json:"result_summary,omitempty"`
	ResultDetails string     `json:"result_details,omitempty"`
}

type TaskRunHistoryView struct {
	RunID         string     `json:"run_id"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty"`
	ResultSummary string     `json:"result_summary,omitempty"`
	ResultDetails string     `json:"result_details,omitempty"`
}

type TaskDetailView struct {
	Task *domain.Task         `json:"task"`
	Runs []TaskRunHistoryView `json:"runs"`
}

type MCPStatusView struct {
	Enabled   bool       `json:"enabled"`
	State     string     `json:"state"`
	Message   string     `json:"message"`
	RunID     string     `json:"run_id,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type GlobalSettingsView struct {
	TelegramEnabled        bool      `json:"telegram_enabled"`
	TelegramBotToken       string    `json:"telegram_bot_token"`
	TelegramChatIDs        string    `json:"telegram_chat_ids"`
	TelegramPollTimeout    int       `json:"telegram_poll_timeout"`
	TelegramProxyURL       string    `json:"telegram_proxy_url"`
	SystemNotificationMode string    `json:"system_notification_mode"`
	SystemPrompt           string    `json:"system_prompt"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type UpdateGlobalSettingsRequest struct {
	TelegramEnabled        bool   `json:"telegram_enabled"`
	TelegramBotToken       string `json:"telegram_bot_token"`
	TelegramChatIDs        string `json:"telegram_chat_ids"`
	TelegramPollTimeout    int    `json:"telegram_poll_timeout"`
	TelegramProxyURL       string `json:"telegram_proxy_url"`
	SystemNotificationMode string `json:"system_notification_mode"`
	SystemPrompt           string `json:"system_prompt"`
}
