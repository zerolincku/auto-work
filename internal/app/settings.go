package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"auto-work/internal/integration/telegrambot"
	"auto-work/internal/repository"
)

const (
	systemNotificationModeNever         = "never"
	systemNotificationModeWhenUnfocused = "when_unfocused"
	systemNotificationModeAlways        = "always"
)

func (a *App) GetGlobalSettings(ctx context.Context) (*GlobalSettingsView, error) {
	settings, err := a.settingsRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return mapGlobalSettings(settings), nil
}

func (a *App) UpdateGlobalSettings(ctx context.Context, req UpdateGlobalSettingsRequest) (*GlobalSettingsView, error) {
	token := strings.TrimSpace(req.TelegramBotToken)
	chatIDs, err := normalizeTelegramChatIDsCSV(req.TelegramChatIDs)
	if err != nil {
		return nil, err
	}
	pollTimeout := req.TelegramPollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30
	}
	if pollTimeout > 120 {
		pollTimeout = 120
	}
	if req.TelegramEnabled && token == "" {
		return nil, errors.New("启用 Telegram 需要先填写 Bot Token")
	}
	proxyURL, err := normalizeProxyURL(req.TelegramProxyURL)
	if err != nil {
		return nil, err
	}
	settings, err := a.settingsRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	systemPrompt := settings.SystemPrompt
	systemNotificationMode := normalizeSystemNotificationMode(settings.SystemNotificationMode)
	if rawMode := strings.TrimSpace(req.SystemNotificationMode); rawMode != "" {
		systemNotificationMode, err = parseSystemNotificationMode(rawMode)
		if err != nil {
			return nil, err
		}
	}

	if err := a.settingsRepo.Update(ctx, req.TelegramEnabled, token, chatIDs, pollTimeout, proxyURL, systemNotificationMode, systemPrompt); err != nil {
		return nil, err
	}
	if _, err := a.reloadTelegramFromDB(ctx); err != nil {
		return nil, fmt.Errorf("配置已保存，但 Telegram 启动失败: %w", err)
	}
	return &GlobalSettingsView{
		TelegramEnabled:        req.TelegramEnabled,
		TelegramBotToken:       token,
		TelegramChatIDs:        chatIDs,
		TelegramPollTimeout:    pollTimeout,
		TelegramProxyURL:       proxyURL,
		SystemNotificationMode: systemNotificationMode,
		SystemPrompt:           systemPrompt,
		UpdatedAt:              time.Now().UTC(),
	}, nil
}

func (a *App) AutoRunEnabled(ctx context.Context, projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	enabled, err := a.projectRepo.AutoDispatchEnabled(ctx, projectID)
	if err != nil {
		return false
	}
	return enabled
}

func (a *App) SetAutoRunEnabled(ctx context.Context, projectID string, enabled bool) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	if err := a.projectRepo.SetAutoDispatchEnabled(ctx, projectID, enabled); err != nil {
		a.log.Errorf("set auto dispatch failed project_id=%s enabled=%t err=%v", projectID, enabled, err)
		return false
	}
	a.log.Infof("set auto dispatch project_id=%s enabled=%t", projectID, enabled)
	if enabled {
		go a.triggerAutoDispatch(projectID)
	}
	return enabled
}

func (a *App) reloadTelegramFromDB(ctx context.Context) (*repository.GlobalSettings, error) {
	settings, err := a.settingsRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.applyTelegramSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func (a *App) applyTelegramSettings(settings *repository.GlobalSettings) error {
	if settings == nil {
		return nil
	}
	a.stopTelegramService()
	if !settings.TelegramEnabled {
		a.log.Infof("telegram disabled by global settings")
		return nil
	}

	token := strings.TrimSpace(settings.TelegramBotToken)
	if token == "" {
		return errors.New("telegram enabled but bot token is empty")
	}
	chatIDs, err := parseTelegramChatIDsCSV(settings.TelegramChatIDs)
	if err != nil {
		return err
	}
	svc, err := telegrambot.New(telegrambot.Options{
		Token:       token,
		ChatIDs:     chatIDs,
		PollTimeout: settings.TelegramPollTimeout,
		ProxyURL:    strings.TrimSpace(settings.TelegramProxyURL),
		TaskRepo:    a.taskRepo,
		RunRepo:     a.runRepo,
		EventRepo:   a.eventRepo,
		ProjectRepo: a.projectRepo,
		ErrorReporter: func(format string, args ...any) {
			a.log.Errorf("telegram "+format, args...)
		},
		IncomingMessageReporter: a.handleTelegramIncomingMessage,
		DispatchTask: func(ctx context.Context, taskID string) (*telegrambot.DispatchTaskResult, error) {
			resp, err := a.DispatchTask(ctx, taskID)
			if err != nil {
				return nil, err
			}
			return &telegrambot.DispatchTaskResult{
				Claimed: resp.Claimed,
				RunID:   resp.RunID,
				TaskID:  resp.TaskID,
				Message: resp.Message,
			}, nil
		},
	})
	if err != nil {
		return err
	}
	svc.Start(context.Background())
	a.telegramMu.Lock()
	a.telegramSvc = svc
	a.telegramMu.Unlock()
	a.log.Infof("telegram bot polling started")
	return nil
}

func mapGlobalSettings(settings *repository.GlobalSettings) *GlobalSettingsView {
	if settings == nil {
		return nil
	}
	return &GlobalSettingsView{
		TelegramEnabled:        settings.TelegramEnabled,
		TelegramBotToken:       settings.TelegramBotToken,
		TelegramChatIDs:        settings.TelegramChatIDs,
		TelegramPollTimeout:    settings.TelegramPollTimeout,
		TelegramProxyURL:       settings.TelegramProxyURL,
		SystemNotificationMode: normalizeSystemNotificationMode(settings.SystemNotificationMode),
		SystemPrompt:           settings.SystemPrompt,
		UpdatedAt:              settings.UpdatedAt,
	}
}

func normalizeSystemNotificationMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case systemNotificationModeNever:
		return systemNotificationModeNever
	case systemNotificationModeWhenUnfocused, "unfocused", "app_unfocused":
		return systemNotificationModeWhenUnfocused
	case "", systemNotificationModeAlways:
		return systemNotificationModeAlways
	default:
		return systemNotificationModeAlways
	}
}

func parseSystemNotificationMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case systemNotificationModeNever:
		return systemNotificationModeNever, nil
	case systemNotificationModeWhenUnfocused, "unfocused", "app_unfocused":
		return systemNotificationModeWhenUnfocused, nil
	case systemNotificationModeAlways:
		return systemNotificationModeAlways, nil
	default:
		return "", errors.New("无效系统通知模式")
	}
}

func normalizeProxyURL(raw string) (string, error) {
	val := strings.TrimSpace(raw)
	if val == "" {
		return "", nil
	}
	u, err := url.Parse(val)
	if err != nil {
		return "", fmt.Errorf("无效代理地址: %v", err)
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return "", errors.New("无效代理地址: 需包含 scheme 与 host")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", errors.New("无效代理地址: 仅支持 http/https/socks5/socks5h")
	}
	return u.String(), nil
}

func parseTelegramChatIDsCSV(raw string) ([]int64, error) {
	normalized, err := normalizeTelegramChatIDsCSV(raw)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return nil, nil
	}
	parts := strings.Split(normalized, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.ParseInt(p, 10, 64)
		out = append(out, n)
	}
	return out, nil
}

func normalizeTelegramChatIDs(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, 0, len(values))
	for _, id := range values {
		out = append(out, strconv.FormatInt(id, 10))
	}
	return strings.Join(out, ",")
}

func normalizeTelegramChatIDsCSV(raw string) (string, error) {
	val := strings.TrimSpace(raw)
	if val == "" {
		return "", nil
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return "", fmt.Errorf("无效 Telegram Chat ID: %s", p)
		}
		key := strconv.FormatInt(id, 10)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return strings.Join(out, ","), nil
}
