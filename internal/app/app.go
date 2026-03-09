package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	mcpCheckMu               sync.Mutex
	mcpProvisionFailures     map[string]mcpProvisionFailure
	mcpCancel                context.CancelFunc
	mcpDone                  chan struct{}
	loopCancel               context.CancelFunc
	loopDone                 chan struct{}
	log                      *applog.Logger
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
		cfg:                  cfg,
		db:                   sqlDB,
		taskRepo:             taskRepo,
		projectRepo:          projectRepo,
		settingsRepo:         settingsRepo,
		agentRepo:            agentRepo,
		runRepo:              runRepo,
		eventRepo:            eventRepo,
		projectSvc:           projectservice.NewService(projectRepo),
		taskSvc:              taskservice.NewService(taskRepo, projectRepo),
		dispatcher:           scheduler.NewDispatcher(sqlDB),
		runFrontendBaseline:  make(map[string]frontendRunBaseline),
		mcpProvisionFailures: make(map[string]mcpProvisionFailure),
		log:                  appLogger,
	}
	a.log.Infof("startup begin db=%s log=%s", strings.TrimSpace(cfg.DatabasePath), appLogger.Path())
	a.dispatcher.SetRunFinishedHook(a.onRunFinishedTelegramNotify)

	mcpBaseURL := strings.TrimSpace(cfg.MCPHTTPURL)
	if cfg.EnableMCPCallback {
		mcpBaseURL, err = a.startMCPHTTPServer()
		if err != nil {
			_ = sqlDB.Close()
			_ = appLogger.Close()
			return nil, err
		}
	}
	a.cfg.MCPHTTPURL = mcpBaseURL

	a.claudeRunner = clauderunner.New(clauderunner.Options{
		Binary:            cfg.ClaudeBinary,
		Model:             cfg.ClaudeModel,
		Workdir:           cfg.WorkspacePath,
		DebugDir:          filepath.Join(os.TempDir(), "auto-work", "claude-debug"),
		AllowedTools:      cfg.ClaudeAllowedTools,
		PermissionMode:    cfg.ClaudePermissionMode,
		EnableMCPCallback: cfg.EnableMCPCallback,
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
