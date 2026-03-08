package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"

	"auto-work/internal/domain"
	"auto-work/internal/repository"
	projectservice "auto-work/internal/service/project"
)

const (
	defaultPollTimeout  = 30
	defaultRecentLimit  = 5
	maxRecentLimit      = 20
	defaultProjectLimit = 8
	maxProjectLimit     = 20
	defaultQueueLimit   = 5
	maxQueueLimit       = 20
	projectPreviewLimit = 3
	defaultLogLimit     = 20
	maxLogLimit         = 80
	maxTelegramTextLen  = 3800
	maxTelegramCaption  = 900
)

type Options struct {
	Token                   string
	ChatIDs                 []int64
	PollTimeout             int
	ProxyURL                string
	TaskRepo                *repository.TaskRepository
	RunRepo                 *repository.RunRepository
	EventRepo               *repository.RunEventRepository
	ProjectRepo             *repository.ProjectRepository
	ErrorReporter           func(string, ...any)
	IncomingMessageReporter func(IncomingMessage)
	DispatchTask            func(context.Context, string) (*DispatchTaskResult, error)
}

type IncomingMessage struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type,omitempty"`
	From     string `json:"from,omitempty"`
	Command  string `json:"command,omitempty"`
	Text     string `json:"text,omitempty"`
}

type DispatchTaskResult struct {
	Claimed bool
	RunID   string
	TaskID  string
	Message string
}

type Service struct {
	bot              *tgbotapi.BotAPI
	taskRepo         *repository.TaskRepository
	runRepo          *repository.RunRepository
	eventRepo        *repository.RunEventRepository
	projectRepo      *repository.ProjectRepository
	projectSvc       *projectservice.Service
	allowedChatID    map[int64]struct{}
	pollTimeout      int
	errorf           func(string, ...any)
	incomingReporter func(IncomingMessage)
	dispatchTask     func(context.Context, string) (*DispatchTaskResult, error)

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func New(opts Options) (*Service, error) {
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return nil, errors.New("telegram token is empty")
	}
	if opts.TaskRepo == nil || opts.RunRepo == nil || opts.EventRepo == nil || opts.ProjectRepo == nil {
		return nil, errors.New("telegram repositories are required")
	}
	client, err := buildHTTPClient(strings.TrimSpace(opts.ProxyURL))
	if err != nil {
		return nil, err
	}
	bot, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, client)
	if err != nil {
		return nil, err
	}

	pollTimeout := opts.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultPollTimeout
	}
	allowed := make(map[int64]struct{}, len(opts.ChatIDs))
	for _, id := range opts.ChatIDs {
		allowed[id] = struct{}{}
	}
	logf := opts.ErrorReporter
	if logf == nil {
		logf = func(string, ...any) {}
	}

	return &Service{
		bot:              bot,
		taskRepo:         opts.TaskRepo,
		runRepo:          opts.RunRepo,
		eventRepo:        opts.EventRepo,
		projectRepo:      opts.ProjectRepo,
		projectSvc:       projectservice.NewService(opts.ProjectRepo),
		allowedChatID:    allowed,
		pollTimeout:      pollTimeout,
		errorf:           logf,
		incomingReporter: opts.IncomingMessageReporter,
		dispatchTask:     opts.DispatchTask,
	}, nil
}

func buildHTTPClient(proxyURL string) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram proxy url: %w", err)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Transport: tr,
		Timeout:   75 * time.Second,
	}, nil
}

func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()

	go s.loop(loopCtx)
	go s.sendStartupNotification()
}

func (s *Service) Stop(ctx context.Context) {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if s.bot != nil {
		s.bot.StopReceivingUpdates()
	}
	if done == nil {
		return
	}

	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.done)

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = s.pollTimeout
	updates := s.bot.GetUpdatesChan(updateCfg)
	for {
		select {
		case <-ctx.Done():
			return
		case upd, ok := <-updates:
			if !ok {
				return
			}
			s.handleUpdate(ctx, upd)
		}
	}
}

func (s *Service) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	msg := upd.Message
	if msg == nil {
		return
	}
	s.logIncomingMessage(msg)
	s.reportIncomingMessage(msg)
	if !msg.IsCommand() {
		return
	}

	chatID := msg.Chat.ID
	if !s.chatAllowed(chatID) {
		s.sendText(chatID, "未授权的 Chat ID，已拒绝请求。")
		return
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Command()))
	args := strings.TrimSpace(msg.CommandArguments())

	var response string
	switch cmd {
	case "start", "help":
		response = helpText()
	case "addproject", "newproject":
		response = s.handleCreateProject(ctx, args)
	case "setprovider", "provider":
		response = s.handleSetProjectProvider(ctx, args)
	case "projects":
		response = s.handleProjects(ctx, args)
	case "project":
		response = s.handleProject(ctx, args)
	case "recent":
		response = s.handleRecent(ctx, args)
	case "pending", "queue":
		response = s.handlePending(ctx, args)
	case "failed":
		response = s.handleFailed(ctx, args)
	case "running":
		response = s.handleRunning(ctx, args)
	case "autodisp_on":
		response = s.handleSetAutoDispatch(ctx, args, true)
	case "autodisp_off":
		response = s.handleSetAutoDispatch(ctx, args, false)
	case "task", "status":
		response = s.handleTaskStatus(ctx, args)
	case "logs":
		response = s.handleLogs(ctx, args)
	case "tasklogs", "latestlogs":
		response = s.handleTaskLatestLogs(ctx, args)
	case "addtask", "newtask":
		response = s.handleCreateTask(ctx, args)
	case "dispatch":
		response = s.handleDispatchTask(ctx, args)
	default:
		response = "未知命令。发送 /help 查看可用指令。"
	}
	s.sendText(chatID, response)
}

func (s *Service) logIncomingMessage(msg *tgbotapi.Message) {
	info := buildIncomingMessageInfo(msg)
	if info == nil {
		return
	}
	s.errorf("%s", formatIncomingMessageLog(info))
}

func (s *Service) reportIncomingMessage(msg *tgbotapi.Message) {
	if s.incomingReporter == nil {
		return
	}
	info := buildIncomingMessageInfo(msg)
	if info == nil {
		return
	}
	s.incomingReporter(*info)
}

func buildIncomingMessageLog(msg *tgbotapi.Message) string {
	return formatIncomingMessageLog(buildIncomingMessageInfo(msg))
}

func formatIncomingMessageLog(msg *IncomingMessage) string {
	if msg == nil {
		return "incoming message: <nil>"
	}
	return fmt.Sprintf(
		"incoming message: chat_id=%d chat_type=%s from=%s command=%s text=%q",
		msg.ChatID,
		msg.ChatType,
		msg.From,
		msg.Command,
		msg.Text,
	)
}

func buildIncomingMessageInfo(msg *tgbotapi.Message) *IncomingMessage {
	if msg == nil {
		return nil
	}
	chatID := int64(0)
	chatType := ""
	if msg.Chat != nil {
		chatID = msg.Chat.ID
		chatType = strings.TrimSpace(msg.Chat.Type)
	}
	from := "unknown"
	if msg.From != nil {
		if username := strings.TrimSpace(msg.From.UserName); username != "" {
			from = "@" + username
		} else {
			fullName := strings.TrimSpace(strings.TrimSpace(msg.From.FirstName) + " " + strings.TrimSpace(msg.From.LastName))
			if fullName != "" {
				from = fullName
			}
		}
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}
	if text == "" {
		text = "<non-text>"
	}
	command := ""
	if msg.IsCommand() {
		command = "/" + strings.TrimSpace(msg.Command())
	}
	return &IncomingMessage{
		ChatID:   chatID,
		ChatType: chatType,
		From:     from,
		Command:  command,
		Text:     trimText(text, 120),
	}
}

func (s *Service) chatAllowed(chatID int64) bool {
	if len(s.allowedChatID) == 0 {
		return true
	}
	_, ok := s.allowedChatID[chatID]
	return ok
}

func (s *Service) sendText(chatID int64, text string) {
	out := tgbotapi.NewMessage(chatID, trimText(text, maxTelegramTextLen))
	out.DisableWebPagePreview = true
	if _, err := s.bot.Send(out); err != nil {
		s.errorf("telegram send failed: %v", err)
	}
}

func (s *Service) sendPhoto(chatID int64, filePath, caption string) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return
	}
	out := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(filePath))
	out.Caption = trimText(strings.TrimSpace(caption), maxTelegramCaption)
	if _, err := s.bot.Send(out); err != nil {
		s.errorf("telegram send photo failed: %v", err)
	}
}

func (s *Service) NotifyAll(text string) {
	targets := startupNotifyTargets(s.allowedChatID)
	if len(targets) == 0 {
		s.errorf("telegram notify skipped: no AUTO_WORK_TELEGRAM_CHAT_IDS configured")
		return
	}
	for _, chatID := range targets {
		s.sendText(chatID, text)
	}
}

func (s *Service) NotifyAllPhotos(caption string, filePaths []string) {
	if len(filePaths) == 0 {
		return
	}
	targets := startupNotifyTargets(s.allowedChatID)
	if len(targets) == 0 {
		s.errorf("telegram photo notify skipped: no AUTO_WORK_TELEGRAM_CHAT_IDS configured")
		return
	}

	paths := make([]string, 0, len(filePaths))
	for _, p := range filePaths {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}

	for _, chatID := range targets {
		for i, path := range paths {
			capText := ""
			if i == 0 {
				capText = caption
			}
			s.sendPhoto(chatID, path, capText)
		}
	}
}

func (s *Service) handleRecent(ctx context.Context, args string) string {
	limit := parseLeadingInt(args, defaultRecentLimit, maxRecentLimit)
	items, err := s.taskRepo.ListRecentByStatuses(ctx, []domain.TaskStatus{
		domain.TaskRunning,
		domain.TaskDone,
	}, limit)
	if err != nil {
		return fmt.Sprintf("查询最近任务失败: %v", err)
	}
	if len(items) == 0 {
		return "暂无执行中或已完成任务。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("最近任务（running/done，最多 %d 条）\n", limit))
	for i, task := range items {
		projectName := s.projectLabel(ctx, task.ProjectID)
		runBrief := s.latestRunBrief(ctx, task.ID)
		b.WriteString(fmt.Sprintf(
			"%d. [%s] %s\n任务: %s\n项目: %s\n优先级: %d | 更新时间: %s\n%s\n",
			i+1,
			statusLabel(task.Status),
			trimText(task.Title, 80),
			task.ID,
			projectName,
			task.Priority,
			task.UpdatedAt.Local().Format("2006-01-02 15:04:05"),
			runBrief,
		))
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) handleProjects(ctx context.Context, args string) string {
	limit := parseLeadingInt(args, defaultProjectLimit, maxProjectLimit)
	projects, err := s.projectRepo.List(ctx, limit)
	if err != nil {
		return fmt.Sprintf("查询项目失败: %v", err)
	}
	if len(projects) == 0 {
		return "暂无项目。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("项目列表（最多 %d 个）\n", limit))
	for i, project := range projects {
		b.WriteString(fmt.Sprintf(
			"%d. %s\n项目: %s\n默认 Provider: %s | Model: %s\n自动派发: %s | 失败策略: %s\n路径: %s\n",
			i+1,
			trimText(strings.TrimSpace(project.Name), 80),
			project.ID,
			project.DefaultProvider,
			displayOrDash(project.Model),
			onOffLabel(project.AutoDispatchEnabled),
			failurePolicyLabel(project.FailurePolicy),
			trimText(displayOrDash(project.Path), 120),
		))
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) handleProject(ctx context.Context, args string) string {
	selector := strings.TrimSpace(args)
	if selector == "" {
		return "用法: /project <项目ID或项目名>"
	}
	project, err := s.resolveProjectForCreateTask(ctx, selector)
	if err != nil {
		return fmt.Sprintf("查询项目失败: %v", err)
	}

	running, err := s.runRepo.ListRunning(ctx, project.ID, projectPreviewLimit)
	if err != nil {
		return fmt.Sprintf("查询项目失败: %v", err)
	}
	pendingStatus := domain.TaskPending
	pending, err := s.taskRepo.List(ctx, repository.TaskListFilter{
		Status:    &pendingStatus,
		ProjectID: project.ID,
		Limit:     projectPreviewLimit,
	})
	if err != nil {
		return fmt.Sprintf("查询项目失败: %v", err)
	}
	failed, err := s.taskRepo.ListByProjectAndStatuses(ctx, project.ID, []domain.TaskStatus{
		domain.TaskFailed,
		domain.TaskBlocked,
	}, projectPreviewLimit)
	if err != nil {
		return fmt.Sprintf("查询项目失败: %v", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"项目详情\n名称: %s\nID: %s\n路径: %s\n默认 Provider: %s\nModel: %s\n自动派发: %s\n失败策略: %s\n",
		trimText(strings.TrimSpace(project.Name), 100),
		project.ID,
		trimText(displayOrDash(project.Path), 160),
		project.DefaultProvider,
		displayOrDash(project.Model),
		onOffLabel(project.AutoDispatchEnabled),
		failurePolicyLabel(project.FailurePolicy),
	))
	appendRunningRunSection(&b, running, nil)
	appendTaskSection(&b, "待执行", pending, false, false, nil)
	appendTaskSection(&b, "失败/阻塞", failed, false, true, nil)
	return strings.TrimSpace(b.String())
}

func (s *Service) handleCreateProject(ctx context.Context, args string) string {
	name, path, provider, failurePolicy, err := parseAddProjectArgs(args)
	if err != nil {
		return err.Error()
	}
	project, err := s.projectSvc.Create(ctx, projectservice.CreateProjectInput{
		Name:            name,
		Path:            path,
		DefaultProvider: provider,
		FailurePolicy:   failurePolicy,
	})
	if err != nil {
		if errors.Is(err, projectservice.ErrInvalidInput) {
			return "创建项目失败: 参数不合法，请检查 provider/path/failure_policy"
		}
		return fmt.Sprintf("创建项目失败: %v", err)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"已创建项目\n项目: %s(%s)\n路径: %s\n默认 Provider: %s\n失败策略: %s\n自动派发: %s",
		project.Name,
		project.ID,
		project.Path,
		project.DefaultProvider,
		failurePolicyLabel(project.FailurePolicy),
		onOffLabel(project.AutoDispatchEnabled),
	))
}

func (s *Service) handleSetProjectProvider(ctx context.Context, args string) string {
	projectSelector, provider, err := parseSetProviderArgs(args)
	if err != nil {
		return err.Error()
	}
	project, err := s.resolveProjectForCreateTask(ctx, projectSelector)
	if err != nil {
		return fmt.Sprintf("修改项目 Provider 失败: %v", err)
	}
	updated, err := s.projectSvc.UpdateAIConfig(ctx, projectservice.UpdateProjectAIInput{
		ProjectID:       project.ID,
		DefaultProvider: provider,
		Model:           project.Model,
		SystemPrompt:    project.SystemPrompt,
		FailurePolicy:   string(project.FailurePolicy),
	})
	if err != nil {
		if errors.Is(err, projectservice.ErrInvalidInput) {
			return "修改项目 Provider 失败: provider 仅支持 claude 或 codex"
		}
		return fmt.Sprintf("修改项目 Provider 失败: %v", err)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"已更新项目默认 Provider\n项目: %s(%s)\n默认 Provider: %s\nModel: %s\n失败策略: %s",
		updated.Name,
		updated.ID,
		updated.DefaultProvider,
		displayOrDash(updated.Model),
		failurePolicyLabel(updated.FailurePolicy),
	))
}

func (s *Service) handleTaskStatus(ctx context.Context, args string) string {
	taskID := strings.TrimSpace(args)
	if taskID == "" {
		return "用法: /task <task_id> 或 /status <task_id>"
	}
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Sprintf("查询任务失败: %v", err)
	}
	runs, err := s.runRepo.ListByTask(ctx, task.ID, 1)
	if err != nil {
		return fmt.Sprintf("查询任务运行记录失败: %v", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务详情\nID: %s\n标题: %s\n状态: %s\n项目: %s\n优先级: %d\nProvider: %s\n",
		task.ID, trimText(task.Title, 100), statusLabel(task.Status), s.projectLabel(ctx, task.ProjectID), task.Priority, task.Provider))
	if strings.TrimSpace(task.Description) != "" {
		b.WriteString(fmt.Sprintf("描述: %s\n", trimText(task.Description, 300)))
	}
	if task.NextRetryAt != nil {
		b.WriteString(fmt.Sprintf("下次重试: %s\n", task.NextRetryAt.Local().Format("2006-01-02 15:04:05")))
	}
	b.WriteString(fmt.Sprintf("重试: %s\n", formatRetryQuota(task.RetryCount, task.MaxRetries)))

	if len(runs) == 0 {
		b.WriteString("最近运行: 无")
		return b.String()
	}
	last := runs[0]
	b.WriteString(fmt.Sprintf("最近运行: %s | 状态: %s | attempt: %d | 开始: %s\n",
		last.ID, runStatusLabel(last.Status), last.Attempt, last.StartedAt.Local().Format("2006-01-02 15:04:05")))
	if last.FinishedAt != nil {
		b.WriteString(fmt.Sprintf("结束: %s\n", last.FinishedAt.Local().Format("2006-01-02 15:04:05")))
	}
	if last.ExitCode != nil {
		b.WriteString(fmt.Sprintf("退出码: %d\n", *last.ExitCode))
	}
	if strings.TrimSpace(last.ResultSummary) != "" {
		b.WriteString(fmt.Sprintf("摘要: %s\n", trimText(last.ResultSummary, 240)))
	}
	if strings.TrimSpace(last.ResultDetails) != "" {
		b.WriteString(fmt.Sprintf("详情: %s\n", trimText(last.ResultDetails, 300)))
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) handleTaskLatestLogs(ctx context.Context, args string) string {
	parts := strings.Fields(strings.TrimSpace(args))
	if len(parts) == 0 {
		return "用法: /tasklogs <task_id> [条数]"
	}
	taskID := strings.TrimSpace(parts[0])
	if taskID == "" {
		return "用法: /tasklogs <task_id> [条数]"
	}
	limit := defaultLogLimit
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Sprintf("查询任务最新运行日志失败: %v", err)
	}
	runs, err := s.runRepo.ListByTask(ctx, task.ID, 1)
	if err != nil {
		return fmt.Sprintf("查询任务最新运行日志失败: %v", err)
	}
	if len(runs) == 0 {
		return fmt.Sprintf("任务 %s 暂无运行记录。", task.ID)
	}
	return s.renderRunLogs(ctx, task.ID, runs[0].ID, true, limit)
}

func (s *Service) handleDispatchTask(ctx context.Context, args string) string {
	taskID := strings.TrimSpace(args)
	if taskID == "" {
		return "用法: /dispatch <task_id>"
	}
	if s.dispatchTask == nil {
		return "当前环境未接入任务派发能力。"
	}
	resp, err := s.dispatchTask(ctx, taskID)
	if err != nil {
		return fmt.Sprintf("派发任务失败: %v", err)
	}
	if resp == nil {
		return "派发任务失败: 返回结果为空"
	}
	return strings.TrimSpace(fmt.Sprintf(
		"派发结果\n任务: %s\nrun: %s\nclaimed: %t\n消息: %s",
		displayOrDash(resp.TaskID),
		displayOrDash(resp.RunID),
		resp.Claimed,
		displayOrDash(resp.Message),
	))
}

func (s *Service) handlePending(ctx context.Context, args string) string {
	selector, limit := parseTrailingLimit(args, defaultQueueLimit, maxQueueLimit)
	status := domain.TaskPending

	var (
		items  []domain.Task
		header string
		err    error
	)
	if selector == "" {
		items, err = s.taskRepo.ListByStatus(ctx, status, limit)
		header = fmt.Sprintf("待执行任务（最多 %d 条）", limit)
	} else {
		project, resolveErr := s.resolveProjectForCreateTask(ctx, selector)
		if resolveErr != nil {
			return fmt.Sprintf("查询待执行任务失败: %v", resolveErr)
		}
		items, err = s.taskRepo.List(ctx, repository.TaskListFilter{
			Status:    &status,
			ProjectID: project.ID,
			Limit:     limit,
		})
		header = fmt.Sprintf("待执行任务（项目: %s，最多 %d 条）", s.projectLabel(ctx, project.ID), limit)
	}
	if err != nil {
		return fmt.Sprintf("查询待执行任务失败: %v", err)
	}
	if len(items) == 0 {
		if selector == "" {
			return "暂无待执行任务。"
		}
		return fmt.Sprintf("项目 %q 暂无待执行任务。", selector)
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	appendTaskItems(&b, items, selector == "", false, func(projectID string) string {
		return s.projectLabel(ctx, projectID)
	})
	return strings.TrimSpace(b.String())
}

func (s *Service) handleFailed(ctx context.Context, args string) string {
	selector, limit := parseTrailingLimit(args, defaultQueueLimit, maxQueueLimit)

	var (
		items  []domain.Task
		header string
		err    error
	)
	if selector == "" {
		items, err = s.taskRepo.ListRecentByStatuses(ctx, []domain.TaskStatus{
			domain.TaskFailed,
			domain.TaskBlocked,
		}, limit)
		header = fmt.Sprintf("失败/阻塞任务（最多 %d 条）", limit)
	} else {
		project, resolveErr := s.resolveProjectForCreateTask(ctx, selector)
		if resolveErr != nil {
			return fmt.Sprintf("查询失败任务失败: %v", resolveErr)
		}
		items, err = s.taskRepo.ListByProjectAndStatuses(ctx, project.ID, []domain.TaskStatus{
			domain.TaskFailed,
			domain.TaskBlocked,
		}, limit)
		header = fmt.Sprintf("失败/阻塞任务（项目: %s，最多 %d 条）", s.projectLabel(ctx, project.ID), limit)
	}
	if err != nil {
		return fmt.Sprintf("查询失败任务失败: %v", err)
	}
	if len(items) == 0 {
		if selector == "" {
			return "暂无失败或阻塞任务。"
		}
		return fmt.Sprintf("项目 %q 暂无失败或阻塞任务。", selector)
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	appendTaskItems(&b, items, selector == "", true, func(projectID string) string {
		return s.projectLabel(ctx, projectID)
	})
	return strings.TrimSpace(b.String())
}

func (s *Service) handleRunning(ctx context.Context, args string) string {
	selector, limit := parseTrailingLimit(args, defaultQueueLimit, maxQueueLimit)
	projectID := ""
	header := fmt.Sprintf("运行中任务（最多 %d 条）", limit)
	if selector != "" {
		project, err := s.resolveProjectForCreateTask(ctx, selector)
		if err != nil {
			return fmt.Sprintf("查询运行中任务失败: %v", err)
		}
		projectID = project.ID
		header = fmt.Sprintf("运行中任务（项目: %s，最多 %d 条）", s.projectLabel(ctx, project.ID), limit)
	}

	items, err := s.runRepo.ListRunning(ctx, projectID, limit)
	if err != nil {
		return fmt.Sprintf("查询运行中任务失败: %v", err)
	}
	if len(items) == 0 {
		if selector == "" {
			return "当前没有运行中的任务。"
		}
		return fmt.Sprintf("项目 %q 当前没有运行中的任务。", selector)
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	appendRunningRunItems(&b, items, selector == "", func(projectID string) string {
		return s.projectLabel(ctx, projectID)
	})
	return strings.TrimSpace(b.String())
}

func (s *Service) handleLogs(ctx context.Context, args string) string {
	parts := strings.Fields(strings.TrimSpace(args))
	if len(parts) == 0 {
		return "用法: /logs <run_id|task_id> [条数]"
	}
	targetID := parts[0]
	limit := defaultLogLimit
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	runID, fromTask, err := s.resolveRunID(ctx, targetID)
	if err != nil {
		return fmt.Sprintf("定位运行记录失败: %v", err)
	}
	return s.renderRunLogs(ctx, targetID, runID, fromTask, limit)
}

func (s *Service) renderRunLogs(ctx context.Context, targetID, runID string, fromTask bool, limit int) string {
	events, err := s.eventRepo.ListByRun(ctx, runID, limit)
	if err != nil {
		return fmt.Sprintf("读取日志失败: %v", err)
	}
	if len(events) == 0 {
		return fmt.Sprintf("run=%s 暂无日志。", runID)
	}

	var b strings.Builder
	if fromTask {
		b.WriteString(fmt.Sprintf("任务 %s 最新运行日志（run=%s）\n", targetID, runID))
	} else {
		b.WriteString(fmt.Sprintf("运行日志（run=%s）\n", runID))
	}

	written := 0
	for _, e := range events {
		msg, ok := compactEventPayload(e.Kind, e.Payload)
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("%s [%s] %s\n",
			e.TS.Local().Format("15:04:05"),
			e.Kind,
			msg,
		))
		written++
	}
	if written == 0 {
		return fmt.Sprintf("run=%s 日志仅包含可忽略噪音，暂无可读摘要。", runID)
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) handleCreateTask(ctx context.Context, args string) string {
	projectSelector, title, desc, _, _, err := parseAddTaskArgs(args)
	if err != nil {
		return err.Error()
	}
	project, err := s.resolveProjectForCreateTask(ctx, projectSelector)
	if err != nil {
		return fmt.Sprintf("创建任务失败: %v", err)
	}
	priority, err := s.taskRepo.NextAppendPriority(ctx, project.ID, 100)
	if err != nil {
		return fmt.Sprintf("创建任务失败: 获取优先级失败: %v", err)
	}
	now := time.Now().UTC()
	task := &domain.Task{
		ID:          uuid.NewString(),
		ProjectID:   project.ID,
		Title:       title,
		Description: desc,
		Priority:    priority,
		Status:      domain.TaskPending,
		DependsOn:   []string{},
		Provider:    "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return fmt.Sprintf("创建任务失败: %v", err)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"已创建任务\n任务ID: %s\n项目: %s(%s)\n标题: %s\n优先级: %d\nProvider: 未分配（执行时按项目默认=%s）\n状态: %s",
		task.ID,
		project.Name,
		project.ID,
		trimText(task.Title, 100),
		task.Priority,
		project.DefaultProvider,
		statusLabel(task.Status),
	))
}

func (s *Service) handleSetAutoDispatch(ctx context.Context, args string, enabled bool) string {
	selector := strings.TrimSpace(args)
	if selector == "" {
		if enabled {
			return "用法: /autodisp_on <项目ID或项目名>"
		}
		return "用法: /autodisp_off <项目ID或项目名>"
	}

	project, err := s.resolveProjectForCreateTask(ctx, selector)
	if err != nil {
		return fmt.Sprintf("设置自动派发失败: %v", err)
	}

	actionLabel := "开启"
	if !enabled {
		actionLabel = "关闭"
	}
	stateLabel := "已关闭"
	if enabled {
		stateLabel = "已开启"
	}

	if project.AutoDispatchEnabled == enabled {
		return strings.TrimSpace(fmt.Sprintf(
			"项目自动派发已是%s\n项目: %s(%s)\n当前状态: %s",
			actionLabel,
			project.Name,
			project.ID,
			stateLabel,
		))
	}

	if err := s.projectRepo.SetAutoDispatchEnabled(ctx, project.ID, enabled); err != nil {
		return fmt.Sprintf("设置自动派发失败: %v", err)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"已%s项目自动派发\n项目: %s(%s)\n当前状态: %s",
		actionLabel,
		project.Name,
		project.ID,
		stateLabel,
	))
}

func parseAddTaskArgs(raw string) (projectSelector string, title string, desc string, provider string, providerSpecified bool, err error) {
	parts := splitPipedArgs(raw)
	if len(parts) < 2 {
		return "", "", "", "", false, errors.New(addTaskUsage())
	}

	provider = ""
	if len(parts) >= 3 {
		candidateProvider := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		if candidateProvider == "claude" || candidateProvider == "codex" {
			provider = candidateProvider
			providerSpecified = true
			parts = parts[:len(parts)-1]
		}
	}

	if len(parts) == 3 && strings.HasPrefix(strings.ToLower(parts[0]), "p:") {
		projectSelector = strings.TrimSpace(parts[0][2:])
		parts = parts[1:]
	}

	if len(parts) != 2 {
		return "", "", "", "", false, errors.New(addTaskUsage())
	}

	title = strings.TrimSpace(parts[0])
	desc = strings.TrimSpace(parts[1])
	if title == "" || desc == "" {
		return "", "", "", "", false, errors.New(addTaskUsage())
	}
	if len([]rune(title)) > 200 {
		return "", "", "", "", false, errors.New("创建任务失败: 标题过长（最多 200 字符）")
	}
	if len([]rune(desc)) > 4000 {
		return "", "", "", "", false, errors.New("创建任务失败: 描述过长（最多 4000 字符）")
	}
	return projectSelector, title, desc, provider, providerSpecified, nil
}

func parseAddProjectArgs(raw string) (name string, path string, provider string, failurePolicy string, err error) {
	parts := splitPipedArgs(raw)
	if len(parts) < 2 || len(parts) > 4 {
		return "", "", "", "", errors.New(addProjectUsage())
	}

	name = strings.TrimSpace(parts[0])
	path = strings.TrimSpace(parts[1])
	if name == "" || path == "" {
		return "", "", "", "", errors.New(addProjectUsage())
	}
	provider = "claude"
	failurePolicy = "block"

	for _, extra := range parts[2:] {
		val := strings.ToLower(strings.TrimSpace(extra))
		switch val {
		case "claude", "codex":
			provider = val
		case "block", "continue":
			failurePolicy = val
		default:
			return "", "", "", "", errors.New(addProjectUsage())
		}
	}
	return name, path, provider, failurePolicy, nil
}

func parseSetProviderArgs(raw string) (projectSelector string, provider string, err error) {
	parts := splitPipedArgs(raw)
	if len(parts) != 2 {
		return "", "", errors.New(setProviderUsage())
	}
	projectSelector = strings.TrimSpace(parts[0])
	provider = strings.ToLower(strings.TrimSpace(parts[1]))
	if projectSelector == "" || (provider != "claude" && provider != "codex") {
		return "", "", errors.New(setProviderUsage())
	}
	return projectSelector, provider, nil
}

func splitPipedArgs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		val := strings.TrimSpace(p)
		if val == "" {
			continue
		}
		out = append(out, val)
	}
	return out
}

func (s *Service) resolveProjectForCreateTask(ctx context.Context, selector string) (*domain.Project, error) {
	selector = strings.TrimSpace(selector)
	projects, err := s.projectRepo.List(ctx, 200)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, errors.New("当前没有项目，请先在界面中创建项目")
	}

	if selector == "" {
		if len(projects) == 1 {
			return &projects[0], nil
		}
		return nil, fmt.Errorf("存在多个项目，请在命令里指定项目。可用项目: %s", projectChoices(projects, 8))
	}

	if p, err := s.projectRepo.GetByID(ctx, selector); err == nil {
		return p, nil
	} else if !errors.Is(err, repository.ErrProjectNotFound) {
		return nil, err
	}

	matches := make([]domain.Project, 0, 2)
	for _, p := range projects {
		if strings.EqualFold(strings.TrimSpace(p.Name), selector) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("项目名 %q 不唯一，请改用项目 ID。匹配项: %s", selector, projectChoices(matches, 8))
	}
	return nil, fmt.Errorf("未找到项目 %q。可用项目: %s", selector, projectChoices(projects, 8))
}

func projectChoices(projects []domain.Project, max int) string {
	if len(projects) == 0 {
		return "-"
	}
	if max <= 0 {
		max = 8
	}
	limit := len(projects)
	if limit > max {
		limit = max
	}
	items := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		p := projects[i]
		items = append(items, fmt.Sprintf("%s(%s)", strings.TrimSpace(p.Name), p.ID))
	}
	if len(projects) > limit {
		items = append(items, fmt.Sprintf("...共%d个项目", len(projects)))
	}
	return strings.Join(items, ", ")
}

func addTaskUsage() string {
	return strings.TrimSpace(`用法:
/addtask <标题> | <描述> [| claude|codex]
/addtask p:<项目ID或项目名> | <标题> | <描述> [| claude|codex]
示例:
/addtask 修复登录超时 | 排查 timeout 根因并补测试
/addtask p:proj-123 | 新增健康检查接口 | 增加 /healthz 与单测 | claude`)
}

func addProjectUsage() string {
	return strings.TrimSpace(`用法:
/addproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]
/newproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]
示例:
/addproject 支付系统 | /srv/payments
/addproject 自动工作台 | /Users/me/work/auto-work | codex | continue`)
}

func setProviderUsage() string {
	return strings.TrimSpace(`用法:
/setprovider <项目ID或项目名> | <claude|codex>
/provider <项目ID或项目名> | <claude|codex>
示例:
/setprovider 支付系统 | codex
/provider proj-123 | claude`)
}

func (s *Service) resolveRunID(ctx context.Context, target string) (runID string, fromTask bool, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false, errors.New("id 不能为空")
	}
	run, runErr := s.runRepo.GetByID(ctx, target)
	if runErr == nil {
		return run.ID, false, nil
	}
	if !errors.Is(runErr, repository.ErrRunNotFound) {
		return "", false, runErr
	}

	task, taskErr := s.taskRepo.GetByID(ctx, target)
	if taskErr != nil {
		return "", false, taskErr
	}
	runs, err := s.runRepo.ListByTask(ctx, task.ID, 1)
	if err != nil {
		return "", false, err
	}
	if len(runs) == 0 {
		return "", false, errors.New("该任务暂无运行记录")
	}
	return runs[0].ID, true, nil
}

func (s *Service) latestRunBrief(ctx context.Context, taskID string) string {
	runs, err := s.runRepo.ListByTask(ctx, taskID, 1)
	if err != nil || len(runs) == 0 {
		return "最近运行: 无"
	}
	last := runs[0]
	return fmt.Sprintf("最近运行: %s | %s | attempt=%d",
		last.ID, runStatusLabel(last.Status), last.Attempt)
}

func (s *Service) projectLabel(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "-"
	}
	p, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return projectID
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return projectID
	}
	return fmt.Sprintf("%s(%s)", name, projectID)
}

func parseLeadingInt(raw string, fallback int, max int) int {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return fallback
	}
	v, err := strconv.Atoi(fields[0])
	if err != nil || v <= 0 {
		return fallback
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func parseTrailingLimit(raw string, fallback int, max int) (string, int) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", fallback
	}
	last := fields[len(fields)-1]
	n, err := strconv.Atoi(last)
	if err != nil || n <= 0 {
		return strings.TrimSpace(raw), fallback
	}
	if max > 0 && n > max {
		n = max
	}
	return strings.TrimSpace(strings.Join(fields[:len(fields)-1], " ")), n
}

func formatRetryQuota(retryCount int, maxRetries int) string {
	if maxRetries <= 0 {
		return fmt.Sprintf("%d/∞", retryCount)
	}
	return fmt.Sprintf("%d/%d", retryCount, maxRetries)
}

func statusLabel(status domain.TaskStatus) string {
	switch status {
	case domain.TaskPending:
		return "待执行(pending)"
	case domain.TaskRunning:
		return "执行中(running)"
	case domain.TaskDone:
		return "已完成(done)"
	case domain.TaskFailed:
		return "失败(failed)"
	case domain.TaskBlocked:
		return "阻塞(blocked)"
	default:
		return string(status)
	}
}

func runStatusLabel(status domain.RunStatus) string {
	switch status {
	case domain.RunRunning:
		return "执行中(running)"
	case domain.RunDone:
		return "已完成(done)"
	case domain.RunFailed:
		return "失败(failed)"
	case domain.RunNeedsInput:
		return "等待输入(needs_input)"
	case domain.RunCancelled:
		return "已取消(cancelled)"
	default:
		return string(status)
	}
}

func displayOrDash(in string) string {
	val := strings.TrimSpace(in)
	if val == "" {
		return "-"
	}
	return val
}

func onOffLabel(enabled bool) string {
	if enabled {
		return "已开启"
	}
	return "已关闭"
}

func failurePolicyLabel(policy domain.ProjectFailurePolicy) string {
	switch policy {
	case domain.ProjectFailurePolicyContinue:
		return "失败后继续后续任务(continue)"
	case domain.ProjectFailurePolicyBlock:
		fallthrough
	default:
		return "失败后阻塞后续任务(block)"
	}
}

func appendTaskSection(b *strings.Builder, title string, items []domain.Task, showProject bool, includeRetry bool, projectLabeler func(string) string) {
	b.WriteString(title)
	b.WriteString(":\n")
	if len(items) == 0 {
		b.WriteString("- 无\n")
		return
	}
	appendTaskItems(b, items, showProject, includeRetry, projectLabeler)
}

func appendTaskItems(b *strings.Builder, items []domain.Task, showProject bool, includeRetry bool, projectLabeler func(string) string) {
	for i, task := range items {
		b.WriteString(fmt.Sprintf(
			"%d. [%s] %s\n任务: %s\n",
			i+1,
			statusLabel(task.Status),
			trimText(task.Title, 80),
			task.ID,
		))
		if showProject {
			projectLabel := displayOrDash(strings.TrimSpace(task.ProjectID))
			if projectLabeler != nil {
				projectLabel = projectLabeler(task.ProjectID)
			}
			b.WriteString(fmt.Sprintf("项目: %s\n", projectLabel))
		}
		b.WriteString(fmt.Sprintf(
			"优先级: %d | 更新时间: %s\n",
			task.Priority,
			task.UpdatedAt.Local().Format("2006-01-02 15:04:05"),
		))
		if includeRetry {
			b.WriteString(fmt.Sprintf("重试: %s\n", formatRetryQuota(task.RetryCount, task.MaxRetries)))
			if task.NextRetryAt != nil {
				b.WriteString(fmt.Sprintf("下次重试: %s\n", task.NextRetryAt.Local().Format("2006-01-02 15:04:05")))
			}
		}
	}
}

func appendRunningRunSection(b *strings.Builder, items []repository.RunningRunRecord, projectLabeler func(string) string) {
	b.WriteString("运行中:\n")
	if len(items) == 0 {
		b.WriteString("- 无\n")
		return
	}
	appendRunningRunItems(b, items, false, projectLabeler)
}

func appendRunningRunItems(b *strings.Builder, items []repository.RunningRunRecord, showProject bool, projectLabeler func(string) string) {
	for i, item := range items {
		heartbeat := "-"
		if item.HeartbeatAt != nil {
			heartbeat = item.HeartbeatAt.Local().Format("2006-01-02 15:04:05")
		}
		b.WriteString(fmt.Sprintf(
			"%d. %s\nrun: %s | task: %s\n",
			i+1,
			trimText(item.TaskTitle, 80),
			item.RunID,
			item.TaskID,
		))
		if showProject {
			projectLabel := displayOrDash(strings.TrimSpace(item.ProjectID))
			if projectLabeler != nil {
				projectLabel = projectLabeler(item.ProjectID)
			}
			b.WriteString(fmt.Sprintf("项目: %s\n", projectLabel))
		}
		b.WriteString(fmt.Sprintf(
			"状态: %s | agent: %s | 启动: %s | 心跳: %s\n",
			runStatusLabel(item.Status),
			displayOrDash(item.AgentID),
			item.StartedAt.Local().Format("2006-01-02 15:04:05"),
			heartbeat,
		))
	}
}

func helpText() string {
	return strings.TrimSpace(`
可用指令：
/help
/addproject ...                创建项目
/newproject ...                同 /addproject
/setprovider ...               修改项目默认 Provider
/provider ...                  同 /setprovider
/projects [n]                 查看项目列表与 AI 配置摘要
/project <项目ID或项目名>       查看单个项目详情
/recent [n]                   查看最近执行中或已完成任务
/pending [项目ID或项目名] [n]  查看待执行任务队列
/queue [项目ID或项目名] [n]    同 /pending
/failed [项目ID或项目名] [n]   查看失败/阻塞任务
/running [项目ID或项目名] [n]  查看运行中的任务
/dispatch <task_id>           派发指定任务
/autodisp_on <项目ID或项目名>   开启指定项目自动派发
/autodisp_off <项目ID或项目名>  关闭指定项目自动派发
/task <task_id>               查看任务详情与最新运行状态
/status <task_id>             同 /task
/logs <run_id|task_id> [n]    查看缩略日志（默认20条）
/tasklogs <task_id> [n]       查看任务最新一次运行日志
/latestlogs <task_id> [n]     同 /tasklogs
/addtask ...                  创建任务（自动分配优先级，详见用法）
`)
}

func (s *Service) sendStartupNotification() {
	targets := startupNotifyTargets(s.allowedChatID)
	if len(targets) == 0 {
		s.errorf("telegram startup notice skipped: no AUTO_WORK_TELEGRAM_CHAT_IDS configured")
		return
	}
	msg := startupNotifyMessage(time.Now())
	for _, chatID := range targets {
		s.sendText(chatID, msg)
	}
}

func startupNotifyTargets(allowed map[int64]struct{}) []int64 {
	if len(allowed) == 0 {
		return nil
	}
	out := make([]int64, 0, len(allowed))
	for id := range allowed {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func startupNotifyMessage(now time.Time) string {
	return fmt.Sprintf(
		"自动工作台已启动。\n时间: %s\n你可以发送 /help 查看可用命令。",
		now.Local().Format("2006-01-02 15:04:05"),
	)
}
