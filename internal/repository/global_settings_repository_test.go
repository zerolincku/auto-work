package repository_test

import (
	"testing"

	"auto-work/internal/repository"
)

func TestGlobalSettingsRepository_EnsureDefaultsAndUpdate(t *testing.T) {
	t.Parallel()

	fixture := setupTaskRepositoryFixture(t)
	settingsRepo := repository.NewGlobalSettingsRepository(fixture.sqlDB)

	if err := settingsRepo.EnsureDefaults(fixture.ctx, repository.GlobalSettings{
		TelegramEnabled:     true,
		TelegramBotToken:    "bot-token",
		TelegramChatIDs:     "1001,1002",
		TelegramPollTimeout: 0,
		TelegramProxyURL:    "http://proxy.local",
		SystemPrompt:        "initial prompt",
	}); err != nil {
		t.Fatalf("ensure defaults: %v", err)
	}

	settings, err := settingsRepo.Get(fixture.ctx)
	if err != nil {
		t.Fatalf("get settings after defaults: %v", err)
	}
	if !settings.TelegramEnabled {
		t.Fatalf("expected telegram to be enabled")
	}
	if settings.TelegramBotToken != "bot-token" || settings.TelegramChatIDs != "1001,1002" {
		t.Fatalf("unexpected telegram settings: %+v", settings)
	}
	if settings.TelegramPollTimeout != 30 {
		t.Fatalf("expected default poll timeout 30, got %d", settings.TelegramPollTimeout)
	}
	if settings.SystemNotificationMode != "always" {
		t.Fatalf("expected default notification mode always, got %q", settings.SystemNotificationMode)
	}
	if settings.SystemPrompt != "initial prompt" {
		t.Fatalf("expected system prompt to round-trip, got %q", settings.SystemPrompt)
	}
	initialCreatedAt := settings.CreatedAt
	initialUpdatedAt := settings.UpdatedAt

	if err := settingsRepo.EnsureDefaults(fixture.ctx, repository.GlobalSettings{
		TelegramEnabled:        false,
		TelegramBotToken:       "ignored-token",
		TelegramChatIDs:        "ignored-chat",
		TelegramPollTimeout:    99,
		TelegramProxyURL:       "http://ignored",
		SystemNotificationMode: "never",
		SystemPrompt:           "ignored prompt",
	}); err != nil {
		t.Fatalf("ensure defaults idempotently: %v", err)
	}

	persistedDefaults, err := settingsRepo.Get(fixture.ctx)
	if err != nil {
		t.Fatalf("get settings after second ensure defaults: %v", err)
	}
	if persistedDefaults.TelegramBotToken != "bot-token" || persistedDefaults.TelegramPollTimeout != 30 {
		t.Fatalf("expected ensure defaults not to overwrite existing row, got %+v", persistedDefaults)
	}
	if persistedDefaults.SystemNotificationMode != "always" {
		t.Fatalf("expected original notification mode to remain always, got %q", persistedDefaults.SystemNotificationMode)
	}
	if !persistedDefaults.CreatedAt.Equal(initialCreatedAt) {
		t.Fatalf("expected created_at preserved, got %s want %s", persistedDefaults.CreatedAt, initialCreatedAt)
	}

	if err := settingsRepo.Update(fixture.ctx, false, "updated-token", "2001", 0, "", "", "updated prompt"); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	updated, err := settingsRepo.Get(fixture.ctx)
	if err != nil {
		t.Fatalf("get updated settings: %v", err)
	}
	if updated.TelegramEnabled {
		t.Fatalf("expected telegram to be disabled after update")
	}
	if updated.TelegramBotToken != "updated-token" || updated.TelegramChatIDs != "2001" {
		t.Fatalf("unexpected updated telegram settings: %+v", updated)
	}
	if updated.TelegramPollTimeout != 30 {
		t.Fatalf("expected zero timeout to fall back to 30, got %d", updated.TelegramPollTimeout)
	}
	if updated.SystemNotificationMode != "always" {
		t.Fatalf("expected blank notification mode to fall back to always, got %q", updated.SystemNotificationMode)
	}
	if updated.SystemPrompt != "updated prompt" {
		t.Fatalf("expected updated system prompt, got %q", updated.SystemPrompt)
	}
	if !updated.CreatedAt.Equal(initialCreatedAt) {
		t.Fatalf("expected created_at preserved on update, got %s want %s", updated.CreatedAt, initialCreatedAt)
	}
	if !updated.UpdatedAt.After(initialUpdatedAt) {
		t.Fatalf("expected updated_at to advance beyond %s, got %s", initialUpdatedAt, updated.UpdatedAt)
	}
}
