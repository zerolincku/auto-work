package repository

import (
	"context"
	"database/sql"
	"time"
)

type GlobalSettings struct {
	TelegramEnabled        bool
	TelegramBotToken       string
	TelegramChatIDs        string
	TelegramPollTimeout    int
	TelegramProxyURL       string
	SystemNotificationMode string
	SystemPrompt           string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type GlobalSettingsRepository struct {
	db *sql.DB
}

func NewGlobalSettingsRepository(db *sql.DB) *GlobalSettingsRepository {
	return &GlobalSettingsRepository{db: db}
}

func (r *GlobalSettingsRepository) EnsureDefaults(ctx context.Context, defaults GlobalSettings) error {
	now := time.Now().UTC()
	pollTimeout := defaults.TelegramPollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30
	}
	notificationMode := defaults.SystemNotificationMode
	if notificationMode == "" {
		notificationMode = "always"
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO global_settings(id, telegram_enabled, telegram_bot_token, telegram_chat_ids, telegram_poll_timeout, telegram_proxy_url, system_notification_mode, system_prompt, created_at, updated_at)
SELECT 1, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM global_settings WHERE id = 1)`,
		boolToInt(defaults.TelegramEnabled), defaults.TelegramBotToken, defaults.TelegramChatIDs, pollTimeout, defaults.TelegramProxyURL, notificationMode, defaults.SystemPrompt, now, now,
	)
	return err
}

func (r *GlobalSettingsRepository) Get(ctx context.Context) (*GlobalSettings, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT telegram_enabled, telegram_bot_token, telegram_chat_ids, telegram_poll_timeout, telegram_proxy_url, system_notification_mode, system_prompt, created_at, updated_at
FROM global_settings
WHERE id = 1`)

	var (
		enabled int
		v       GlobalSettings
	)
	if err := row.Scan(&enabled, &v.TelegramBotToken, &v.TelegramChatIDs, &v.TelegramPollTimeout, &v.TelegramProxyURL, &v.SystemNotificationMode, &v.SystemPrompt, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.TelegramEnabled = enabled == 1
	return &v, nil
}

func (r *GlobalSettingsRepository) Update(ctx context.Context, enabled bool, token, chatIDs string, pollTimeout int, proxyURL, systemNotificationMode, systemPrompt string) error {
	if pollTimeout <= 0 {
		pollTimeout = 30
	}
	if systemNotificationMode == "" {
		systemNotificationMode = "always"
	}
	_, err := r.db.ExecContext(ctx, `
UPDATE global_settings
SET telegram_enabled = ?,
    telegram_bot_token = ?,
    telegram_chat_ids = ?,
    telegram_poll_timeout = ?,
    telegram_proxy_url = ?,
    system_notification_mode = ?,
    system_prompt = ?,
    updated_at = ?
WHERE id = 1`,
		boolToInt(enabled), token, chatIDs, pollTimeout, proxyURL, systemNotificationMode, systemPrompt, time.Now().UTC(),
	)
	return err
}
