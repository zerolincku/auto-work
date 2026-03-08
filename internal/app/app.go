package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"auto-work/internal/applog"
	"auto-work/internal/config"
	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/domain"
	"auto-work/internal/integration/telegrambot"
	mcphttp "auto-work/internal/mcp/httpserver"
	"auto-work/internal/repository"
	clauderunner "auto-work/internal/runner/claude"
	codexrunner "auto-work/internal/runner/codex"
	"auto-work/internal/runsignal"
	projectservice "auto-work/internal/service/project"
	"auto-work/internal/service/scheduler"
	taskservice "auto-work/internal/service/task"
	"auto-work/internal/systemprompt"
)

type App struct {
	cfg config.Config
	db  *sql.DB

	taskRepo     *repository.TaskRepository
	projectRepo  *repository.ProjectRepository
	settingsRepo *repository.GlobalSettingsRepository
	agentRepo    *repository.AgentRepository
	runRepo      *repository.RunRepository
	eventRepo    *repository.RunEventRepository

	projectSvc *projectservice.Service
	taskSvc    *taskservice.Service
	dispatcher *scheduler.Dispatcher

	claudeRunner             *clauderunner.Runner
	codexRunner              *codexrunner.Runner
	telegramSvc              *telegrambot.Service
	telegramMu               sync.Mutex
	telegramIncomingMu       sync.RWMutex
	telegramIncomingReporter func(telegrambot.IncomingMessage)
	frontendChangeMu         sync.Mutex
	runFrontendBaseline      map[string]frontendRunBaseline
	mcpCancel                context.CancelFunc
	mcpDone                  chan struct{}
	loopCancel               context.CancelFunc
	loopDone                 chan struct{}
	log                      *applog.Logger
}

type CreateTaskRequest struct {
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	DependsOn   []string `json:"depends_on"`
	Provider    string   `json:"provider"`
}

type UpdateTaskRequest struct {
	TaskID      string   `json:"task_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	DependsOn   []string `json:"depends_on"`
}

type CreateProjectRequest struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	DefaultProvider string `json:"default_provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"system_prompt"`
	FailurePolicy   string `json:"failure_policy"`
}

type UpdateProjectAIConfigRequest struct {
	ProjectID       string `json:"project_id"`
	DefaultProvider string `json:"default_provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"system_prompt"`
	FailurePolicy   string `json:"failure_policy"`
}

type UpdateProjectRequest struct {
	ProjectID       string `json:"project_id"`
	Name            string `json:"name"`
	DefaultProvider string `json:"default_provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"system_prompt"`
	FailurePolicy   string `json:"failure_policy"`
}

type DispatchResponse struct {
	Claimed bool   `json:"claimed"`
	RunID   string `json:"run_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type FinishRunRequest struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"`      // done/failed/blocked
	Summary    string `json:"summary"`     // human readable
	Details    string `json:"details"`     // technical details
	ExitCode   *int   `json:"exit_code"`   // optional
	TaskStatus string `json:"task_status"` // done/failed/blocked
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
	State     string     `json:"state"` // disabled/connected/failed/unknown
	Message   string     `json:"message"`
	RunID     string     `json:"run_id,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type GlobalSettingsView struct {
	TelegramEnabled     bool      `json:"telegram_enabled"`
	TelegramBotToken    string    `json:"telegram_bot_token"`
	TelegramChatIDs     string    `json:"telegram_chat_ids"`
	TelegramPollTimeout int       `json:"telegram_poll_timeout"`
	TelegramProxyURL    string    `json:"telegram_proxy_url"`
	SystemPrompt        string    `json:"system_prompt"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type UpdateGlobalSettingsRequest struct {
	TelegramEnabled     bool   `json:"telegram_enabled"`
	TelegramBotToken    string `json:"telegram_bot_token"`
	TelegramChatIDs     string `json:"telegram_chat_ids"`
	TelegramPollTimeout int    `json:"telegram_poll_timeout"`
	TelegramProxyURL    string `json:"telegram_proxy_url"`
	SystemPrompt        string `json:"system_prompt"`
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	sqlDB, err := db.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}

	logPath := strings.TrimSpace(cfg.AppLogPath)
	if logPath == "" {
		logPath = filepath.Join(filepath.Dir(strings.TrimSpace(cfg.DatabasePath)), "log", "auto-work.log")
	}
	cfg.AppLogPath = logPath

	appLogger, err := applog.New(logPath, cfg.AppLogMaxSizeMB, cfg.AppLogMaxBackups)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("init app logger: %w", err)
	}

	if err := migrate.Up(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		_ = appLogger.Close()
		return nil, err
	}

	taskRepo := repository.NewTaskRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	settingsRepo := repository.NewGlobalSettingsRepository(sqlDB)
	agentRepo := repository.NewAgentRepository(sqlDB)
	runRepo := repository.NewRunRepository(sqlDB)
	eventRepo := repository.NewRunEventRepository(sqlDB)

	a := &App{
		cfg:                 cfg,
		db:                  sqlDB,
		taskRepo:            taskRepo,
		projectRepo:         projectRepo,
		settingsRepo:        settingsRepo,
		agentRepo:           agentRepo,
		runRepo:             runRepo,
		eventRepo:           eventRepo,
		projectSvc:          projectservice.NewService(projectRepo),
		taskSvc:             taskservice.NewService(taskRepo, projectRepo),
		dispatcher:          scheduler.NewDispatcher(sqlDB),
		runFrontendBaseline: make(map[string]frontendRunBaseline),
		log:                 appLogger,
	}
	a.log.Infof("startup begin db=%s log=%s", strings.TrimSpace(cfg.DatabasePath), appLogger.Path())
	a.dispatcher.SetRunFinishedHook(a.onRunFinishedTelegramNotify)

	mcpBaseURL := strings.TrimSpace(cfg.MCPHTTPURL)
	if cfg.EnableMCPCallback {
		var err error
		mcpBaseURL, err = a.startMCPHTTPServer()
		if err != nil {
			_ = sqlDB.Close()
			_ = appLogger.Close()
			return nil, err
		}
	}

	a.claudeRunner = clauderunner.New(clauderunner.Options{
		Binary:            cfg.ClaudeBinary,
		Model:             cfg.ClaudeModel,
		Workdir:           cfg.WorkspacePath,
		DebugDir:          filepath.Join(os.TempDir(), "auto-work", "claude-debug"),
		AllowedTools:      cfg.ClaudeAllowedTools,
		PermissionMode:    cfg.ClaudePermissionMode,
		EnableMCPCallback: cfg.EnableMCPCallback,
		MCPTransport:      "http",
		MCPHTTPURL:        mcpBaseURL,
		OnLine:            a.onClaudeLine,
		OnExit:            a.onClaudeExit,
	})
	a.codexRunner = codexrunner.New(codexrunner.Options{
		Binary:            cfg.CodexBinary,
		Model:             cfg.CodexModel,
		Workdir:           cfg.WorkspacePath,
		DebugDir:          filepath.Join(os.TempDir(), "auto-work", "codex-debug"),
		EnableMCPCallback: cfg.EnableMCPCallback,
		MCPTransport:      "http",
		MCPHTTPURL:        mcpBaseURL,
		OnLine:            a.onCodexLine,
		OnExit:            a.onCodexExit,
	})
	if err := a.settingsRepo.EnsureDefaults(ctx, repository.GlobalSettings{
		TelegramEnabled:     cfg.TelegramEnabled,
		TelegramBotToken:    strings.TrimSpace(cfg.TelegramBotToken),
		TelegramChatIDs:     normalizeTelegramChatIDs(cfg.TelegramChatIDs),
		TelegramPollTimeout: cfg.TelegramPollTimeout,
		TelegramProxyURL:    "",
		SystemPrompt:        systemprompt.DefaultGlobalSystemPromptTemplate,
	}); err != nil {
		_ = sqlDB.Close()
		_ = appLogger.Close()
		return nil, err
	}
	if _, err := a.reloadTelegramFromDB(ctx); err != nil {
		a.log.Errorf("telegram reload from global settings failed: %v", err)
	}

	if err := a.ensureDefaultAgents(ctx); err != nil {
		_ = sqlDB.Close()
		_ = appLogger.Close()
		return nil, err
	}
	_ = a.recoverDeadRunningRuns(context.Background(), "recovered dead running run on startup")
	a.startAutoDispatchLoop()
	a.log.Infof("startup completed")
	return a, nil
}

func (a *App) Close() error {
	a.log.Infof("shutdown begin")
	a.stopTelegramService()
	if a.mcpCancel != nil {
		a.mcpCancel()
	}
	if a.mcpDone != nil {
		<-a.mcpDone
	}
	if a.loopCancel != nil {
		a.loopCancel()
	}
	if a.loopDone != nil {
		<-a.loopDone
	}
	dbErr := a.db.Close()
	if a.log != nil {
		_ = a.log.Close()
	}
	return dbErr
}

func (a *App) startMCPHTTPServer() (string, error) {
	baseURL, listenAddr, endpointPath, err := normalizeMCPHTTPURL(strings.TrimSpace(a.cfg.MCPHTTPURL))
	if err != nil {
		return "", err
	}
	server := mcphttp.NewServer(a.runRepo, a.projectRepo, a.taskRepo, a.eventRepo, a.dispatcher, "", "", endpointPath, a.log)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return "", fmt.Errorf("start in-process mcp http server failed (listen %s): %w", listenAddr, err)
	}
	if parsed, parseErr := url.Parse(baseURL); parseErr == nil {
		parsed.Host = ln.Addr().String()
		baseURL = parsed.String()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.mcpCancel = cancel
	a.mcpDone = done

	go func() {
		defer close(done)
		if serveErr := server.ServeListener(ctx, ln); serveErr != nil {
			a.log.Errorf("mcp-http server stopped with error: %v", serveErr)
		}
	}()
	a.log.Infof("mcp-http in-process server listening at %s", baseURL)
	return baseURL, nil
}

func normalizeMCPHTTPURL(raw string) (baseURL string, listenAddr string, endpointPath string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "http://127.0.0.1:39123/mcp"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid mcp http url: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(u.Scheme)) != "http" {
		return "", "", "", fmt.Errorf("mcp http url must use http scheme: %s", raw)
	}
	host := strings.TrimSpace(u.Hostname())
	port := strings.TrimSpace(u.Port())
	if host == "" || port == "" {
		return "", "", "", fmt.Errorf("mcp http url must include host and port: %s", raw)
	}
	listenAddr = net.JoinHostPort(host, port)
	endpointPath = strings.TrimSpace(u.Path)
	if endpointPath == "" {
		endpointPath = "/mcp"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	u.Path = endpointPath
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), listenAddr, endpointPath, nil
}

func (a *App) Health() string {
	return fmt.Sprintf("ok db=%s", a.cfg.DatabasePath)
}

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

	if err := a.settingsRepo.UpdateTelegram(ctx, req.TelegramEnabled, token, chatIDs, pollTimeout, proxyURL, systemPrompt); err != nil {
		return nil, err
	}
	if _, err := a.reloadTelegramFromDB(ctx); err != nil {
		return nil, fmt.Errorf("配置已保存，但 Telegram 启动失败: %w", err)
	}
	return &GlobalSettingsView{
		TelegramEnabled:     req.TelegramEnabled,
		TelegramBotToken:    token,
		TelegramChatIDs:     chatIDs,
		TelegramPollTimeout: pollTimeout,
		TelegramProxyURL:    proxyURL,
		SystemPrompt:        systemPrompt,
		UpdatedAt:           time.Now().UTC(),
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

func (a *App) stopTelegramService() {
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramSvc = nil
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	svc.Stop(stopCtx)
	cancel()
}

func (a *App) SetTelegramIncomingReporter(reporter func(telegrambot.IncomingMessage)) {
	a.telegramIncomingMu.Lock()
	a.telegramIncomingReporter = reporter
	a.telegramIncomingMu.Unlock()
}

func (a *App) handleTelegramIncomingMessage(message telegrambot.IncomingMessage) {
	a.telegramIncomingMu.RLock()
	reporter := a.telegramIncomingReporter
	a.telegramIncomingMu.RUnlock()
	if reporter == nil {
		return
	}
	reporter(message)
}

func (a *App) notifyTaskStarted(task *domain.Task, run *domain.Run, agent domain.Agent) {
	if task == nil || run == nil {
		return
	}
	projectLabel := a.projectLabelForNotify(context.Background(), task.ProjectID)
	provider := strings.TrimSpace(task.Provider)
	if provider == "" {
		provider = strings.TrimSpace(agent.Provider)
	}
	if provider == "" {
		provider = "unknown"
	}
	msg := strings.TrimSpace(fmt.Sprintf(
		"🚀 任务启动\n项目: %s\n标题: %s\n任务ID: %s\nRun: %s (attempt=%d)\nProvider: %s\n优先级: %d\n时间: %s",
		projectLabel,
		trimTextForNotify(task.Title, 180),
		task.ID,
		run.ID,
		run.Attempt,
		strings.ToLower(strings.TrimSpace(provider)),
		task.Priority,
		time.Now().Local().Format("2006-01-02 15:04:05"),
	))
	a.sendTelegramNotification(msg)
}

func (a *App) onRunFinishedTelegramNotify(_ context.Context, event scheduler.RunFinishedEvent) {
	if event.TaskStatus != domain.TaskDone && event.TaskStatus != domain.TaskFailed && event.TaskStatus != domain.TaskBlocked {
		return
	}
	baselineConsumed := false
	defer func() {
		if !baselineConsumed {
			a.discardRunFrontendBaseline(event.RunID)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := a.taskRepo.GetByID(ctx, strings.TrimSpace(event.TaskID))
	if err != nil {
		return
	}
	run, err := a.runRepo.GetByID(ctx, strings.TrimSpace(event.RunID))
	if err != nil {
		return
	}

	projectLabel := a.projectLabelForNotify(ctx, task.ProjectID)
	stateLabel := "运行结束"
	icon := "ℹ️"
	switch event.TaskStatus {
	case domain.TaskDone:
		stateLabel = "任务完成"
		icon = "✅"
	case domain.TaskFailed:
		stateLabel = "任务失败"
		icon = "❌"
	case domain.TaskBlocked:
		stateLabel = "任务阻塞"
		icon = "⛔"
	}

	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		summary = "-"
	}
	msg := strings.TrimSpace(fmt.Sprintf(
		"%s %s\n项目: %s\n标题: %s\n任务ID: %s\nRun: %s (attempt=%d)\n状态: task=%s run=%s\n摘要: %s\n退出码: %s\n时间: %s",
		icon,
		stateLabel,
		projectLabel,
		trimTextForNotify(task.Title, 180),
		task.ID,
		run.ID,
		run.Attempt,
		task.Status,
		run.Status,
		trimTextForNotify(summary, 300),
		formatExitCodeForNotify(event.ExitCode),
		time.Now().Local().Format("2006-01-02 15:04:05"),
	))
	a.sendTelegramNotification(msg)

	if event.TaskStatus != domain.TaskDone {
		return
	}
	frontendChanges := a.consumeRunFrontendChanges(ctx, event.RunID, task.ProjectID)
	baselineConsumed = true
	if !telegramFrontendScreenshotEnabled() {
		return
	}
	projectPath := strings.TrimSpace(task.ProjectPath)
	if projectPath == "" {
		projectPath = a.lookupProjectPath(ctx, task.ProjectID)
	}
	refs := extractAIScreenshotRefs(
		projectPath,
		event.Details,
		run.ResultDetails,
	)
	shouldSendScreens := decideScreenshotNotify(refs)
	if !shouldSendScreens {
		a.log.Debugf("skip screenshot notify: no screenshot refs run_id=%s task_id=%s frontend_changes=%d", run.ID, task.ID, len(frontendChanges))
		return
	}

	a.log.Infof("sending screenshot notification run_id=%s refs=%d local_refs=%d frontend_changes=%d", run.ID, len(refs.Ordered), len(refs.LocalPaths), len(frontendChanges))
	localPaths := existingImageFiles(refs.LocalPaths)
	if len(localPaths) > 0 {
		a.sendTelegramPhotos(buildFrontendScreenshotCaption(task, run, frontendChanges), localPaths)
		return
	}
	a.log.Warnf("screenshot refs found but no existing local image files run_id=%s", run.ID)
}

func (a *App) sendTelegramNotification(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	go svc.NotifyAll(text)
}

func (a *App) sendTelegramPhotos(caption string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramSvc
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	svc.NotifyAllPhotos(caption, filePaths)
}

func (a *App) projectLabelForNotify(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "-"
	}
	project, err := a.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return projectID
	}
	name := strings.TrimSpace(project.Name)
	if name == "" {
		return projectID
	}
	return fmt.Sprintf("%s(%s)", name, projectID)
}

func trimTextForNotify(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return text
	}
	runes := []rune(text)
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func formatExitCodeForNotify(exitCode *int) string {
	if exitCode == nil {
		return "-"
	}
	return strconv.Itoa(*exitCode)
}

func (a *App) CreateTask(ctx context.Context, req CreateTaskRequest) (*domain.Task, error) {
	return a.taskSvc.Create(ctx, taskservice.CreateTaskInput{
		ProjectID:   req.ProjectID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		DependsOn:   req.DependsOn,
		Provider:    req.Provider,
	})
}

func (a *App) UpdateTask(ctx context.Context, req UpdateTaskRequest) (*domain.Task, error) {
	return a.taskSvc.Update(ctx, taskservice.UpdateTaskInput{
		TaskID:      req.TaskID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		DependsOn:   req.DependsOn,
	})
}

func (a *App) DeleteTask(ctx context.Context, taskID string) error {
	return a.taskSvc.Delete(ctx, taskID)
}

func (a *App) CreateProject(ctx context.Context, req CreateProjectRequest) (*domain.Project, error) {
	return a.projectSvc.Create(ctx, projectservice.CreateProjectInput{
		Name:            req.Name,
		Path:            req.Path,
		DefaultProvider: req.DefaultProvider,
		Model:           req.Model,
		SystemPrompt:    req.SystemPrompt,
		FailurePolicy:   req.FailurePolicy,
	})
}

func (a *App) UpdateProjectAIConfig(ctx context.Context, req UpdateProjectAIConfigRequest) (*domain.Project, error) {
	return a.projectSvc.UpdateAIConfig(ctx, projectservice.UpdateProjectAIInput{
		ProjectID:       req.ProjectID,
		DefaultProvider: req.DefaultProvider,
		Model:           req.Model,
		SystemPrompt:    req.SystemPrompt,
		FailurePolicy:   req.FailurePolicy,
	})
}

func (a *App) UpdateProject(ctx context.Context, req UpdateProjectRequest) (*domain.Project, error) {
	return a.projectSvc.Update(ctx, projectservice.UpdateProjectInput{
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		DefaultProvider: req.DefaultProvider,
		Model:           req.Model,
		SystemPrompt:    req.SystemPrompt,
		FailurePolicy:   req.FailurePolicy,
	})
}

func (a *App) DeleteProject(ctx context.Context, projectID string) error {
	return a.projectSvc.Delete(ctx, projectID)
}

func (a *App) ListProjects(ctx context.Context, limit int) ([]domain.Project, error) {
	return a.projectSvc.List(ctx, limit)
}

func (a *App) ListTasks(ctx context.Context, status, provider, projectID string, limit int) ([]domain.Task, error) {
	return a.taskSvc.List(ctx, status, provider, projectID, limit)
}

func (a *App) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	return a.taskSvc.UpdateStatus(ctx, taskID, status)
}

func (a *App) RetryTask(ctx context.Context, taskID string) error {
	return a.taskSvc.Retry(ctx, taskID)
}

func (a *App) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	return a.agentRepo.ListEnabled(ctx)
}

func (a *App) DispatchOnce(ctx context.Context, agentID, projectID string) (*DispatchResponse, error) {
	_ = a.releaseDueRetryTasks(ctx, 50)
	agent, err := a.resolveDispatchAgent(ctx, agentID, projectID)
	if err != nil {
		a.log.Errorf("resolve dispatch agent failed agent_id=%s project_id=%s err=%v", strings.TrimSpace(agentID), strings.TrimSpace(projectID), err)
		return nil, err
	}
	if msg, blocked := a.dispatchDisabledMessage(*agent); blocked {
		return &DispatchResponse{
			Claimed: false,
			Message: msg,
		}, nil
	}

	promptSnapshot := fmt.Sprintf("auto dispatch at %s", time.Now().UTC().Format(time.RFC3339))
	task, run, err := a.dispatcher.ClaimNextTaskForAgent(ctx, agent.ID, agent.Provider, projectID, promptSnapshot)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoRunnableTask) {
			return &DispatchResponse{Claimed: false, Message: "当前项目没有可执行任务"}, nil
		}
		a.log.Errorf("claim next task failed agent_id=%s provider=%s project_id=%s err=%v", agent.ID, agent.Provider, strings.TrimSpace(projectID), err)
		return nil, err
	}
	return a.finalizeDispatchedTask(ctx, *agent, task, run)
}

func (a *App) DispatchTask(ctx context.Context, taskID string) (*DispatchResponse, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task id is required")
	}
	_ = a.releaseDueRetryTasks(ctx, 50)

	task, err := a.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskPending {
		return &DispatchResponse{
			Claimed: false,
			TaskID:  task.ID,
			Message: fmt.Sprintf("任务当前状态为 %s，仅 pending 任务可手动派发", task.Status),
		}, nil
	}

	agent, err := a.resolveDispatchAgent(ctx, "", task.ProjectID)
	if err != nil {
		a.log.Errorf("resolve dispatch agent failed task_id=%s project_id=%s err=%v", task.ID, task.ProjectID, err)
		return nil, err
	}
	if msg, blocked := a.dispatchDisabledMessage(*agent); blocked {
		return &DispatchResponse{
			Claimed: false,
			TaskID:  task.ID,
			Message: msg,
		}, nil
	}

	promptSnapshot := fmt.Sprintf("manual dispatch task=%s at %s", task.ID, time.Now().UTC().Format(time.RFC3339))
	claimedTask, run, err := a.dispatcher.ClaimTaskForAgent(ctx, agent.ID, agent.Provider, task.ID, promptSnapshot)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoRunnableTask) {
			return &DispatchResponse{
				Claimed: false,
				TaskID:  task.ID,
				Message: "任务当前不可派发，可能是依赖未完成、被失败策略阻塞，或对应 agent 正忙",
			}, nil
		}
		a.log.Errorf("claim specific task failed task_id=%s agent_id=%s provider=%s err=%v", task.ID, agent.ID, agent.Provider, err)
		return nil, err
	}
	return a.finalizeDispatchedTask(ctx, *agent, claimedTask, run)
}

func (a *App) finalizeDispatchedTask(ctx context.Context, agent domain.Agent, task *domain.Task, run *domain.Run) (*DispatchResponse, error) {
	a.log.Infof("task claimed task_id=%s run_id=%s provider=%s project_id=%s", task.ID, run.ID, strings.ToLower(strings.TrimSpace(agent.Provider)), task.ProjectID)

	projectSystemPrompt := ""
	if task.ProjectID != "" {
		project, getErr := a.projectRepo.GetByID(ctx, task.ProjectID)
		if getErr == nil {
			task.ProjectName = strings.TrimSpace(project.Name)
			task.ProjectPath = project.Path
			task.Model = strings.TrimSpace(project.Model)
			projectSystemPrompt = strings.TrimSpace(project.SystemPrompt)
		}
	}
	settings, settingsErr := a.settingsRepo.Get(ctx)
	if settingsErr != nil {
		a.log.Errorf("load global settings failed run_id=%s err=%v", run.ID, settingsErr)
		return nil, fmt.Errorf("load global settings: %w", settingsErr)
	}
	task.SystemPrompt = systemprompt.Compose(settings.SystemPrompt, projectSystemPrompt)
	task.SystemPrompt = appendFrontendScreenshotPromptHint(task.SystemPrompt)
	a.recordRunFrontendBaseline(run.ID, task.ProjectPath)
	pid, startErr := a.startProviderRun(ctx, agent, *run, *task)
	if startErr != nil {
		exitCode := 127
		summary := fmt.Sprintf("%s start failed", strings.ToLower(strings.TrimSpace(agent.Provider)))
		_ = a.appendRunFailureLog(run.ID, summary, startErr.Error())
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.ID, domain.RunFailed, domain.TaskFailed, summary, startErr.Error(), &exitCode)
		a.log.Errorf("start provider run failed task_id=%s run_id=%s provider=%s err=%v", task.ID, run.ID, agent.Provider, startErr)
		return &DispatchResponse{
			Claimed: true,
			RunID:   run.ID,
			TaskID:  task.ID,
			Message: "任务启动失败，已记录失败日志",
		}, nil
	}
	if err := a.runRepo.AttachProcess(ctx, run.ID, pid); err != nil {
		_ = a.stopProviderRun(context.Background(), agent.Provider, run.ID)
		exitCode := 126
		_ = a.appendRunFailureLog(run.ID, "attach process pid failed", err.Error())
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.ID, domain.RunFailed, domain.TaskFailed, "attach process pid failed", err.Error(), &exitCode)
		a.log.Errorf("attach process pid failed run_id=%s pid=%d err=%v", run.ID, pid, err)
		return &DispatchResponse{
			Claimed: true,
			RunID:   run.ID,
			TaskID:  task.ID,
			Message: "任务进程附加失败，已记录失败日志",
		}, nil
	}
	_ = a.eventRepo.Append(context.Background(), run.ID, "system.started", fmt.Sprintf("%s process started pid=%d", strings.ToLower(strings.TrimSpace(agent.Provider)), pid))
	a.log.Infof("provider run started task_id=%s run_id=%s pid=%d provider=%s", task.ID, run.ID, pid, strings.ToLower(strings.TrimSpace(agent.Provider)))
	a.notifyTaskStarted(task, run, agent)

	return &DispatchResponse{
		Claimed: true,
		RunID:   run.ID,
		TaskID:  task.ID,
		Message: "task claimed",
	}, nil
}

func (a *App) dispatchDisabledMessage(agent domain.Agent) (string, bool) {
	if strings.EqualFold(agent.Provider, "claude") && !a.cfg.RunClaudeOnDispatch {
		return "当前环境未启用 Claude 执行（AUTO_WORK_RUN_CLAUDE_ON_DISPATCH=false）", true
	}
	if strings.EqualFold(agent.Provider, "codex") && !a.cfg.RunCodexOnDispatch {
		return "当前环境未启用 Codex 执行（AUTO_WORK_RUN_CODEX_ON_DISPATCH=false）", true
	}
	return "", false
}

func (a *App) FinishRun(ctx context.Context, req FinishRunRequest) error {
	runStatus, taskStatus, err := validateFinishStatus(req.Status, req.TaskStatus)
	if err != nil {
		return err
	}
	return a.dispatcher.MarkRunFinished(ctx, req.RunID, runStatus, taskStatus, req.Summary, req.Details, req.ExitCode)
}

func (a *App) resolveDispatchAgent(ctx context.Context, agentID, projectID string) (*domain.Agent, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID != "" {
		return a.agentRepo.GetByID(ctx, agentID)
	}

	provider := "claude"
	if strings.TrimSpace(projectID) != "" {
		if project, getErr := a.projectRepo.GetByID(ctx, strings.TrimSpace(projectID)); getErr == nil {
			if strings.TrimSpace(project.DefaultProvider) != "" {
				provider = strings.ToLower(strings.TrimSpace(project.DefaultProvider))
			}
		}
	}

	return a.agentRepo.GetByID(ctx, defaultAgentIDForProvider(provider))
}

func (a *App) startProviderRun(ctx context.Context, agent domain.Agent, run domain.Run, task domain.Task) (int, error) {
	switch strings.ToLower(strings.TrimSpace(agent.Provider)) {
	case "claude":
		return a.claudeRunner.Start(ctx, run, task, agent)
	case "codex":
		return a.codexRunner.Start(ctx, run, task, agent)
	default:
		return 0, fmt.Errorf("unsupported provider: %s", agent.Provider)
	}
}

func (a *App) stopProviderRun(ctx context.Context, provider, runID string) error {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude":
		return a.claudeRunner.Stop(ctx, runID)
	case "codex":
		return a.codexRunner.Stop(ctx, runID)
	default:
		return errors.New("unsupported provider")
	}
}

func (a *App) ListRunningRuns(ctx context.Context, projectID string, limit int) ([]RunningRunView, error) {
	items, err := a.runRepo.ListRunning(ctx, strings.TrimSpace(projectID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunningRunView, 0, len(items))
	for _, v := range items {
		out = append(out, RunningRunView{
			RunID:       v.RunID,
			TaskID:      v.TaskID,
			TaskTitle:   v.TaskTitle,
			ProjectID:   v.ProjectID,
			AgentID:     v.AgentID,
			PID:         v.PID,
			Status:      string(v.Status),
			StartedAt:   v.StartedAt,
			HeartbeatAt: v.HeartbeatAt,
		})
	}
	return out, nil
}

func (a *App) ListRunLogs(ctx context.Context, runID string, limit int) ([]RunLogEventView, error) {
	events, err := a.eventRepo.ListByRun(ctx, strings.TrimSpace(runID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunLogEventView, 0, len(events))
	for _, e := range events {
		out = append(out, RunLogEventView{
			ID:      e.ID,
			RunID:   e.RunID,
			TS:      e.TS,
			Kind:    e.Kind,
			Payload: e.Payload,
		})
	}
	return out, nil
}

func (a *App) ListSystemLogs(ctx context.Context, projectID string, limit int) ([]SystemLogView, error) {
	events, err := a.eventRepo.ListRecent(ctx, strings.TrimSpace(projectID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]SystemLogView, 0, len(events))
	for _, e := range events {
		out = append(out, SystemLogView{
			ID:        e.ID,
			RunID:     e.RunID,
			TaskID:    e.TaskID,
			TaskTitle: e.TaskTitle,
			ProjectID: e.ProjectID,
			TS:        e.TS,
			Kind:      e.Kind,
			Payload:   e.Payload,
		})
	}
	return out, nil
}

func (a *App) GetTaskLatestRun(ctx context.Context, taskID string) (*TaskLatestRunView, error) {
	runs, err := a.runRepo.ListByTask(ctx, strings.TrimSpace(taskID), 1)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	r := runs[0]
	return &TaskLatestRunView{
		RunID:         r.ID,
		Status:        string(r.Status),
		Attempt:       r.Attempt,
		StartedAt:     r.StartedAt,
		FinishedAt:    r.FinishedAt,
		ExitCode:      r.ExitCode,
		ResultSummary: r.ResultSummary,
		ResultDetails: r.ResultDetails,
	}, nil
}

func (a *App) GetTaskDetail(ctx context.Context, taskID string) (*TaskDetailView, error) {
	task, err := a.taskRepo.GetByID(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}

	runs, err := a.runRepo.ListByTask(ctx, task.ID, 50)
	if err != nil {
		return nil, err
	}

	items := make([]TaskRunHistoryView, 0, len(runs))
	for _, r := range runs {
		items = append(items, TaskRunHistoryView{
			RunID:         r.ID,
			Status:        string(r.Status),
			Attempt:       r.Attempt,
			StartedAt:     r.StartedAt,
			FinishedAt:    r.FinishedAt,
			ExitCode:      r.ExitCode,
			ResultSummary: r.ResultSummary,
			ResultDetails: r.ResultDetails,
		})
	}

	return &TaskDetailView{
		Task: task,
		Runs: items,
	}, nil
}

func (a *App) MCPStatus(ctx context.Context) (*MCPStatusView, error) {
	if !a.cfg.EnableMCPCallback {
		return &MCPStatusView{
			Enabled: false,
			State:   "disabled",
			Message: "MCP 回调未启用",
		}, nil
	}

	type latestRun struct {
		RunID      string
		Status     string
		UpdatedAt  time.Time
		Summary    string
		Details    string
		TaskStatus string
	}

	var latest latestRun
	var (
		updatedAt sql.NullTime
		createdAt time.Time
	)
	row := a.db.QueryRowContext(ctx, `
SELECT r.id,
       r.status,
       r.updated_at,
       r.created_at,
       COALESCE(r.result_summary, ''),
       COALESCE(r.result_details, ''),
       COALESCE(t.status, '')
FROM runs r
JOIN tasks t ON t.id = r.task_id
ORDER BY r.created_at DESC
LIMIT 1`)
	if err := row.Scan(&latest.RunID, &latest.Status, &updatedAt, &createdAt, &latest.Summary, &latest.Details, &latest.TaskStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &MCPStatusView{
				Enabled: true,
				State:   "unknown",
				Message: "暂无运行记录",
			}, nil
		}
		return nil, err
	}
	if updatedAt.Valid {
		latest.UpdatedAt = updatedAt.Time
	} else {
		latest.UpdatedAt = createdAt
	}

	if ok, err := a.runHasEvent(ctx, latest.RunID, "mcp.report_result.applied"); err == nil && ok {
		return &MCPStatusView{
			Enabled:   true,
			State:     "connected",
			Message:   "最近一次 MCP 回调成功",
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}

	if reason, ok, err := a.latestEventPayload(ctx, latest.RunID, "system.mcp_failure"); err == nil && ok {
		return &MCPStatusView{
			Enabled:   true,
			State:     "failed",
			Message:   fmt.Sprintf("MCP 连接异常：%s", reason),
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}
	if reason, ok, err := a.latestEventPayload(ctx, latest.RunID, "system.mcp_fallback"); err == nil && ok {
		return &MCPStatusView{
			Enabled:   true,
			State:     "failed",
			Message:   fmt.Sprintf("MCP 回调缺失，已启用兜底：%s", reason),
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}

	if latest.Status == string(domain.RunRunning) {
		if ok, err := a.runHasMCPConnectedInit(ctx, latest.RunID); err == nil && ok {
			return &MCPStatusView{
				Enabled:   true,
				State:     "connected",
				Message:   "当前运行实例 MCP 已连接",
				RunID:     latest.RunID,
				UpdatedAt: &latest.UpdatedAt,
			}, nil
		}
		if ok, err := a.runHasMCPFailedInit(ctx, latest.RunID); err == nil && ok {
			msg := "当前运行实例 MCP 初始化失败"
			if reason := strings.TrimSpace(a.findMCPFailureReason(latest.RunID)); reason != "" {
				msg = fmt.Sprintf("当前运行实例 MCP 初始化失败：%s", reason)
			}
			return &MCPStatusView{
				Enabled:   true,
				State:     "failed",
				Message:   msg,
				RunID:     latest.RunID,
				UpdatedAt: &latest.UpdatedAt,
			}, nil
		}
		return &MCPStatusView{
			Enabled:   true,
			State:     "unknown",
			Message:   "运行中，等待 MCP 初始化/回调",
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}
	if latest.Status == string(domain.RunNeedsInput) || latest.TaskStatus == string(domain.TaskBlocked) {
		msg := "任务等待人工输入，自动派发已中断"
		if reason := strings.TrimSpace(a.findNeedsInputReason(latest.RunID)); reason != "" {
			msg = fmt.Sprintf("%s：%s", msg, reason)
		}
		return &MCPStatusView{
			Enabled:   true,
			State:     "failed",
			Message:   msg,
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}

	if strings.Contains(latest.Summary, "without mcp report_result callback") {
		msg := "上一次运行未收到 MCP report_result 回调"
		if reason := strings.TrimSpace(a.findMCPFailureReason(latest.RunID)); reason != "" {
			msg = fmt.Sprintf("%s：%s", msg, reason)
		}
		return &MCPStatusView{
			Enabled:   true,
			State:     "failed",
			Message:   msg,
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}
	if strings.Contains(latest.Details, "mcp_reason=") {
		return &MCPStatusView{
			Enabled:   true,
			State:     "failed",
			Message:   latest.Details,
			RunID:     latest.RunID,
			UpdatedAt: &latest.UpdatedAt,
		}, nil
	}

	return &MCPStatusView{
		Enabled:   true,
		State:     "unknown",
		Message:   "暂无可判定的 MCP 状态",
		RunID:     latest.RunID,
		UpdatedAt: &latest.UpdatedAt,
	}, nil
}

func (a *App) ensureDefaultAgents(ctx context.Context) error {
	now := time.Now().UTC()
	if err := a.agentRepo.Upsert(ctx, &domain.Agent{
		ID:          defaultClaudeAgentID,
		Name:        "Claude Default Agent",
		Provider:    "claude",
		Enabled:     true,
		Concurrency: 1,
		LastSeenAt:  &now,
	}); err != nil {
		return err
	}
	return a.agentRepo.Upsert(ctx, &domain.Agent{
		ID:          defaultCodexAgentID,
		Name:        "Codex Default Agent",
		Provider:    "codex",
		Enabled:     true,
		Concurrency: 1,
		LastSeenAt:  &now,
	})
}

func (a *App) onClaudeLine(runID, stream, line string) {
	a.onRunnerLine("claude", runID, stream, line)
}

func (a *App) onCodexLine(runID, stream, line string) {
	a.onRunnerLine("codex", runID, stream, line)
}

func (a *App) onRunnerLine(provider, runID, stream, line string) {
	kindPrefix := strings.ToLower(strings.TrimSpace(provider))
	if kindPrefix == "" {
		kindPrefix = "runner"
	}
	_ = a.eventRepo.Append(context.Background(), runID, kindPrefix+"."+stream, line)
	if reason := runsignal.ExtractNeedsInputReason(line); strings.TrimSpace(reason) != "" {
		_ = a.eventRepo.Append(context.Background(), runID, "system.needs_input", reason)
		_ = a.stopProviderRun(context.Background(), provider, runID)
	}
	if reason := extractMCPRuntimeFailureReason(line); strings.TrimSpace(reason) != "" {
		_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
	}
	_ = a.runRepo.UpdateHeartbeat(context.Background(), runID, time.Now().UTC())
}

func (a *App) onClaudeExit(runID string, exitCode int, runErr error) {
	a.onRunnerExit("claude", runID, exitCode, runErr)
}

func (a *App) onCodexExit(runID string, exitCode int, runErr error) {
	a.onRunnerExit("codex", runID, exitCode, runErr)
}

func (a *App) onRunnerExit(provider, runID string, exitCode int, runErr error) {
	run, err := a.runRepo.GetByID(context.Background(), runID)
	if err != nil {
		a.log.Errorf("load run on exit failed run_id=%s err=%v", runID, err)
		return
	}
	if run.Status != domain.RunRunning {
		a.log.Debugf("ignore runner exit for non-running run run_id=%s status=%s", runID, run.Status)
		return
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		name = "runner"
	}
	if reason := strings.TrimSpace(a.findNeedsInputReason(runID)); reason != "" {
		summary := fmt.Sprintf("%s 需要人工输入", name)
		details := fmt.Sprintf("needs_input_reason=%s", reason)
		_ = a.dispatcher.MarkRunFinished(context.Background(), runID, domain.RunNeedsInput, domain.TaskBlocked, summary, details, &exitCode)
		a.log.Warnf("runner needs input run_id=%s provider=%s reason=%s", runID, name, reason)
		return
	}

	summary := fmt.Sprintf("%s process exited", name)
	details := fmt.Sprintf("exit_code=%d", exitCode)
	runStatus := domain.RunDone
	taskStatus := domain.TaskDone
	if runErr != nil || exitCode != 0 {
		runStatus = domain.RunFailed
		taskStatus = domain.TaskFailed
		summary = fmt.Sprintf("%s process failed", name)
		if runErr != nil {
			details = fmt.Sprintf("%s, err=%v", details, runErr)
		}
		_ = a.appendRunFailureLog(runID, summary, details)
		a.log.Errorf("runner process failed run_id=%s provider=%s exit_code=%d err=%v", runID, name, exitCode, runErr)
	} else if a.cfg.RequireMCPCallback && strings.EqualFold(name, "claude") {
		if a.tryFinalizeRunWithoutMCP(runID, exitCode) {
			return
		}
		runStatus = domain.RunFailed
		taskStatus = domain.TaskFailed
		summary = "claude exited without mcp report_result callback"
		details = "process exit 0 but no MCP report was received"
		if reason := a.findMCPFailureReason(runID); strings.TrimSpace(reason) != "" {
			_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
			details = fmt.Sprintf("%s; mcp_reason=%s", details, reason)
		}
		_ = a.appendRunFailureLog(runID, summary, details)
	} else if a.cfg.RequireMCPCallback {
		if ok, err := a.runHasEvent(context.Background(), runID, "mcp.report_result.applied"); err == nil && !ok {
			runStatus = domain.RunFailed
			taskStatus = domain.TaskFailed
			summary = fmt.Sprintf("%s exited without mcp report_result callback", name)
			details = "process exit 0 but no MCP report was received"
			if reason := a.findMCPFailureReason(runID); strings.TrimSpace(reason) != "" {
				_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_failure", reason)
				details = fmt.Sprintf("%s; mcp_reason=%s", details, reason)
			}
			_ = a.appendRunFailureLog(runID, summary, details)
			a.log.Errorf("runner exited without mcp callback run_id=%s provider=%s", runID, name)
		}
	}
	_ = a.dispatcher.MarkRunFinished(context.Background(), runID, runStatus, taskStatus, summary, details, &exitCode)
	a.log.Infof("runner process finished run_id=%s provider=%s run_status=%s task_status=%s exit_code=%d", runID, name, runStatus, taskStatus, exitCode)
}

func (a *App) tryFinalizeRunWithoutMCP(runID string, exitCode int) bool {
	result, ok := a.findLatestClaudeResultEvent(runID)
	if !ok {
		return false
	}
	if result.hasMCPPermissionDenied() {
		return false
	}
	if !result.isSuccess() {
		return false
	}
	text := strings.TrimSpace(result.Result)
	if text == "" {
		return false
	}

	summary := "MCP 回调缺失，已按 Claude 最终结果兜底完成"
	details := fmt.Sprintf("fallback_from=claude.result; exit_code=%d\n\n%s", exitCode, trimText(text, 12000))
	_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_fallback", "finalized from Claude result event without MCP callback")
	if err := a.dispatcher.MarkRunFinished(context.Background(), runID, domain.RunDone, domain.TaskDone, summary, details, &exitCode); err != nil {
		_ = a.eventRepo.Append(context.Background(), runID, "system.mcp_fallback_error", err.Error())
		return false
	}
	return true
}

func (a *App) appendRunFailureLog(runID, summary, details string) error {
	payload := fmt.Sprintf("%s | %s", strings.TrimSpace(summary), strings.TrimSpace(details))
	if strings.TrimSpace(payload) == "|" {
		payload = "failure occurred"
	}
	return a.eventRepo.Append(context.Background(), runID, "system.failure", payload)
}

func (a *App) startAutoDispatchLoop() {
	loopCtx, cancel := context.WithCancel(context.Background())
	a.loopCancel = cancel
	a.loopDone = make(chan struct{})

	go func() {
		defer close(a.loopDone)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				_ = a.recoverDeadRunningRuns(context.Background(), "recovered dead running run during auto-dispatch loop")
				projectIDs, err := a.projectRepo.ListAutoDispatchEnabledProjectIDs(context.Background(), 50)
				if err != nil || len(projectIDs) == 0 {
					continue
				}
				for _, projectID := range projectIDs {
					resp, dispatchErr := a.DispatchOnce(context.Background(), "", projectID)
					if dispatchErr != nil {
						continue
					}
					if resp != nil && resp.Claimed {
						break
					}
				}
			}
		}
	}()
}

func (a *App) triggerAutoDispatch(projectID string) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	_, _ = a.DispatchOnce(context.Background(), "", strings.TrimSpace(projectID))
}

func (a *App) releaseDueRetryTasks(ctx context.Context, limit int) int {
	ids, err := a.taskRepo.ListDueRetryTaskIDs(ctx, time.Now().UTC(), limit)
	if err != nil {
		return 0
	}
	released := 0
	for _, taskID := range ids {
		if err := a.taskRepo.PromoteFailedRetryToPending(ctx, taskID); err != nil {
			if errors.Is(err, repository.ErrTaskNotFound) {
				continue
			}
			continue
		}
		released++
	}
	return released
}

func (a *App) findMCPFailureReason(runID string) string {
	if provider, ok := a.runProvider(runID); ok && provider == "claude" {
		debugPath := a.claudeRunner.DebugLogPath(runID)
		if reason := findMCPFailureInDebugFile(debugPath); strings.TrimSpace(reason) != "" {
			return reason
		}
	}

	events, err := a.eventRepo.ListByRun(context.Background(), runID, 200)
	if err != nil {
		return ""
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		if strings.Contains(e.Payload, `MCP server "auto-work" Connection failed:`) {
			parts := strings.SplitN(e.Payload, `Connection failed:`, 2)
			if len(parts) == 2 {
				if reason := strings.TrimSpace(parts[1]); reason != "" {
					return reason
				}
			}
			return `MCP server "auto-work" connection failed`
		}
		if reason := extractMCPRuntimeFailureReason(e.Payload); strings.TrimSpace(reason) != "" {
			return reason
		}
		if status, ok := extractMCPInitServerStatus(e.Payload, "auto-work"); ok && isMCPServerFailedStatus(status) {
			return fmt.Sprintf("auto-work MCP server status=%s during initialize", status)
		}
	}
	return ""
}

func (a *App) findNeedsInputReason(runID string) string {
	reason, ok, err := a.latestEventPayload(context.Background(), runID, "system.needs_input")
	if err == nil && ok && strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}

	events, err := a.eventRepo.ListByRun(context.Background(), runID, 300)
	if err != nil {
		return ""
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		if reason := runsignal.ExtractNeedsInputReason(e.Payload); strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
	}
	return ""
}

type claudeResultEvent struct {
	Type              string `json:"type"`
	Subtype           string `json:"subtype"`
	IsError           bool   `json:"is_error"`
	Result            string `json:"result"`
	PermissionDenials []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
}

func (e claudeResultEvent) isSuccess() bool {
	return strings.EqualFold(strings.TrimSpace(e.Subtype), "success") && !e.IsError
}

func (e claudeResultEvent) hasMCPPermissionDenied() bool {
	for _, item := range e.PermissionDenials {
		name := strings.TrimSpace(item.ToolName)
		if strings.HasPrefix(name, "mcp__auto-work__") {
			return true
		}
	}
	return false
}

func (a *App) findLatestClaudeResultEvent(runID string) (claudeResultEvent, bool) {
	events, err := a.eventRepo.ListByRun(context.Background(), runID, 400)
	if err != nil {
		return claudeResultEvent{}, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != "claude.stdout" {
			continue
		}
		parsed, ok := parseClaudeResultEvent(e.Payload)
		if !ok {
			continue
		}
		return parsed, true
	}
	return claudeResultEvent{}, false
}

func parseClaudeResultEvent(payload string) (claudeResultEvent, bool) {
	line := strings.TrimSpace(payload)
	if line == "" {
		return claudeResultEvent{}, false
	}
	var out claudeResultEvent
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		return claudeResultEvent{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(out.Type), "result") {
		return claudeResultEvent{}, false
	}
	return out, true
}

func extractMCPPermissionDeniedReason(payload string) string {
	p := strings.TrimSpace(payload)
	if p == "" {
		return ""
	}
	if !strings.Contains(p, `"permission_denials"`) {
		return ""
	}
	if strings.Contains(p, `"tool_name":"mcp__auto-work__`) && strings.Contains(p, "report_result") {
		return "MCP tool auto-work.report_result permission denied by Claude Code"
	}
	if strings.Contains(p, `"mcp__auto-work__`) {
		return "MCP tool permission denied by Claude Code"
	}
	return ""
}

func extractMCPRuntimeFailureReason(payload string) string {
	p := strings.TrimSpace(payload)
	if p == "" {
		return ""
	}
	if reason := extractMCPPermissionDeniedReason(p); reason != "" {
		return reason
	}
	if strings.Contains(p, `MCP server "auto-work" Connection failed:`) {
		parts := strings.SplitN(p, `Connection failed:`, 2)
		if len(parts) == 2 {
			if reason := strings.TrimSpace(parts[1]); reason != "" {
				return reason
			}
		}
		return `MCP server "auto-work" connection failed`
	}
	if status, ok := extractMCPInitServerStatus(p, "auto-work"); ok && isMCPServerFailedStatus(status) {
		return fmt.Sprintf("auto-work MCP server status=%s during initialize", status)
	}
	if server, ok := parseUnknownMCPServerName(p); ok {
		if strings.EqualFold(server, "todo") {
			return "unknown MCP server 'todo'（请使用 auto-work）"
		}
		if strings.TrimSpace(server) != "" {
			return fmt.Sprintf("unknown MCP server '%s'", strings.TrimSpace(server))
		}
		return "unknown MCP server"
	}
	return ""
}

func parseUnknownMCPServerName(payload string) (string, bool) {
	line := strings.TrimSpace(payload)
	if line == "" {
		return "", false
	}
	lower := strings.ToLower(line)
	key := "unknown mcp server "
	idx := strings.Index(lower, key)
	if idx < 0 {
		return "", false
	}
	tail := strings.TrimSpace(line[idx+len(key):])
	if tail == "" {
		return "", true
	}
	if tail[0] == '\'' || tail[0] == '"' {
		quote := tail[0]
		if end := strings.IndexByte(tail[1:], quote); end >= 0 {
			return strings.TrimSpace(tail[1 : 1+end]), true
		}
	}
	parts := strings.Fields(tail)
	if len(parts) == 0 {
		return "", true
	}
	name := strings.Trim(parts[0], `"'.,;:()[]{}<>`)
	return strings.TrimSpace(name), true
}

func extractMCPInitServerStatus(payload, serverName string) (string, bool) {
	line := strings.TrimSpace(payload)
	name := strings.ToLower(strings.TrimSpace(serverName))
	if line == "" || name == "" {
		return "", false
	}

	var root any
	if err := json.Unmarshal([]byte(line), &root); err != nil {
		return "", false
	}
	return findMCPServerStatus(root, name)
}

func findMCPServerStatus(node any, serverName string) (string, bool) {
	switch typed := node.(type) {
	case map[string]any:
		if rawServers, ok := typed["mcp_servers"]; ok {
			if status, ok := readMCPServerStatus(rawServers, serverName); ok {
				return status, true
			}
		}
		for _, child := range typed {
			if status, ok := findMCPServerStatus(child, serverName); ok {
				return status, true
			}
		}
	case []any:
		for _, item := range typed {
			if status, ok := findMCPServerStatus(item, serverName); ok {
				return status, true
			}
		}
	}
	return "", false
}

func readMCPServerStatus(raw any, serverName string) (string, bool) {
	servers, ok := raw.([]any)
	if !ok {
		return "", false
	}
	for _, item := range servers {
		server, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(anyToString(server["name"])))
		if name == "" || name != serverName {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(anyToString(server["status"])))
		if status == "" {
			continue
		}
		return status, true
	}
	return "", false
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return fmt.Sprint(v)
}

func isMCPServerConnectedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "connected", "ok", "ready":
		return true
	default:
		return false
	}
}

func isMCPServerFailedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "disconnected", "timeout":
		return true
	default:
		return false
	}
}

func trimText(in string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	val := strings.TrimSpace(in)
	r := []rune(val)
	if len(r) <= maxRunes {
		return val
	}
	return strings.TrimSpace(string(r[:maxRunes])) + "...(truncated)"
}

func (a *App) recoverDeadRunningRuns(ctx context.Context, reason string) error {
	items, err := a.runRepo.ListRunning(ctx, "", 500)
	if err != nil {
		a.log.Errorf("list running runs for recovery failed: %v", err)
		return err
	}
	for _, run := range items {
		if run.PID == nil || *run.PID <= 0 {
			exitCode := 137
			_ = a.appendRunFailureLog(run.RunID, "orphan running run recovered", reason)
			_ = a.dispatcher.MarkRunFinished(context.Background(), run.RunID, domain.RunFailed, domain.TaskFailed, "orphan running run recovered", reason, &exitCode)
			a.log.Warnf("recover orphan running run run_id=%s reason=%s", run.RunID, strings.TrimSpace(reason))
			continue
		}
		if processExists(*run.PID) {
			continue
		}
		details := fmt.Sprintf("%s; pid=%d not alive", strings.TrimSpace(reason), *run.PID)
		exitCode := 137
		_ = a.appendRunFailureLog(run.RunID, "dead running run recovered", details)
		_ = a.dispatcher.MarkRunFinished(context.Background(), run.RunID, domain.RunFailed, domain.TaskFailed, "dead running run recovered", details, &exitCode)
		a.log.Warnf("recover dead running run run_id=%s pid=%d", run.RunID, *run.PID)
	}
	return nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if p == nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func findMCPFailureInDebugFile(path string) string {
	f, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	defer f.Close()

	var reason string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `MCP server "auto-work" Connection failed:`) {
			parts := strings.SplitN(line, `Connection failed:`, 2)
			if len(parts) == 2 {
				reason = strings.TrimSpace(parts[1])
			}
		}
		if strings.Contains(line, `MCP server "auto-work": Connection timeout triggered`) {
			reason = "MCP server connection timeout"
		}
	}
	return strings.TrimSpace(reason)
}

func validateFinishStatus(runStatus, taskStatus string) (domain.RunStatus, domain.TaskStatus, error) {
	rs := domain.RunStatus(runStatus)
	ts := domain.TaskStatus(taskStatus)

	switch rs {
	case domain.RunDone, domain.RunFailed, domain.RunNeedsInput, domain.RunCancelled:
	default:
		return "", "", errors.New("invalid run status")
	}

	switch ts {
	case domain.TaskDone, domain.TaskFailed, domain.TaskBlocked:
	default:
		return "", "", errors.New("invalid task status")
	}

	return rs, ts, nil
}

func (a *App) runHasEvent(ctx context.Context, runID, kind string) (bool, error) {
	row := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM run_events WHERE run_id = ? AND kind = ?`, strings.TrimSpace(runID), strings.TrimSpace(kind))
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) latestEventPayload(ctx context.Context, runID, kind string) (string, bool, error) {
	row := a.db.QueryRowContext(ctx, `
SELECT payload
FROM run_events
WHERE run_id = ? AND kind = ?
ORDER BY ts DESC
LIMIT 1`, strings.TrimSpace(runID), strings.TrimSpace(kind))
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(payload), true, nil
}

func (a *App) runHasMCPConnectedInit(ctx context.Context, runID string) (bool, error) {
	events, err := a.eventRepo.ListByRun(ctx, strings.TrimSpace(runID), 400)
	if err != nil {
		return false, err
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		status, ok := extractMCPInitServerStatus(e.Payload, "auto-work")
		if !ok {
			continue
		}
		if isMCPServerConnectedStatus(status) {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) runHasMCPFailedInit(ctx context.Context, runID string) (bool, error) {
	events, err := a.eventRepo.ListByRun(ctx, strings.TrimSpace(runID), 400)
	if err != nil {
		return false, err
	}
	provider, _ := a.runProvider(runID)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind == "system.mcp_failure" {
			return true, nil
		}
		if !isProviderStdoutKind(e.Kind, provider) {
			continue
		}
		if status, ok := extractMCPInitServerStatus(e.Payload, "auto-work"); ok && isMCPServerFailedStatus(status) {
			return true, nil
		}
		if strings.Contains(e.Payload, `MCP server "auto-work" Connection failed:`) {
			return true, nil
		}
		if strings.Contains(strings.ToLower(e.Payload), "unknown mcp server") {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) runProvider(runID string) (string, bool) {
	row := a.db.QueryRowContext(context.Background(), `
SELECT t.provider
FROM runs r
JOIN tasks t ON t.id = r.task_id
WHERE r.id = ?
LIMIT 1`, strings.TrimSpace(runID))
	var provider string
	if err := row.Scan(&provider); err != nil {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(provider)), true
}

func isProviderStdoutKind(kind, provider string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return strings.HasSuffix(kind, ".stdout")
	}
	return kind == provider+".stdout"
}

func mapGlobalSettings(settings *repository.GlobalSettings) *GlobalSettingsView {
	if settings == nil {
		return nil
	}
	return &GlobalSettingsView{
		TelegramEnabled:     settings.TelegramEnabled,
		TelegramBotToken:    settings.TelegramBotToken,
		TelegramChatIDs:     settings.TelegramChatIDs,
		TelegramPollTimeout: settings.TelegramPollTimeout,
		TelegramProxyURL:    settings.TelegramProxyURL,
		SystemPrompt:        settings.SystemPrompt,
		UpdatedAt:           settings.UpdatedAt,
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

const (
	defaultClaudeAgentID = "agent-claude-default"
	defaultCodexAgentID  = "agent-codex-default"
)

func defaultAgentIDForProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return defaultCodexAgentID
	default:
		return defaultClaudeAgentID
	}
}

func NewID() string {
	return uuid.NewString()
}
