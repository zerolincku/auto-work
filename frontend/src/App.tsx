import { FormEvent, PropsWithChildren, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Environment, EventsOn, Quit, WindowIsMaximised, WindowMinimise, WindowToggleMaximise } from "../wailsjs/runtime/runtime";

import { LANGUAGE_STORAGE_KEY } from "./i18n";
import "./App.css";

type Project = {
  ID: string;
  Name: string;
  Path: string;
  DefaultProvider?: string;
  Model?: string;
  SystemPrompt?: string;
  FailurePolicy?: string;
  AutoDispatchEnabled?: boolean;
  CreatedAt: string;
  UpdatedAt: string;
};

type Task = {
  ID: string;
  ProjectID: string;
  Title: string;
  Description: string;
  Priority: number;
  Status: string;
  DependsOn: string[];
  Provider: string;
  RetryCount?: number;
  NextRetryAt?: string;
  CreatedAt: string;
  UpdatedAt: string;
};

type RunningRun = {
  run_id: string;
  task_id: string;
  task_title: string;
  project_id: string;
  agent_id: string;
  status: string;
  started_at: string;
  heartbeat_at?: string;
};

type RunLogEvent = {
  id: string;
  run_id: string;
  ts: string;
  kind: string;
  payload: string;
};

type SystemLogEvent = {
  id: string;
  run_id: string;
  task_id: string;
  task_title: string;
  project_id: string;
  ts: string;
  kind: string;
  payload: string;
};

type DisplayLogEvent = {
  id: string;
  ts: string;
  kind: string;
  text: string;
  title: string;
};

type TaskRunHistory = {
  run_id: string;
  status: string;
  attempt: number;
  started_at: string;
  finished_at?: string;
  exit_code?: number;
  result_summary?: string;
  result_details?: string;
};

type TaskDetail = {
  task: Task;
  runs: TaskRunHistory[];
};

type DispatchResponse = {
  claimed: boolean;
  run_id?: string;
  task_id?: string;
  message?: string;
};

type MCPStatus = {
  enabled: boolean;
  state: "disabled" | "connected" | "failed" | "unknown";
  message: string;
  run_id?: string;
  updated_at?: string;
};

type GlobalSettings = {
  telegram_enabled: boolean;
  telegram_bot_token: string;
  telegram_chat_ids: string;
  telegram_poll_timeout: number;
  telegram_proxy_url: string;
  system_prompt: string;
  updated_at?: string;
};

type AppAPI = {
  Health: () => Promise<string>;
  MCPStatus: () => Promise<MCPStatus>;
  GetGlobalSettings: () => Promise<GlobalSettings>;
  UpdateGlobalSettings: (req: {
    telegram_enabled: boolean;
    telegram_bot_token: string;
    telegram_chat_ids: string;
    telegram_poll_timeout: number;
    telegram_proxy_url: string;
    system_prompt: string;
  }) => Promise<GlobalSettings>;
  AutoRunEnabled: (projectID: string) => Promise<boolean>;
  SetAutoRunEnabled: (projectID: string, enabled: boolean) => Promise<boolean>;
  CreateProject: (req: {
    name: string;
    path: string;
    default_provider: string;
    model: string;
    system_prompt: string;
    failure_policy: string;
  }) => Promise<Project>;
  UpdateProjectAIConfig: (req: {
    project_id: string;
    default_provider: string;
    model: string;
    system_prompt: string;
    failure_policy: string;
  }) => Promise<Project>;
  UpdateProject: (req: {
    project_id: string;
    name: string;
    default_provider: string;
    model: string;
    system_prompt: string;
    failure_policy: string;
  }) => Promise<Project>;
  DeleteProject: (projectID: string) => Promise<void>;
  ListProjects: (limit: number) => Promise<Project[]>;
  CreateTask: (req: {
    project_id: string;
    title: string;
    description: string;
    priority: number;
    depends_on: string[];
    provider: string;
  }) => Promise<Task>;
  UpdateTask: (req: {
    task_id: string;
    title: string;
    description: string;
    priority: number;
    depends_on: string[];
  }) => Promise<Task>;
  DeleteTask: (taskID: string) => Promise<void>;
  ListTasks: (
    status: string,
    provider: string,
    projectID: string,
    limit: number,
  ) => Promise<Task[]>;
  UpdateTaskStatus: (taskID: string, status: string) => Promise<void>;
  RetryTask: (taskID: string) => Promise<void>;
  DispatchOnce: (agentID: string, projectID: string) => Promise<DispatchResponse>;
  FinishRun: (req: {
    run_id: string;
    status: string;
    summary: string;
    details: string;
    task_status: string;
  }) => Promise<void>;
  ListRunningRuns: (projectID: string, limit: number) => Promise<RunningRun[]>;
  ListRunLogs: (runID: string, limit: number) => Promise<RunLogEvent[]>;
  ListSystemLogs: (projectID: string, limit: number) => Promise<SystemLogEvent[]>;
  GetTaskDetail: (taskID: string) => Promise<TaskDetail | null>;
};

type Translate = (key: string, options?: Record<string, unknown>) => string;
type OSPlatform = "darwin" | "windows" | "linux" | "unknown";

const HEALTH_MESSAGE_CHECKING = "__health.checking__";
const HEALTH_MESSAGE_BACKEND_NOT_READY = "__health.backend_not_ready__";
const NOTICE_AUTO_CLOSE_MS = 5000;

function formatTelegramIncomingNotice(payload: unknown, t: Translate): string | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const raw = payload as Record<string, unknown>;
  const chatIDRaw = raw.chat_id;
  const chatID =
    typeof chatIDRaw === "number" || typeof chatIDRaw === "string"
      ? String(chatIDRaw).trim() || t("common.unknown")
      : t("common.unknown");
  const command = typeof raw.command === "string" ? raw.command.trim() : "";
  if (command) {
    return t("info.telegramIncomingCommand", { chatId: chatID, command });
  }
  const text = typeof raw.text === "string" ? raw.text.trim() : "";
  return t("info.telegramIncomingMessage", { chatId: chatID, text: text || t("common.dash") });
}

function normalizeLanguage(language?: string): "zh-CN" | "en-US" {
  if (!language) {
    return "zh-CN";
  }
  const lowered = language.toLowerCase();
  if (lowered.startsWith("zh")) {
    return "zh-CN";
  }
  if (lowered.startsWith("en")) {
    return "en-US";
  }
  return "zh-CN";
}

function normalizePlatform(platform?: string): OSPlatform {
  switch ((platform || "").toLowerCase()) {
    case "darwin":
    case "windows":
    case "linux":
      return platform!.toLowerCase() as OSPlatform;
    default:
      return "unknown";
  }
}

function applyPlatformClass(platform: OSPlatform): void {
  if (typeof document === "undefined") {
    return;
  }
  document.body.dataset.platform = platform;
  document.body.classList.remove("os-darwin", "os-windows", "os-linux", "os-unknown");
  document.body.classList.add(`os-${platform}`);
}

function renderHealthMessage(value: string, t: Translate): string {
  if (value === HEALTH_MESSAGE_CHECKING) {
    return t("health.checking");
  }
  if (value === HEALTH_MESSAGE_BACKEND_NOT_READY) {
    return t("health.backendNotReady");
  }
  return value;
}

function formatBackendError(err: unknown, t: Translate): string {
  const raw = String(err || "").trim();
  const lowered = raw.toLowerCase();

  if (!raw) {
    return t("errors.unknown");
  }
  if (raw.includes("UNIQUE constraint failed: projects.path")) {
    return t("errors.duplicateProjectPath");
  }
  if (raw.includes("UNIQUE constraint failed: projects.name")) {
    return t("errors.duplicateProjectName");
  }
  if (raw.includes("invalid project input")) {
    return t("errors.invalidProjectInput");
  }
  if (raw.includes("invalid task input")) {
    return t("errors.invalidTaskInput");
  }
  if (raw.includes("task is only editable when pending") || raw.includes("task is not editable while running")) {
    return t("errors.taskEditOnlyNonRunning");
  }
  if (raw.includes("task is not deletable while running")) {
    return t("errors.taskDeleteOnlyNonRunning");
  }
  if (raw.includes("task status does not support retry")) {
    return t("errors.retryUnsupported");
  }
  if (raw.includes("chdir") && raw.includes("no such file or directory")) {
    return t("errors.projectPathMissing");
  }
  if (
    raw.includes("启用 Telegram 需要先填写 Bot Token") ||
    (lowered.includes("telegram") && lowered.includes("bot token"))
  ) {
    return t("errors.telegramTokenRequired");
  }
  if (raw.includes("无效 Telegram Chat ID") || lowered.includes("invalid telegram chat id")) {
    return t("errors.invalidTelegramChatId", { detail: raw });
  }
  if (raw.includes("配置已保存，但 Telegram 启动失败") || lowered.includes("telegram") && lowered.includes("start failed")) {
    return t("errors.telegramStartFailed", { detail: raw });
  }
  if (raw.includes("无效代理地址") || lowered.includes("invalid proxy")) {
    return t("errors.invalidProxy", { detail: raw });
  }
  return raw;
}

function safeParseJSON(raw: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object") {
      return null;
    }
    return parsed as Record<string, unknown>;
  } catch {
    return null;
  }
}

function compactText(raw: string, maxLen = 300): string {
  const normalized = raw.replace(/\s+/g, " ").trim();
  if (!normalized) {
    return "";
  }
  if (normalized.length <= maxLen) {
    return normalized;
  }
  return `${normalized.slice(0, maxLen)}...`;
}

function formatSystemLogPayload(raw: string, maxLen = 1400): string {
  const trimmed = raw.trim();
  if (!trimmed) {
    return "";
  }
  const parsed = safeParseJSON(trimmed);
  const normalized = parsed ? JSON.stringify(parsed, null, 2) : trimmed;
  if (normalized.length <= maxLen) {
    return normalized;
  }
  return `${normalized.slice(0, maxLen)}...`;
}

function splitLogKind(kind: string): { source: string; channel: string } {
  const [source, channel] = kind.split(".");
  return {
    source: source || kind || "system",
    channel: channel || kind || "log",
  };
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

function parseClaudeLogLine(log: RunLogEvent, t: Translate): DisplayLogEvent | null {
  const parsed = safeParseJSON(log.payload);
  if (!parsed) {
    return {
      id: log.id,
      ts: log.ts,
      kind: log.kind,
      text: compactText(log.payload),
      title: log.payload,
    };
  }

  const type = String(parsed.type || "");
  if (!type) {
    return {
      id: log.id,
      ts: log.ts,
      kind: log.kind,
      text: compactText(log.payload),
      title: log.payload,
    };
  }

  if (type === "assistant") {
    const message = parsed.message as Record<string, unknown> | undefined;
    const content = Array.isArray(message?.content) ? (message?.content as Array<Record<string, unknown>>) : [];
    const texts: string[] = [];
    let toolName = "";
    for (const item of content) {
      const itemType = String(item?.type || "");
      if (itemType === "thinking") {
        continue;
      }
      if (itemType === "text") {
        const text = compactText(String(item?.text || ""));
        if (text) texts.push(text);
        continue;
      }
      if (itemType === "tool_use") {
        toolName = String(item?.name || "");
        continue;
      }
    }
    if (toolName) {
      return {
        id: log.id,
        ts: log.ts,
        kind: "assistant.tool",
        text: t("logs.callTool", { toolName }),
        title: log.payload,
      };
    }
    if (!texts.length) {
      return null;
    }
    return {
      id: log.id,
      ts: log.ts,
      kind: "assistant",
      text: texts.join(" "),
      title: log.payload,
    };
  }

  if (type === "result") {
    const subtype = String(parsed.subtype || "");
    const resultText = compactText(String(parsed.result || ""));
    const denials = Array.isArray(parsed.permission_denials)
      ? (parsed.permission_denials as Array<Record<string, unknown>>).map((v) => String(v?.tool_name || "")).filter(Boolean)
      : [];
    let text = t("logs.result", { subtype: subtype || t("common.unknown") });
    if (resultText) {
      text = `${text} | ${resultText}`;
    }
    if (denials.length) {
      text = `${text} | ${t("logs.deniedTools", { tools: denials.join(", ") })}`;
    }
    return {
      id: log.id,
      ts: log.ts,
      kind: "result",
      text,
      title: log.payload,
    };
  }

  if (type === "system") {
    const subtype = String(parsed.subtype || "");
    if (subtype === "init") {
      const mcpServers = Array.isArray(parsed.mcp_servers) ? (parsed.mcp_servers as Array<Record<string, unknown>>) : [];
      const mcp = mcpServers
        .map((s) => `${String(s?.name || t("common.unknown"))}:${String(s?.status || t("common.unknown"))}`)
        .join(", ");
      const permissionMode = String(parsed.permissionMode || "");
      const fragments = [t("logs.sessionInit")];
      if (permissionMode) {
        fragments.push(t("logs.permission", { permissionMode }));
      }
      if (mcp) {
        fragments.push(t("logs.mcp", { mcp }));
      }
      const text = compactText(fragments.join(" | "));
      return {
        id: log.id,
        ts: log.ts,
        kind: "system.init",
        text,
        title: log.payload,
      };
    }
    return {
      id: log.id,
      ts: log.ts,
      kind: subtype ? `system.${subtype}` : "system",
      text: compactText(log.payload),
      title: log.payload,
    };
  }

  if (type === "user") {
    const message = parsed.message as Record<string, unknown> | undefined;
    const content = Array.isArray(message?.content) ? (message?.content as Array<Record<string, unknown>>) : [];
    for (const item of content) {
      if (String(item?.type || "") === "tool_result") {
        const isError = Boolean(item?.is_error);
        const result = compactText(String(item?.content || ""));
        if (!result) {
          return null;
        }
        return {
          id: log.id,
          ts: log.ts,
          kind: isError ? "tool.error" : "tool.result",
          text: isError ? t("logs.toolError", { result }) : t("logs.toolOutput", { result }),
          title: log.payload,
        };
      }
    }
    return null;
  }

  return {
    id: log.id,
    ts: log.ts,
    kind: type,
    text: compactText(log.payload),
    title: log.payload,
  };
}

function toDisplayLog(log: RunLogEvent, t: Translate): DisplayLogEvent | null {
  if (
    log.kind === "claude.stdout" ||
    log.kind === "claude.stderr" ||
    log.kind === "codex.stdout" ||
    log.kind === "codex.stderr"
  ) {
    return parseClaudeLogLine(log, t);
  }
  return {
    id: log.id,
    ts: log.ts,
    kind: log.kind,
    text: compactText(log.payload),
    title: log.payload,
  };
}

type IconProps = {
  className?: string;
};

function Icon({ className, children }: PropsWithChildren<IconProps>) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

function LogoIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="4" y="4" width="6" height="6" rx="1.4" />
      <rect x="14" y="4" width="6" height="6" rx="1.4" />
      <rect x="9" y="14" width="6" height="6" rx="1.4" />
      <path d="M10 7h4" />
      <path d="M7 10v4" />
      <path d="M17 10v4" />
      <path d="M12 14v-4" />
    </Icon>
  );
}

function FolderIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H9l2.1 2.2H18.5A2.5 2.5 0 0 1 21 9.7v7.8A2.5 2.5 0 0 1 18.5 20h-13A2.5 2.5 0 0 1 3 17.5z" />
    </Icon>
  );
}

function TaskIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M8 4h8l4 4v11a1 1 0 0 1-1 1H8a4 4 0 0 1-4-4V8a4 4 0 0 1 4-4z" />
      <path d="M16 4v5h5" />
      <path d="m9 14 2.2 2.2L15.8 11.5" />
    </Icon>
  );
}

function GlobeIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3a14 14 0 0 1 0 18" />
      <path d="M12 3a14 14 0 0 0 0 18" />
    </Icon>
  );
}

function SettingsIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 3.75 13.2 5.8l2.35.5 1.65 1.66-.5 2.33L18.75 12l-2.05 1.2.5 2.35-1.66 1.65-2.33-.5L12 20.25l-1.2-2.05-2.35-.5-1.65-1.66.5-2.33L5.25 12l2.05-1.2-.5-2.35 1.66-1.65 2.33.5z" />
      <circle cx="12" cy="12" r="3.1" />
    </Icon>
  );
}

function RefreshIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M20 11a8 8 0 0 0-14-4" />
      <path d="M4 5v5h5" />
      <path d="M4 13a8 8 0 0 0 14 4" />
      <path d="M20 19v-5h-5" />
    </Icon>
  );
}

function PanelIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="3" y="5" width="18" height="14" rx="2.5" />
      <path d="M7 9h6" />
      <path d="M7 13h10" />
      <path d="M7 17h4" />
    </Icon>
  );
}

function PlusIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 5v14" />
      <path d="M5 12h14" />
    </Icon>
  );
}

function PlayIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="m8 6 10 6-10 6z" />
    </Icon>
  );
}

function CheckCircleIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="8" />
      <path d="m8.7 12.2 2.2 2.2 4.7-5" />
    </Icon>
  );
}

function WindowMinimiseIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M5 12h14" />
    </Icon>
  );
}

function WindowMaximiseIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="5" y="5" width="14" height="14" rx="1.5" />
    </Icon>
  );
}

function WindowRestoreIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M9 5h10v10" />
      <path d="M5 9h10v10" />
    </Icon>
  );
}

function WindowCloseIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="m6 6 12 12" />
      <path d="M18 6 6 18" />
    </Icon>
  );
}

function App() {
  const { t, i18n } = useTranslation();
  const tr = useCallback<Translate>(
    (key, options) => {
      if (options === undefined) {
        return String(t(key));
      }
      return String(t(key, options as never));
    },
    [t],
  );

  const currentLanguage = normalizeLanguage(i18n.resolvedLanguage || i18n.language);

  const statusText = useMemo<Record<string, string>>(
    () => ({
      pending: tr("status.pending"),
      running: tr("status.running"),
      done: tr("status.done"),
      failed: tr("status.failed"),
      blocked: tr("status.blocked"),
    }),
    [currentLanguage, tr],
  );

  const mcpStateText = useMemo<Record<MCPStatus["state"], string>>(
    () => ({
      disabled: tr("mcpState.disabled"),
      connected: tr("mcpState.connected"),
      failed: tr("mcpState.failed"),
      unknown: tr("mcpState.unknown"),
    }),
    [currentLanguage, tr],
  );

  const api = useMemo(() => {
    const win = window as Window & { go?: { main?: { App?: AppAPI } } };
    return win.go?.main?.App;
  }, []);

  const [platform, setPlatform] = useState<OSPlatform>(() =>
    typeof document === "undefined" ? "unknown" : normalizePlatform(document.body.dataset.platform),
  );
  const [isWindowMaximised, setIsWindowMaximised] = useState(false);
  const [health, setHealth] = useState(HEALTH_MESSAGE_CHECKING);
  const [mcpStatus, setMCPStatus] = useState<MCPStatus>({
    enabled: true,
    state: "unknown",
    message: "",
  });
  const [autoRunEnabled, setAutoRunEnabled] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProjectID, setSelectedProjectID] = useState("");
  const [tasks, setTasks] = useState<Task[]>([]);
  const [error, setError] = useState("");
  const [info, setInfo] = useState("");
  const [toast, setToast] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [lastRunID, setLastRunID] = useState("");
  const [showProjectModal, setShowProjectModal] = useState(false);
  const [showTaskModal, setShowTaskModal] = useState(false);
  const [editingTaskID, setEditingTaskID] = useState("");
  const [page, setPage] = useState<"home" | "project" | "settings" | "systemLogs">("home");
  const [pendingDeleteProject, setPendingDeleteProject] = useState<{ projectID: string; projectName: string } | null>(null);
  const [pendingDeleteTask, setPendingDeleteTask] = useState<{ taskID: string; taskName: string } | null>(null);
  const [savingGlobalSettings, setSavingGlobalSettings] = useState(false);
  const [runningRuns, setRunningRuns] = useState<RunningRun[]>([]);
  const [selectedRunID, setSelectedRunID] = useState("");
  const [runLogs, setRunLogs] = useState<RunLogEvent[]>([]);
  const logStreamRef = useRef<HTMLDivElement | null>(null);
  const projectNameInputRef = useRef<HTMLInputElement | null>(null);
  const taskTitleInputRef = useRef<HTMLInputElement | null>(null);
  const [showTaskDetailModal, setShowTaskDetailModal] = useState(false);
  const [selectedDetailTask, setSelectedDetailTask] = useState<Task | null>(null);
  const [taskDetailRuns, setTaskDetailRuns] = useState<TaskRunHistory[]>([]);
  const [systemLogs, setSystemLogs] = useState<SystemLogEvent[]>([]);
  const [loadingSystemLogs, setLoadingSystemLogs] = useState(false);

  const [projectName, setProjectName] = useState("");
  const [projectPath, setProjectPath] = useState("");
  const [newProjectProvider, setNewProjectProvider] = useState("claude");
  const [newProjectFailurePolicy, setNewProjectFailurePolicy] = useState("block");
  const [newProjectModel, setNewProjectModel] = useState("");
  const [projectEditName, setProjectEditName] = useState("");
  const [projectDefaultProvider, setProjectDefaultProvider] = useState("claude");
  const [projectFailurePolicy, setProjectFailurePolicy] = useState("block");
  const [projectModel, setProjectModel] = useState("");
  const [projectSystemPrompt, setProjectSystemPrompt] = useState("");
  const [savingProjectAIConfig, setSavingProjectAIConfig] = useState(false);

  const [taskTitle, setTaskTitle] = useState("");
  const [taskDesc, setTaskDesc] = useState("");
  const [taskPriority, setTaskPriority] = useState("");
  const [projectFormError, setProjectFormError] = useState("");
  const [taskFormError, setTaskFormError] = useState("");
  const [projectSettingsFormError, setProjectSettingsFormError] = useState("");
  const [settingsFormError, setSettingsFormError] = useState("");
  const [projectSubmitAttempted, setProjectSubmitAttempted] = useState(false);
  const [taskSubmitAttempted, setTaskSubmitAttempted] = useState(false);
  const [telegramEnabled, setTelegramEnabled] = useState(false);
  const [telegramBotToken, setTelegramBotToken] = useState("");
  const [telegramChatIDs, setTelegramChatIDs] = useState("");
  const [telegramPollTimeout, setTelegramPollTimeout] = useState(30);
  const [telegramProxyURL, setTelegramProxyURL] = useState("");
  const [systemPrompt, setSystemPrompt] = useState("");

  const syncWindowMaximised = useCallback(() => {
    if (platform !== "windows") {
      setIsWindowMaximised(false);
      return;
    }
    void WindowIsMaximised()
      .then((value) => {
        setIsWindowMaximised(value);
      })
      .catch(() => {
        setIsWindowMaximised(false);
      });
  }, [platform]);

  useEffect(() => {
    let cancelled = false;
    void Environment()
      .then((env) => {
        if (cancelled) {
          return;
        }
        const nextPlatform = normalizePlatform(env.platform);
        setPlatform(nextPlatform);
        applyPlatformClass(nextPlatform);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setPlatform("unknown");
        applyPlatformClass("unknown");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (platform !== "windows") {
      setIsWindowMaximised(false);
      return;
    }
    syncWindowMaximised();
    window.addEventListener("resize", syncWindowMaximised);
    return () => {
      window.removeEventListener("resize", syncWindowMaximised);
    };
  }, [platform, syncWindowMaximised]);

  const displayLogs = useMemo(() => {
    const out: DisplayLogEvent[] = [];
    for (const item of runLogs) {
      const parsed = toDisplayLog(item, tr);
      if (!parsed || !parsed.text) {
        continue;
      }
      out.push(parsed);
    }
    return out;
  }, [runLogs, tr, currentLanguage]);

  const lastDisplayLogID = displayLogs.length ? displayLogs[displayLogs.length - 1].id : "";
  const orderedTasks = useMemo(() => {
    return [...tasks].sort((a, b) => {
      if (a.Priority !== b.Priority) {
        return a.Priority - b.Priority;
      }
      const aTime = Date.parse(a.CreatedAt);
      const bTime = Date.parse(b.CreatedAt);
      if (!Number.isNaN(aTime) && !Number.isNaN(bTime) && aTime !== bTime) {
        return aTime - bTime;
      }
      return a.ID.localeCompare(b.ID);
    });
  }, [tasks]);
  const selectedProject = useMemo(
    () => projects.find((p) => p.ID === selectedProjectID) || null,
    [projects, selectedProjectID],
  );
  const runningTaskIDs = useMemo(() => {
    return new Set(runningRuns.map((run) => run.task_id));
  }, [runningRuns]);

  const selectedRunIsLive = useMemo(() => {
    if (!selectedRunID) {
      return false;
    }
    const detailRun = taskDetailRuns.find((run) => run.run_id === selectedRunID);
    if (detailRun) {
      return detailRun.status === "running";
    }
    return runningRuns.some((run) => run.run_id === selectedRunID && run.status === "running");
  }, [runningRuns, selectedRunID, taskDetailRuns]);
  const pollTimeoutNumber = Number(telegramPollTimeout);
  const isPollTimeoutInvalid = !Number.isInteger(pollTimeoutNumber) || pollTimeoutNumber < 1 || pollTimeoutNumber > 120;
  const healthText = useMemo(() => renderHealthMessage(health, tr), [health, currentLanguage, tr]);
  const selectedProjectName = selectedProject?.Name || tr("home.selectProject");
  const selectedProjectPath = selectedProjectID
    ? selectedProject?.Path || tr("home.projectPathNotFound")
    : tr("home.selectProjectFirst");
  const systemLogScopeName = selectedProjectID
    ? selectedProject?.Name || tr("home.projectPathNotFound")
    : tr("systemLogs.allProjects");
  const latestSystemLogTime = systemLogs.length > 0 ? formatDateTime(systemLogs[0].ts) : tr("common.dash");
  const dispatchModeText = autoRunEnabled ? tr("home.autoDispatchMode") : tr("home.manualDispatchMode");
  const dispatchStatusText = useMemo(
    () =>
      lastRunID
        ? tr("home.dispatchLatestRun", { runId: lastRunID.slice(0, 8) })
        : tr("home.dispatchIdle"),
    [currentLanguage, lastRunID, tr],
  );

  const onChangeLanguage = useCallback(
    (language: string) => {
      const normalized = normalizeLanguage(language);
      window.localStorage.setItem(LANGUAGE_STORAGE_KEY, normalized);
      void i18n.changeLanguage(normalized);
    },
    [i18n],
  );

  const renderLanguageSwitcher = (idSuffix: string) => (
    <div className="locale-switch">
      <label htmlFor={`language-select-${idSuffix}`}>{tr("language.label")}</label>
      <select
        id={`language-select-${idSuffix}`}
        value={currentLanguage}
        onChange={(e) => onChangeLanguage(e.target.value)}
      >
        <option value="zh-CN">{tr("language.zhCN")}</option>
        <option value="en-US">{tr("language.enUS")}</option>
      </select>
    </div>
  );

  useEffect(() => {
    if (!api) return;
    let cancelled = false;

    const bootstrap = async () => {
      for (let attempt = 1; attempt <= 20; attempt++) {
        if (cancelled) return;
        try {
          const h = await api.Health();
          if (!cancelled) {
            setHealth(h);
          }
        } catch {
          if (!cancelled) {
            setHealth(HEALTH_MESSAGE_BACKEND_NOT_READY);
          }
          await wait(400);
          continue;
        }

        const [settingsOK, listOK] = await Promise.all([
          refreshGlobalSettings(),
          refreshAll(undefined, attempt === 1),
        ]);
        if (settingsOK && listOK) {
          return;
        }
        await wait(400);
      }
    };

    void bootstrap();
    return () => {
      cancelled = true;
    };
  }, [api, tr]);

  useEffect(() => {
    if (!api || !selectedProjectID) {
      setAutoRunEnabled(false);
      return;
    }
    api
      .AutoRunEnabled(selectedProjectID)
      .then(setAutoRunEnabled)
      .catch((err) => setError(formatBackendError(err, tr)));
  }, [api, selectedProjectID, tr]);

  useEffect(() => {
    if (!selectedProject) {
      setProjectEditName("");
      setProjectDefaultProvider("claude");
      setProjectFailurePolicy("block");
      setProjectModel("");
      setProjectSystemPrompt("");
      return;
    }
    setProjectEditName(selectedProject.Name || "");
    setProjectDefaultProvider(selectedProject.DefaultProvider || "claude");
    setProjectFailurePolicy(selectedProject.FailurePolicy || "block");
    setProjectModel(selectedProject.Model || "");
    setProjectSystemPrompt(selectedProject.SystemPrompt || "");
  }, [selectedProject]);

  useEffect(() => {
    if (!api) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const status = await api.MCPStatus();
        if (!cancelled) {
          setMCPStatus(status);
        }
      } catch (err) {
        if (!cancelled) {
          setMCPStatus({
            enabled: true,
            state: "failed",
            message: formatBackendError(err, tr),
          });
        }
      }
    };

    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, 3000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, selectedProjectID, selectedRunID, tr]);

  useEffect(() => {
    const runtimeBridge = (window as Window & { runtime?: { EventsOnMultiple?: unknown } }).runtime;
    if (!runtimeBridge?.EventsOnMultiple) {
      return;
    }

    let off = () => {};
    try {
      off = EventsOn("telegram.incoming", (...eventData: unknown[]) => {
        const notice = formatTelegramIncomingNotice(eventData[0], tr);
        if (!notice) {
          return;
        }
        setInfo(notice);
      });
    } catch {
      return;
    }

    return () => {
      off();
    };
  }, [tr]);

  useEffect(() => {
    if (!info) return;
    const message = info;
    setToast({ type: "success", text: message });
    const toastTimer = window.setTimeout(() => {
      setToast((prev) => (prev?.type === "success" && prev.text === message ? null : prev));
    }, NOTICE_AUTO_CLOSE_MS);
    const clearInfoTimer = window.setTimeout(() => {
      setInfo("");
    }, NOTICE_AUTO_CLOSE_MS);
    return () => {
      window.clearTimeout(toastTimer);
      window.clearTimeout(clearInfoTimer);
    };
  }, [info]);

  useEffect(() => {
    if (!error) return;
    const message = error;
    setToast({ type: "error", text: message });
    const toastTimer = window.setTimeout(() => {
      setToast((prev) => (prev?.type === "error" && prev.text === message ? null : prev));
    }, NOTICE_AUTO_CLOSE_MS);
    const clearErrorTimer = window.setTimeout(() => {
      setError("");
    }, NOTICE_AUTO_CLOSE_MS);
    return () => {
      window.clearTimeout(toastTimer);
      window.clearTimeout(clearErrorTimer);
    };
  }, [error]);

  useEffect(() => {
    if (!api || !selectedProjectID) {
      setRunningRuns([]);
      if (!showTaskDetailModal) {
        setSelectedRunID("");
        setRunLogs([]);
      }
      return;
    }

    let cancelled = false;
    const tick = async () => {
      try {
        const runs = await api.ListRunningRuns(selectedProjectID, 20);
        if (cancelled) return;
        setRunningRuns(runs);

        if (!runs.length) {
          if (!showTaskDetailModal) {
            setSelectedRunID("");
            setRunLogs([]);
          }
          return;
        }
        const found = runs.some((r) => r.run_id === selectedRunID);
        if (!found && !showTaskDetailModal) {
          setSelectedRunID(runs[0].run_id);
        }
      } catch (err) {
        if (!cancelled) setError(formatBackendError(err, tr));
      }
    };

    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, selectedProjectID, selectedRunID, showTaskDetailModal, tr]);

  useEffect(() => {
    if (!api || !selectedRunID) {
      setRunLogs([]);
      return;
    }

    let cancelled = false;
    const tick = async () => {
      try {
        const logs = await api.ListRunLogs(selectedRunID, 500);
        if (cancelled) return;
        setRunLogs(logs);
      } catch (err) {
        if (!cancelled) setError(formatBackendError(err, tr));
      }
    };

    void tick();
    if (!selectedRunIsLive) {
      return () => {
        cancelled = true;
      };
    }

    const timer = window.setInterval(() => {
      void tick();
    }, 1200);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, selectedRunID, selectedRunIsLive, tr]);

  useEffect(() => {
    const el = logStreamRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [selectedRunID, displayLogs.length, lastDisplayLogID]);

  useEffect(() => {
    if (!api || !selectedProjectID) return;
    let cancelled = false;
    const tick = async () => {
      if (cancelled) return;
      await refreshTasks(selectedProjectID);
    };
    const timer = window.setInterval(() => {
      void tick();
    }, 2500);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, selectedProjectID]);

  useEffect(() => {
    if (!api || page !== "systemLogs") return;
    let cancelled = false;

    const tick = async () => {
      if (cancelled) return;
      try {
        const logs = await api.ListSystemLogs(selectedProjectID, 300);
        if (cancelled) return;
        setSystemLogs(logs);
      } catch (err) {
        if (!cancelled) {
          setError(formatBackendError(err, tr));
        }
      }
    };

    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, 1000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, page, selectedProjectID, tr]);

  useEffect(() => {
    if (!api || !showTaskDetailModal || !selectedDetailTask) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const detail = await api.GetTaskDetail(selectedDetailTask.ID);
        if (cancelled || !detail) return;
        setSelectedDetailTask(detail.task);
        setTaskDetailRuns(detail.runs);
        if (!detail.runs.some((r) => r.run_id === selectedRunID)) {
          setSelectedRunID(detail.runs[0]?.run_id || "");
        }
      } catch (err) {
        if (!cancelled) setError(formatBackendError(err, tr));
      }
    };

    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [api, showTaskDetailModal, selectedDetailTask?.ID, selectedRunID, tr]);

  const closeTaskDetailModal = useCallback(() => {
    setShowTaskDetailModal(false);
    setSelectedDetailTask(null);
    setTaskDetailRuns([]);
    setSelectedRunID("");
    setRunLogs([]);
  }, []);

  const closeProjectModal = useCallback(() => {
    setShowProjectModal(false);
    setProjectFormError("");
    setProjectSubmitAttempted(false);
  }, []);

  const closeTaskModal = useCallback(() => {
    setShowTaskModal(false);
    setTaskFormError("");
    setTaskSubmitAttempted(false);
    setEditingTaskID("");
    setTaskTitle("");
    setTaskDesc("");
    setTaskPriority("");
  }, []);

  const closeDeleteProjectConfirm = useCallback(() => {
    setPendingDeleteProject(null);
  }, []);

  const closeDeleteTaskConfirm = useCallback(() => {
    setPendingDeleteTask(null);
  }, []);

  const openProjectModal = useCallback(() => {
    setProjectFormError("");
    setProjectSubmitAttempted(false);
    setNewProjectProvider("claude");
    setNewProjectFailurePolicy("block");
    setNewProjectModel("");
    setShowProjectModal(true);
  }, []);

  const openTaskModal = useCallback(() => {
    setEditingTaskID("");
    setTaskTitle("");
    setTaskDesc("");
    setTaskPriority("");
    setTaskFormError("");
    setTaskSubmitAttempted(false);
    setShowTaskModal(true);
  }, []);

  const openEditTaskModal = useCallback((task: Task) => {
    setEditingTaskID(task.ID);
    setTaskTitle(task.Title || "");
    setTaskDesc(task.Description || "");
    setTaskPriority(task.Priority > 0 ? String(task.Priority) : "");
    setTaskFormError("");
    setTaskSubmitAttempted(false);
    setShowTaskModal(true);
  }, []);

  useEffect(() => {
    if (showProjectModal) {
      projectNameInputRef.current?.focus();
    }
  }, [showProjectModal]);

  useEffect(() => {
    if (showTaskModal) {
      taskTitleInputRef.current?.focus();
    }
  }, [showTaskModal]);

  useEffect(() => {
    if (!showProjectModal && !showTaskModal && !showTaskDetailModal && !pendingDeleteProject && !pendingDeleteTask) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      if (pendingDeleteTask) {
        closeDeleteTaskConfirm();
        return;
      }
      if (pendingDeleteProject) {
        closeDeleteProjectConfirm();
        return;
      }
      if (showTaskDetailModal) {
        closeTaskDetailModal();
        return;
      }
      if (showTaskModal) {
        closeTaskModal();
        return;
      }
      if (showProjectModal) {
        closeProjectModal();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [
    closeDeleteTaskConfirm,
    closeDeleteProjectConfirm,
    closeProjectModal,
    closeTaskModal,
    closeTaskDetailModal,
    pendingDeleteTask,
    pendingDeleteProject,
    showProjectModal,
    showTaskModal,
    showTaskDetailModal,
  ]);

  async function refreshAll(preferredProjectID?: string, showLoading = true): Promise<boolean> {
    if (!api) return false;
    if (showLoading) {
      setLoading(true);
    }
    try {
      const projectList = await api.ListProjects(200);
      setProjects(projectList);
      const hasPreferredProjectID = preferredProjectID !== undefined;
      const selectedStillExists =
        selectedProjectID !== "" && projectList.some((project) => project.ID === selectedProjectID);
      const initialProjectID = hasPreferredProjectID
        ? preferredProjectID || projectList[0]?.ID || ""
        : selectedStillExists
          ? selectedProjectID
          : projectList[0]?.ID || "";
      setSelectedProjectID(initialProjectID);

      const taskList = initialProjectID ? await api.ListTasks("", "", initialProjectID, 300) : [];
      setTasks(taskList);
      setError("");
      return true;
    } catch (err) {
      setError(formatBackendError(err, tr));
      return false;
    } finally {
      if (showLoading) {
        setLoading(false);
      }
    }
  }

  async function refreshGlobalSettings(): Promise<boolean> {
    if (!api) return false;
    try {
      const settings = await api.GetGlobalSettings();
      setTelegramEnabled(Boolean(settings.telegram_enabled));
      setTelegramBotToken(settings.telegram_bot_token || "");
      setTelegramChatIDs(settings.telegram_chat_ids || "");
      setTelegramPollTimeout(Number(settings.telegram_poll_timeout) || 30);
      setTelegramProxyURL(settings.telegram_proxy_url || "");
      setSystemPrompt(settings.system_prompt || "");
      return true;
    } catch (err) {
      setError(formatBackendError(err, tr));
      return false;
    }
  }

  async function refreshTasks(projectID: string) {
    if (!api) return;
    if (!projectID) {
      setTasks([]);
      return;
    }
    try {
      const taskList = await api.ListTasks("", "", projectID, 300);
      setTasks(taskList);
      setError("");
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function refreshSystemLogs(projectID: string, showLoading = false) {
    if (!api) return;
    if (showLoading) {
      setLoadingSystemLogs(true);
    }
    try {
      const logs = await api.ListSystemLogs(projectID, 300);
      setSystemLogs(logs);
      setError("");
    } catch (err) {
      setError(formatBackendError(err, tr));
    } finally {
      if (showLoading) {
        setLoadingSystemLogs(false);
      }
    }
  }

  async function onCreateProject(e: FormEvent) {
    e.preventDefault();
    setProjectSubmitAttempted(true);
    setProjectFormError("");
    if (!api) return;
    if (!projectName.trim() || !projectPath.trim()) {
      setProjectFormError(tr("validation.projectNameAndPathRequired"));
      return;
    }
    try {
      const project = await api.CreateProject({
        name: projectName,
        path: projectPath,
        default_provider: newProjectProvider,
        model: newProjectModel.trim(),
        system_prompt: "",
        failure_policy: newProjectFailurePolicy,
      });
      setProjectName("");
      setProjectPath("");
      setNewProjectProvider("claude");
      setNewProjectFailurePolicy("block");
      setNewProjectModel("");
      setProjectFormError("");
      setProjectSubmitAttempted(false);
      setInfo(tr("info.projectCreated", { name: project.Name }));
      closeProjectModal();
      await refreshAll(project.ID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onCreateTask(e: FormEvent) {
    e.preventDefault();
    setTaskSubmitAttempted(true);
    setTaskFormError("");
    if (!api) return;
    const isEditingTask = editingTaskID !== "";
    if (!selectedProjectID && !isEditingTask) {
      setTaskFormError(tr("validation.selectProjectBeforeTask"));
      return;
    }
    if (!taskTitle.trim() || !taskDesc.trim()) {
      setTaskFormError(tr("validation.taskTitleAndDescriptionRequired"));
      return;
    }
    const priorityRaw = taskPriority.trim();
    let priority = 0;
    if (priorityRaw !== "") {
      const parsedPriority = Number(priorityRaw);
      if (!Number.isInteger(parsedPriority) || parsedPriority <= 0) {
        setTaskFormError(tr("info.priorityMustBePositiveInteger"));
        return;
      }
      priority = parsedPriority;
    }
    try {
      if (isEditingTask) {
        await api.UpdateTask({
          task_id: editingTaskID,
          title: taskTitle,
          description: taskDesc,
          priority,
          depends_on: [],
        });
      } else {
        await api.CreateTask({
          project_id: selectedProjectID,
          title: taskTitle,
          description: taskDesc,
          priority,
          depends_on: [],
          provider: "",
        });
      }
      setTaskTitle("");
      setTaskDesc("");
      setTaskPriority("");
      setTaskFormError("");
      setTaskSubmitAttempted(false);
      setInfo(isEditingTask ? tr("info.taskUpdated") : tr("info.taskCreated"));
      closeTaskModal();
      await refreshTasks(selectedProjectID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onDispatch() {
    if (!api) return;
    try {
      const res = await api.DispatchOnce("", selectedProjectID);
      if (!res.claimed) {
        setInfo(res.message || tr("info.noDispatchableTask"));
        return;
      }
      setLastRunID(res.run_id || "");
      setSelectedRunID(res.run_id || "");
      setInfo(res.message || tr("info.dispatchedTask", { taskId: res.task_id || tr("common.unknown") }));
      await refreshTasks(selectedProjectID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onToggleAutoRun() {
    if (!api || !selectedProjectID) return;
    try {
      const next = !autoRunEnabled;
      const enabled = await api.SetAutoRunEnabled(selectedProjectID, next);
      setAutoRunEnabled(enabled);
      setInfo(enabled ? tr("info.autoRunEnabled") : tr("info.autoRunDisabled"));
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onSaveProjectAIConfig(e: FormEvent) {
    e.preventDefault();
    setProjectSettingsFormError("");
    if (!api) return;
    if (!selectedProjectID) {
      setProjectSettingsFormError(tr("validation.selectProjectBeforeSaveSettings"));
      return;
    }
    if (!projectEditName.trim()) {
      setProjectSettingsFormError(tr("validation.projectNameRequired"));
      return;
    }
    setSavingProjectAIConfig(true);
    try {
      const updated = await api.UpdateProject({
        project_id: selectedProjectID,
        name: projectEditName.trim(),
        default_provider: projectDefaultProvider,
        model: projectModel.trim(),
        system_prompt: projectSystemPrompt,
        failure_policy: projectFailurePolicy,
      });
      setProjects((prev) => prev.map((item) => (item.ID === updated.ID ? updated : item)));
      setProjectEditName(updated.Name || "");
      setProjectDefaultProvider(updated.DefaultProvider || "claude");
      setProjectFailurePolicy(updated.FailurePolicy || "block");
      setProjectModel(updated.Model || "");
      setProjectSystemPrompt(updated.SystemPrompt || "");
      setInfo(tr("info.projectSettingsSaved"));
    } catch (err) {
      setError(formatBackendError(err, tr));
    } finally {
      setSavingProjectAIConfig(false);
    }
  }

  function onDeleteProject() {
    if (!selectedProjectID) {
      return;
    }
    setPendingDeleteProject({
      projectID: selectedProjectID,
      projectName: selectedProject?.Name || tr("common.unknown"),
    });
  }

  async function onDeleteProjectConfirmed() {
    if (!api || !pendingDeleteProject) return;
    const deletingProjectID = pendingDeleteProject.projectID;
    const deletingProjectName = pendingDeleteProject.projectName;
    closeDeleteProjectConfirm();
    try {
      await api.DeleteProject(deletingProjectID);
      setInfo(tr("info.projectDeleted", { name: deletingProjectName }));
      setPage("home");
      await refreshAll();
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onSaveGlobalSettings(e: FormEvent) {
    e.preventDefault();
    setSettingsFormError("");
    if (!api) return;
    if (isPollTimeoutInvalid) {
      setSettingsFormError(tr("validation.pollTimeoutRange"));
      return;
    }
    setSavingGlobalSettings(true);
    try {
      const updated = await api.UpdateGlobalSettings({
        telegram_enabled: telegramEnabled,
        telegram_bot_token: telegramBotToken.trim(),
        telegram_chat_ids: telegramChatIDs.trim(),
        telegram_poll_timeout: pollTimeoutNumber,
        telegram_proxy_url: telegramProxyURL.trim(),
        system_prompt: systemPrompt,
      });
      setTelegramEnabled(Boolean(updated.telegram_enabled));
      setTelegramBotToken(updated.telegram_bot_token || "");
      setTelegramChatIDs(updated.telegram_chat_ids || "");
      setTelegramPollTimeout(Number(updated.telegram_poll_timeout) || 30);
      setTelegramProxyURL(updated.telegram_proxy_url || "");
      setSystemPrompt(updated.system_prompt || "");
      setInfo(tr("info.globalSettingsSaved"));
    } catch (err) {
      setError(formatBackendError(err, tr));
    } finally {
      setSavingGlobalSettings(false);
    }
  }

  async function onMarkDone(taskID: string) {
    if (!api) return;
    try {
      await api.UpdateTaskStatus(taskID, "done");
      setInfo(tr("info.taskMarkedDone", { taskId: taskID.slice(0, 8) }));
      await refreshTasks(selectedProjectID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  function getSelectedRunInfo() {
    return taskDetailRuns.find((r) => r.run_id === selectedRunID) || null;
  }

  async function refreshTaskDetail(taskID: string, preferredRunID?: string) {
    if (!api) return;
    const detail = await api.GetTaskDetail(taskID);
    if (!detail) {
      throw new Error(tr("info.noTaskDetailData"));
    }
    setSelectedDetailTask(detail.task);
    setTaskDetailRuns(detail.runs);

    const runningRunID = runningRuns.find((r) => r.task_id === detail.task.ID)?.run_id;
    const candidate = preferredRunID || selectedRunID || runningRunID;
    if (candidate && detail.runs.some((r) => r.run_id === candidate)) {
      setSelectedRunID(candidate);
      return;
    }
    setSelectedRunID(detail.runs[0]?.run_id || "");
  }

  async function onOpenTaskDetail(task: Task) {
    if (!api) return;
    try {
      await refreshTaskDetail(task.ID);
      setShowTaskDetailModal(true);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  async function onRetryTask(taskID: string) {
    if (!api) return;
    try {
      await api.RetryTask(taskID);
      setInfo(tr("info.taskRetried", { taskId: taskID.slice(0, 8) }));
      await refreshTasks(selectedProjectID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  function onDeleteTask(task: Task) {
    if (task.Status === "running") {
      return;
    }
    setPendingDeleteTask({
      taskID: task.ID,
      taskName: task.Title || task.ID.slice(0, 8),
    });
  }

  async function onDeleteTaskConfirmed() {
    if (!api || !pendingDeleteTask) {
      return;
    }
    const deletingTaskID = pendingDeleteTask.taskID;
    closeDeleteTaskConfirm();
    try {
      await api.DeleteTask(deletingTaskID);
      setInfo(tr("info.taskDeleted", { taskId: deletingTaskID.slice(0, 8) }));
      await refreshTasks(selectedProjectID);
    } catch (err) {
      setError(formatBackendError(err, tr));
    }
  }

  function onChangeProject(projectID: string) {
    setSelectedProjectID(projectID);
    setProjectSettingsFormError("");
    closeTaskDetailModal();
    void refreshTasks(projectID);
    if (page === "systemLogs") {
      void refreshSystemLogs(projectID, true);
    }
  }

  function formatTime(value?: string) {
    if (!value) return tr("common.dash");
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleTimeString(currentLanguage, { hour12: false });
  }

  function formatDateTime(value?: string) {
    if (!value) return tr("common.dash");
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleString(currentLanguage, { hour12: false });
  }

  const isTaskDetailPage = showTaskDetailModal && !!selectedDetailTask;
  const windowChromeTitle =
    isTaskDetailPage && selectedDetailTask?.Title
      ? selectedDetailTask.Title
      : page === "settings"
        ? tr("settings.title")
        : page === "project"
          ? tr("project.title")
          : page === "systemLogs"
            ? tr("systemLogs.title")
            : tr("home.appTitle");
  const showWindowChrome = platform === "darwin" || platform === "windows";
  const appPageClassName = [
    "app-page",
    `app-page-${platform}`,
    showWindowChrome ? "app-page-native-chrome" : "",
  ].filter(Boolean).join(" ");

  const onToggleWindowMaximise = useCallback(() => {
    WindowToggleMaximise();
    window.setTimeout(() => {
      syncWindowMaximised();
    }, 80);
  }, [syncWindowMaximised]);

  return (
    <div className={appPageClassName}>
      <a className="skip-link" href="#main-content">
        {tr("a11y.skipToMain")}
      </a>
      {showWindowChrome && (
        <div
          className={`window-chrome window-chrome-${platform}`}
          onDoubleClick={platform === "windows" ? onToggleWindowMaximise : undefined}
        >
          <div className="window-chrome-leading">
            {platform === "windows" ? (
              <div className="window-chrome-appname" title={tr("home.appTitle")}>
                <LogoIcon className="window-chrome-app-icon" />
                <span>{tr("home.appTitle")}</span>
              </div>
            ) : (
              <span className="window-chrome-mac-spacer" aria-hidden="true" />
            )}
          </div>
          <div className="window-chrome-title" title={windowChromeTitle}>
            <span>{windowChromeTitle}</span>
          </div>
          <div className="window-chrome-actions">
            {platform === "windows" && (
              <>
                <button
                  type="button"
                  className="window-control-button"
                  onClick={WindowMinimise}
                  title={tr("window.minimise")}
                  aria-label={tr("window.minimise")}
                >
                  <WindowMinimiseIcon className="window-control-icon" />
                </button>
                <button
                  type="button"
                  className="window-control-button"
                  onClick={onToggleWindowMaximise}
                  title={isWindowMaximised ? tr("window.restore") : tr("window.maximise")}
                  aria-label={isWindowMaximised ? tr("window.restore") : tr("window.maximise")}
                >
                  {isWindowMaximised ? (
                    <WindowRestoreIcon className="window-control-icon" />
                  ) : (
                    <WindowMaximiseIcon className="window-control-icon" />
                  )}
                </button>
                <button
                  type="button"
                  className="window-control-button window-control-close"
                  onClick={Quit}
                  title={tr("window.close")}
                  aria-label={tr("window.close")}
                >
                  <WindowCloseIcon className="window-control-icon" />
                </button>
              </>
            )}
          </div>
        </div>
      )}
      {toast && (
        <div
          className={`toast toast-${toast.type}`}
          role={toast.type === "error" ? "alert" : "status"}
          aria-live={toast.type === "error" ? "assertive" : "polite"}
        >
          <span>{toast.text}</span>
        </div>
      )}
      {isTaskDetailPage ? (
        <main className="task-detail-page" id="main-content">
          <header className="home-topbar task-detail-homebar">
            <div className="home-topbar-brand task-detail-homebar-brand">
              <div className="home-topbar-brand-mark task-detail-homebar-mark" aria-hidden="true">
                <TaskIcon className="home-topbar-brand-icon" />
              </div>
              <div className="task-detail-homebar-copy">
                <strong>{tr("detail.title")}</strong>
                <span>{selectedProjectName}</span>
              </div>
            </div>

            <div className="home-topbar-divider" aria-hidden="true" />

            <div className="task-detail-homebar-summary" title={selectedDetailTask?.ID || tr("common.dash")}>
              <TaskIcon className="home-topbar-inline-icon" />
              <div className="task-detail-homebar-summary-copy">
                <strong>{selectedDetailTask?.Title || tr("common.dash")}</strong>
                <span>{selectedDetailTask?.ID ? selectedDetailTask.ID.slice(0, 8) : tr("common.dash")}</span>
              </div>
              <span className={`status status-${selectedDetailTask?.Status || "pending"}`}>
                {selectedDetailTask ? statusText[selectedDetailTask.Status] || selectedDetailTask.Status : tr("common.dash")}
              </span>
            </div>

            <div className="home-topbar-side task-detail-homebar-side">
              <div className="status-pill home-topbar-pill" title={healthText} aria-label={`${tr("common.systemRunning")} · ${healthText}`}>
                <span className="status-dot" />
                <strong>{tr("common.systemRunning")}</strong>
              </div>
              <div
                className={`status-pill status-pill-mcp status-pill-${mcpStatus.state} home-topbar-pill`}
                title={mcpStatus.message || mcpStateText[mcpStatus.state]}
                aria-label={`${tr("detail.mcpStatusLabel")} ${mcpStateText[mcpStatus.state]}`}
              >
                <span className="status-dot" />
                <strong>{mcpStateText[mcpStatus.state]}</strong>
              </div>
              <label className="sr-only" htmlFor="language-select-detail-compact">
                {tr("language.label")}
              </label>
              <div className="home-topbar-language">
                <GlobeIcon className="home-topbar-inline-icon" />
                <select
                  id="language-select-detail-compact"
                  className="home-topbar-select"
                  value={currentLanguage}
                  onChange={(e) => onChangeLanguage(e.target.value)}
                  aria-label={tr("language.label")}
                >
                  <option value="zh-CN">{tr("language.zhCN")}</option>
                  <option value="en-US">{tr("language.enUS")}</option>
                </select>
              </div>
              <button
                type="button"
                className="home-topbar-icon-button"
                title={tr("systemLogs.navButton")}
                aria-label={tr("systemLogs.navButton")}
                onClick={() => setPage("systemLogs")}
              >
                <PanelIcon className="home-topbar-button-icon" />
              </button>
              <button
                type="button"
                className="home-topbar-icon-button"
                title={tr("common.settings")}
                aria-label={tr("common.settings")}
                onClick={() => {
                  setPage("settings");
                  void refreshGlobalSettings();
                }}
              >
                <SettingsIcon className="home-topbar-button-icon" />
              </button>
              <button type="button" className="home-secondary-action task-detail-homebar-back" onClick={closeTaskDetailModal}>
                {tr("detail.backToTaskList")}
              </button>
            </div>
          </header>

          <section className="panel task-detail-header-panel">
            <div className="task-detail-summary">
              <div className="task-detail-summary-top">
                <h3>{selectedDetailTask?.Title || tr("common.dash")}</h3>
                <span className={`status status-${selectedDetailTask?.Status || "pending"}`}>
                  {selectedDetailTask ? statusText[selectedDetailTask.Status] || selectedDetailTask.Status : tr("common.dash")}
                </span>
              </div>
              <dl className="task-detail-meta-grid">
                <div>
                  <dt>{tr("detail.taskId")}</dt>
                  <dd>{selectedDetailTask?.ID || tr("common.dash")}</dd>
                </div>
                <div>
                  <dt>{tr("detail.provider")}</dt>
                  <dd>{selectedDetailTask?.Provider || tr("common.unassigned")}</dd>
                </div>
                <div>
                  <dt>{tr("detail.priority")}</dt>
                  <dd>{selectedDetailTask?.Priority ?? tr("common.dash")}</dd>
                </div>
                <div>
                  <dt>{tr("detail.projectId")}</dt>
                  <dd>{selectedDetailTask?.ProjectID || tr("common.dash")}</dd>
                </div>
                <div>
                  <dt>{tr("detail.updatedAt")}</dt>
                  <dd>{formatDateTime(selectedDetailTask?.UpdatedAt)}</dd>
                </div>
                {selectedDetailTask?.Status === "failed" && (
                  <>
                    <div>
                      <dt>{tr("detail.executionCount")}</dt>
                      <dd>{selectedDetailTask?.RetryCount ?? 0}</dd>
                    </div>
                    <div>
                      <dt>{tr("detail.nextRunAt")}</dt>
                      <dd>{formatDateTime(selectedDetailTask?.NextRetryAt)}</dd>
                    </div>
                  </>
                )}
              </dl>
            </div>
            <p className="task-detail-desc">{selectedDetailTask?.Description || tr("common.noTaskDescription")}</p>
          </section>

          <section className="panel task-detail-content">
            <div className="task-detail-columns">
              <aside className="task-detail-history">
                <h3>{tr("detail.historyTitle")}</h3>
                {taskDetailRuns.length === 0 ? (
                  <p className="empty">{tr("detail.noRunHistory")}</p>
                ) : (
                  <div className="run-history-list" role="listbox" aria-label={tr("detail.historyTitle")}>
                    {taskDetailRuns.map((run) => (
                      <button
                        key={run.run_id}
                        type="button"
                        className={`run-history-item ${selectedRunID === run.run_id ? "run-history-item-active" : ""}`}
                        onClick={() => setSelectedRunID(run.run_id)}
                        aria-selected={selectedRunID === run.run_id}
                        role="option"
                      >
                        <span className="run-history-main">
                          #{run.attempt} · {statusText[run.status] || run.status}
                        </span>
                        <span className="run-history-sub">{formatDateTime(run.started_at)}</span>
                      </button>
                    ))}
                  </div>
                )}
              </aside>

              <section className="task-detail-logs">
                <p className="run-info">{tr("detail.runInfo", { runId: selectedRunID || tr("common.none") })}</p>
                <div className="logbox" aria-busy={selectedRunID !== "" && displayLogs.length === 0}>
                  {!selectedRunID ? (
                    <p className="empty">{tr("detail.noRunInstances")}</p>
                  ) : displayLogs.length === 0 ? (
                    <div className="empty">
                      <p>{tr("detail.waitingLogs")}</p>
                      {getSelectedRunInfo()?.result_summary && (
                        <p>
                          {tr("detail.summary")}: {getSelectedRunInfo()?.result_summary}
                        </p>
                      )}
                      {getSelectedRunInfo()?.result_details && (
                        <p>
                          {tr("detail.details")}: {getSelectedRunInfo()?.result_details}
                        </p>
                      )}
                    </div>
                  ) : (
                    <div className="log-stream" ref={logStreamRef} aria-live="polite">
                      {displayLogs.map((log) => (
                        <div className="log-line" key={log.id}>
                          <span className="log-time">{formatTime(log.ts)}</span>
                          <span className="log-kind">{log.kind}</span>
                          <span className="log-text" title={log.title}>
                            {log.text}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </section>
            </div>
          </section>
        </main>
      ) : page === "project" ? (
        <main className="settings-page" id="main-content">
          <header className="settings-topbar">
            <button type="button" className="btn-ghost settings-back" onClick={() => setPage("home")}>
              {tr("project.backHome")}
            </button>
            <div className="settings-title-wrap">
              <h2>{tr("project.title")}</h2>
              <p>{tr("project.subtitle")}</p>
            </div>
            <div className="status-group">
              {renderLanguageSwitcher("project")}
              <div className="status-pill" title={healthText} aria-label={`${tr("common.systemRunning")} · ${healthText}`}>
                <span className="status-dot" />
                <strong>{tr("common.systemRunning")}</strong>
              </div>
              <div
                className={`status-pill status-pill-mcp status-pill-${mcpStatus.state}`}
                title={mcpStatus.message || mcpStateText[mcpStatus.state]}
                aria-label={`${tr("detail.mcpStatusLabel")} ${mcpStateText[mcpStatus.state]}`}
              >
                <span className="status-dot" />
                <strong>{mcpStateText[mcpStatus.state]}</strong>
              </div>
            </div>
          </header>

          <section className="panel settings-panel">
            <div className="actions-header">
              <h2>{tr("project.projectTitle")}</h2>
              <div className="actions">
                <button
                  type="submit"
                  form="project-settings-form"
                  disabled={!selectedProjectID || savingProjectAIConfig}
                >
                  {savingProjectAIConfig ? tr("common.saving") : tr("project.saveProjectSettings")}
                </button>
                <button type="button" className="btn-danger" onClick={onDeleteProject} disabled={!selectedProjectID}>
                  {tr("project.deleteProject")}
                </button>
              </div>
            </div>
            <form id="project-settings-form" className="form settings-form" onSubmit={onSaveProjectAIConfig}>
              {projectSettingsFormError && (
                <p className="field-error" role="alert">
                  {projectSettingsFormError}
                </p>
              )}
              <label htmlFor="project-detail-select">{tr("home.currentProject")}</label>
              <select
                id="project-detail-select"
                value={selectedProjectID}
                onChange={(e) => onChangeProject(e.target.value)}
              >
                <option value="">{tr("home.selectProject")}</option>
                {projects.map((project) => (
                  <option key={project.ID} value={project.ID}>
                    {project.Name}
                  </option>
                ))}
              </select>

              <label htmlFor="project-detail-name">{tr("project.projectName")}</label>
              <input
                id="project-detail-name"
                value={projectEditName}
                onChange={(e) => {
                  setProjectEditName(e.target.value);
                  setProjectSettingsFormError("");
                }}
                placeholder={tr("modal.projectNamePlaceholder")}
                disabled={!selectedProjectID}
              />

              <p className="run-info">
                {selectedProjectID
                  ? tr("project.projectPathValue", {
                      path: selectedProject?.Path || tr("home.projectPathNotFound"),
                    })
                  : tr("project.projectRequiredHint")}
              </p>

              <label htmlFor="project-detail-provider">{tr("home.defaultProvider")}</label>
              <select
                id="project-detail-provider"
                value={projectDefaultProvider}
                onChange={(e) => {
                  setProjectDefaultProvider(e.target.value);
                  setProjectSettingsFormError("");
                }}
                disabled={!selectedProjectID}
              >
                <option value="claude">claude</option>
                <option value="codex">codex</option>
              </select>

              <label htmlFor="project-detail-failure-policy">{tr("project.failurePolicy")}</label>
              <select
                id="project-detail-failure-policy"
                value={projectFailurePolicy}
                onChange={(e) => {
                  setProjectFailurePolicy(e.target.value);
                  setProjectSettingsFormError("");
                }}
                disabled={!selectedProjectID}
              >
                <option value="block">{tr("project.failurePolicyBlock")}</option>
                <option value="continue">{tr("project.failurePolicyContinue")}</option>
              </select>
              <p className="run-info">
                {projectFailurePolicy === "continue"
                  ? tr("project.failurePolicyContinueHint")
                  : tr("project.failurePolicyBlockHint")}
              </p>

              <label htmlFor="project-detail-model">{tr("home.modelOptional")}</label>
              <input
                id="project-detail-model"
                value={projectModel}
                onChange={(e) => {
                  setProjectModel(e.target.value);
                  setProjectSettingsFormError("");
                }}
                placeholder={tr("home.modelPlaceholder")}
                disabled={!selectedProjectID}
              />

              <label htmlFor="project-detail-system-prompt">{tr("project.projectSystemPrompt")}</label>
              <textarea
                id="project-detail-system-prompt"
                value={projectSystemPrompt}
                onChange={(e) => {
                  setProjectSystemPrompt(e.target.value);
                  setProjectSettingsFormError("");
                }}
                placeholder={tr("project.projectSystemPromptPlaceholder")}
                disabled={!selectedProjectID}
              />
              <p className="run-info">{tr("project.projectPersistenceHint")}</p>
            </form>
          </section>
        </main>
      ) : page === "settings" ? (
        <main className="settings-page" id="main-content">
          <header className="settings-topbar">
            <button type="button" className="btn-ghost settings-back" onClick={() => setPage("home")}>
              {tr("settings.backHome")}
            </button>
            <div className="settings-title-wrap">
              <h2>{tr("settings.title")}</h2>
              <p>{tr("settings.subtitle")}</p>
            </div>
            <div className="status-group">
              {renderLanguageSwitcher("settings")}
              <div className="status-pill" title={healthText} aria-label={`${tr("common.systemRunning")} · ${healthText}`}>
                <span className="status-dot" />
                <strong>{tr("common.systemRunning")}</strong>
              </div>
              <div
                className={`status-pill status-pill-mcp status-pill-${mcpStatus.state}`}
                title={mcpStatus.message || mcpStateText[mcpStatus.state]}
                aria-label={`${tr("detail.mcpStatusLabel")} ${mcpStateText[mcpStatus.state]}`}
              >
                <span className="status-dot" />
                <strong>{mcpStateText[mcpStatus.state]}</strong>
              </div>
            </div>
          </header>

          <section className="panel settings-panel">
            <div className="actions-header">
              <h2>{tr("settings.telegramTitle")}</h2>
              <button type="submit" form="global-settings-form" disabled={savingGlobalSettings}>
                {savingGlobalSettings ? tr("common.saving") : tr("settings.saveSettings")}
              </button>
            </div>
            <form id="global-settings-form" className="form settings-form" onSubmit={onSaveGlobalSettings}>
              {settingsFormError && (
                <p className="field-error" role="alert">
                  {settingsFormError}
                </p>
              )}
              <label className="checkbox-row" htmlFor="telegram-enabled">
                <input
                  id="telegram-enabled"
                  type="checkbox"
                  checked={telegramEnabled}
                  onChange={(e) => {
                    setTelegramEnabled(e.target.checked);
                    setSettingsFormError("");
                  }}
                />
                <span>{tr("settings.enableTelegram")}</span>
              </label>

              <label htmlFor="telegram-token">{tr("settings.botToken")}</label>
              <input
                id="telegram-token"
                type="password"
                value={telegramBotToken}
                onChange={(e) => {
                  setTelegramBotToken(e.target.value);
                  setSettingsFormError("");
                }}
                placeholder={tr("settings.botTokenPlaceholder")}
              />

              <label htmlFor="telegram-chat-ids">{tr("settings.chatIDs")}</label>
              <input
                id="telegram-chat-ids"
                value={telegramChatIDs}
                onChange={(e) => {
                  setTelegramChatIDs(e.target.value);
                  setSettingsFormError("");
                }}
                placeholder={tr("settings.chatIDsPlaceholder")}
              />

              <label htmlFor="telegram-poll-timeout">{tr("settings.pollTimeout")}</label>
              <input
                id="telegram-poll-timeout"
                type="number"
                min={1}
                max={120}
                value={telegramPollTimeout}
                onChange={(e) => {
                  setTelegramPollTimeout(Number(e.target.value));
                  setSettingsFormError("");
                }}
                aria-invalid={isPollTimeoutInvalid}
              />

              <label htmlFor="telegram-proxy-url">{tr("settings.proxyURL")}</label>
              <input
                id="telegram-proxy-url"
                value={telegramProxyURL}
                onChange={(e) => {
                  setTelegramProxyURL(e.target.value);
                  setSettingsFormError("");
                }}
                placeholder={tr("settings.proxyPlaceholder")}
              />

              <label htmlFor="system-prompt">{tr("settings.systemPrompt")}</label>
              <textarea
                id="system-prompt"
                value={systemPrompt}
                readOnly
                placeholder={tr("settings.systemPromptPlaceholder")}
              />
              <p className="run-info">{tr("settings.persistenceHint")}</p>
            </form>
          </section>
        </main>
      ) : page === "systemLogs" ? (
        <main className="system-logs-page" id="main-content">
          <header className="settings-topbar">
            <button type="button" className="btn-ghost settings-back" onClick={() => setPage("home")}>
              {tr("systemLogs.backHome")}
            </button>
            <div className="settings-title-wrap">
              <h2>{tr("systemLogs.pageTitle")}</h2>
              <p>{tr("systemLogs.subtitle")}</p>
            </div>
            <div className="status-group">
              {renderLanguageSwitcher("system-logs")}
              <div className="status-pill" title={healthText} aria-label={`${tr("common.systemRunning")} · ${healthText}`}>
                <span className="status-dot" />
                <strong>{tr("common.systemRunning")}</strong>
              </div>
              <div
                className={`status-pill status-pill-mcp status-pill-${mcpStatus.state}`}
                title={mcpStatus.message || mcpStateText[mcpStatus.state]}
                aria-label={`${tr("detail.mcpStatusLabel")} ${mcpStateText[mcpStatus.state]}`}
              >
                <span className="status-dot" />
                <strong>{mcpStateText[mcpStatus.state]}</strong>
              </div>
            </div>
          </header>

          <section className="panel settings-panel system-log-panel">
            <div className="system-log-toolbar">
              <div className="system-log-toolbar-copy">
                <span className="dashboard-section-kicker">{tr("systemLogs.title")}</span>
                <h2>{tr("systemLogs.pageTitle")}</h2>
                <p>{tr("systemLogs.autoRefreshHint")}</p>
              </div>
              <div className="system-log-toolbar-actions">
                <div className="system-log-live-pill" aria-label={tr("systemLogs.liveStatus")}>
                  <span className="system-log-live-dot" aria-hidden="true" />
                  <span>{tr("systemLogs.liveStatus")}</span>
                </div>
                <button
                  type="button"
                  className="btn-secondary"
                  onClick={() => void refreshSystemLogs(selectedProjectID, true)}
                  disabled={loadingSystemLogs}
                >
                  {loadingSystemLogs ? tr("common.refreshing") : tr("common.refreshData")}
                </button>
              </div>
            </div>

            <div className="system-log-controls">
              <div className="system-log-filter">
                <label htmlFor="system-log-project-select">{tr("home.currentProject")}</label>
                <select
                  id="system-log-project-select"
                  value={selectedProjectID}
                  onChange={(e) => onChangeProject(e.target.value)}
                >
                  <option value="">{tr("systemLogs.allProjects")}</option>
                  {projects.map((project) => (
                    <option key={project.ID} value={project.ID}>
                      {project.Name}
                    </option>
                  ))}
                </select>
              </div>

              <div className="system-log-stats">
                <div className="system-log-stat">
                  <span>{tr("systemLogs.scopeLabel")}</span>
                  <strong>{systemLogScopeName}</strong>
                </div>
                <div className="system-log-stat">
                  <span>{tr("systemLogs.countLabel")}</span>
                  <strong>{systemLogs.length}</strong>
                </div>
                <div className="system-log-stat">
                  <span>{tr("systemLogs.latestEventLabel")}</span>
                  <strong>{latestSystemLogTime}</strong>
                </div>
              </div>
            </div>

            <div className="logbox system-logbox" aria-busy={loadingSystemLogs}>
              {systemLogs.length === 0 ? (
                <p className="empty system-log-empty">{tr("systemLogs.noLogs")}</p>
              ) : (
                <div className="log-stream system-log-stream" aria-live="polite">
                  {systemLogs.map((log) => {
                    const { source, channel } = splitLogKind(log.kind);
                    return (
                      <article className="system-log-card" key={log.id}>
                        <div className="system-log-card-head">
                          <div className="system-log-card-meta">
                            <span className="system-log-time">{formatDateTime(log.ts)}</span>
                            <span className={`system-log-badge system-log-badge-${channel.toLowerCase()}`}>{channel.toUpperCase()}</span>
                            <span className="system-log-source">{source}</span>
                          </div>
                          <span className="system-log-target" title={`${tr("systemLogs.taskLabel")}: ${log.task_title} · ${tr("systemLogs.runLabel")}: ${log.run_id}`}>
                            {log.task_title || tr("common.unknown")} · {log.run_id ? log.run_id.slice(0, 8) : tr("common.none")}
                          </span>
                        </div>
                        <pre className="system-log-text" title={log.payload}>
                          {formatSystemLogPayload(log.payload)}
                        </pre>
                      </article>
                    );
                  })}
                </div>
              )}
            </div>
          </section>
        </main>
      ) : (
        <main className="home-page home-dashboard" id="main-content">
          <header className="home-topbar">
            <div className="home-topbar-brand">
              <div className="home-topbar-brand-mark" aria-hidden="true">
                <LogoIcon className="home-topbar-brand-icon" />
              </div>
              <strong>{tr("home.appTitle")}</strong>
            </div>

            <div className="home-topbar-divider" aria-hidden="true" />

            <div className="home-topbar-project">
              <label className="sr-only" htmlFor="project-select">
                {tr("home.currentProject")}
              </label>
              <div className="home-topbar-project-select">
                <FolderIcon className="home-topbar-inline-icon" />
                <select
                  id="project-select"
                  className="home-topbar-select"
                  value={selectedProjectID}
                  onChange={(e) => onChangeProject(e.target.value)}
                  aria-label={tr("home.currentProject")}
                >
                  <option value="">{tr("home.selectProject")}</option>
                  {projects.map((project) => (
                    <option key={project.ID} value={project.ID}>
                      {project.Name}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="home-topbar-side">
              <div className="status-pill home-topbar-pill" title={healthText} aria-label={`${tr("common.systemRunning")} · ${healthText}`}>
                <span className="status-dot" />
                <strong>{tr("common.systemRunning")}</strong>
              </div>
              <div
                className={`status-pill status-pill-mcp status-pill-${mcpStatus.state} home-topbar-pill`}
                title={mcpStatus.message || mcpStateText[mcpStatus.state]}
                aria-label={`${tr("detail.mcpStatusLabel")} ${mcpStateText[mcpStatus.state]}`}
              >
                <span className="status-dot" />
                <strong>{mcpStateText[mcpStatus.state]}</strong>
              </div>
              <label className="sr-only" htmlFor="language-select-home-compact">
                {tr("language.label")}
              </label>
              <div className="home-topbar-language">
                <GlobeIcon className="home-topbar-inline-icon" />
                <select
                  id="language-select-home-compact"
                  className="home-topbar-select"
                  value={currentLanguage}
                  onChange={(e) => onChangeLanguage(e.target.value)}
                  aria-label={tr("language.label")}
                >
                  <option value="zh-CN">{tr("language.zhCN")}</option>
                  <option value="en-US">{tr("language.enUS")}</option>
                </select>
              </div>
              <div className="home-topbar-actions">
                <button type="button" className="home-primary-action" onClick={openProjectModal}>
                  <PlusIcon className="home-topbar-button-icon" />
                  <span>{tr("home.newProject")}</span>
                </button>
                <button
                  type="button"
                  className="home-secondary-action"
                  onClick={openTaskModal}
                  disabled={!selectedProjectID}
                >
                  <TaskIcon className="home-topbar-button-icon" />
                  <span>{tr("home.newTask")}</span>
                </button>
                <button
                  type="button"
                  className="home-topbar-icon-button"
                  onClick={() => void refreshAll()}
                  disabled={loading}
                  title={loading ? tr("common.refreshing") : tr("common.refreshData")}
                  aria-label={loading ? tr("common.refreshing") : tr("common.refreshData")}
                >
                  <RefreshIcon className="home-topbar-button-icon" />
                </button>
              </div>
              <button
                type="button"
                className="home-topbar-icon-button"
                title={tr("systemLogs.navButton")}
                aria-label={tr("systemLogs.navButton")}
                onClick={() => setPage("systemLogs")}
              >
                <PanelIcon className="home-topbar-button-icon" />
              </button>
              <button
                type="button"
                className="home-topbar-icon-button"
                title={tr("common.settings")}
                aria-label={tr("common.settings")}
                onClick={() => {
                  setPage("settings");
                  void refreshGlobalSettings();
                }}
              >
                <SettingsIcon className="home-topbar-button-icon" />
              </button>
            </div>
          </header>

          <section className="dispatch-console" aria-label={tr("home.runControlTitle")}>
            <div className="dispatch-console-main">
              <div className="dispatch-console-block">
                <span className="dispatch-console-label">{tr("home.controlMode")}</span>
                <button
                  type="button"
                  className={`dispatch-toggle ${autoRunEnabled ? "dispatch-toggle-enabled" : ""}`}
                  onClick={onToggleAutoRun}
                  disabled={!selectedProjectID}
                  aria-pressed={autoRunEnabled}
                >
                  <span className="dispatch-toggle-track" aria-hidden="true">
                    <span className="dispatch-toggle-thumb" />
                  </span>
                  <span>{dispatchModeText}</span>
                </button>
              </div>

              <div className="dispatch-console-divider" aria-hidden="true" />

              <div className="dispatch-console-block dispatch-console-status">
                <span className="dispatch-console-label">{tr("home.lastStatus")}</span>
                <p className="dispatch-status-text">
                  <CheckCircleIcon className="dispatch-status-icon" />
                  <span>{dispatchStatusText}</span>
                </p>
              </div>
            </div>

            <div className="dispatch-console-actions">
              <button type="button" className="dispatch-console-button" onClick={onDispatch} disabled={!selectedProjectID}>
                <PlayIcon className="home-topbar-button-icon" />
                <span>{tr("home.dispatchOnce")}</span>
              </button>
            </div>
          </section>

          <section className="panel dashboard-task-panel">
            <div className="dashboard-section-header">
              <div className="dashboard-section-copy">
                <span className="dashboard-section-kicker">{selectedProjectName}</span>
                <h2>{tr("home.taskListTitle", { count: tasks.length })}</h2>
                <p>{selectedProjectPath}</p>
              </div>
              <div className="actions">
                <button type="button" className="btn-secondary" onClick={() => void refreshAll()} disabled={loading}>
                  {loading ? tr("common.refreshing") : tr("common.refreshData")}
                </button>
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() => setPage("project")}
                  disabled={!selectedProjectID}
                >
                  {tr("home.projectDetail")}
                </button>
              </div>
            </div>
            {loading ? (
              <p className="empty empty-state">{tr("home.loadingTasks")}</p>
            ) : tasks.length === 0 ? (
              <p className="empty empty-state">{tr("home.noTasks")}</p>
            ) : (
              <div className="task-list dashboard-task-list">
                {orderedTasks.map((task) => (
                  <article
                    className={`task-card ${task.Status === "running" || runningTaskIDs.has(task.ID) ? "task-card-running" : ""}`}
                    key={task.ID}
                  >
                    <div className="task-card-header">
                      <div className="task-card-title-wrap">
                        <h3>{task.Title}</h3>
                        <p className="task-desc">{task.Description}</p>
                      </div>
                      <span className={`status status-${task.Status}`} aria-label={`${tr("detail.status")} ${statusText[task.Status] || task.Status}`}>
                        {statusText[task.Status] || task.Status}
                      </span>
                    </div>
                    <dl className="task-meta" aria-label={tr("home.taskMetadata")}>
                      <div className="task-meta-item">
                        <dt>{tr("detail.priority")}</dt>
                        <dd>
                          <span className="badge badge-priority">{tr("home.priorityBadge", { priority: task.Priority })}</span>
                        </dd>
                      </div>
                      <div className="task-meta-item">
                        <dt>{tr("detail.provider")}</dt>
                        <dd>
                          <span className="badge badge-provider">{task.Provider || tr("common.unassigned")}</span>
                        </dd>
                      </div>
                      <div className="task-meta-item">
                        <dt>{tr("detail.updatedAt")}</dt>
                        <dd>{formatDateTime(task.UpdatedAt)}</dd>
                      </div>
                      {task.Status === "failed" && (
                        <>
                          <div className="task-meta-item">
                            <dt>{tr("detail.executionCount")}</dt>
                            <dd>{task.RetryCount ?? 0}</dd>
                          </div>
                          <div className="task-meta-item">
                            <dt>{tr("detail.nextRunAt")}</dt>
                            <dd>{formatDateTime(task.NextRetryAt)}</dd>
                          </div>
                        </>
                      )}
                    </dl>
                    <div className="task-card-actions">
                      <button type="button" className="btn-secondary" onClick={() => void onOpenTaskDetail(task)}>
                        {tr("home.openTaskDetail")}
                      </button>
                      {task.Status !== "running" && (
                        <button
                          type="button"
                          className="btn-secondary"
                          onClick={() => openEditTaskModal(task)}
                        >
                          {tr("home.editTask")}
                        </button>
                      )}
                      {task.Status === "running" && (
                        <button
                          type="button"
                          className="btn-live"
                          onClick={() => void onOpenTaskDetail(task)}
                        >
                          {tr("home.viewLiveOutput")}
                        </button>
                      )}
                      {(task.Status === "failed" || task.Status === "blocked") && (
                        <>
                          <button
                            type="button"
                            className="btn-live"
                            onClick={() => void onOpenTaskDetail(task)}
                          >
                            {tr("home.viewFailedLogs")}
                          </button>
                          <button
                            type="button"
                            onClick={() => void onRetryTask(task.ID)}
                          >
                            {tr("home.retryTask")}
                          </button>
                        </>
                      )}
                      {task.Status !== "running" && (
                        <button
                          type="button"
                          className="btn-danger"
                          onClick={() => void onDeleteTask(task)}
                        >
                          {tr("home.deleteTask")}
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => void onMarkDone(task.ID)}
                      >
                        {tr("home.markDone")}
                      </button>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </section>

          {showProjectModal && (
            <div className="modal-mask" onClick={closeProjectModal}>
              <div
                className="modal"
                onClick={(e) => e.stopPropagation()}
                role="dialog"
                aria-modal="true"
                aria-labelledby="project-modal-title"
              >
                <h3 id="project-modal-title">{tr("modal.newProjectTitle")}</h3>
                <form className="form" onSubmit={onCreateProject}>
                  {projectFormError && (
                    <p className="field-error" role="alert">
                      {projectFormError}
                    </p>
                  )}
                  <label htmlFor="project-name">{tr("modal.projectName")}</label>
                  <input
                    id="project-name"
                    ref={projectNameInputRef}
                    value={projectName}
                    onChange={(e) => {
                      setProjectName(e.target.value);
                      setProjectFormError("");
                    }}
                    placeholder={tr("modal.projectNamePlaceholder")}
                    aria-invalid={projectSubmitAttempted && !projectName.trim()}
                  />
                  <label htmlFor="project-path">{tr("modal.projectPath")}</label>
                  <input
                    id="project-path"
                    value={projectPath}
                    onChange={(e) => {
                      setProjectPath(e.target.value);
                      setProjectFormError("");
                    }}
                    placeholder={tr("modal.projectPathPlaceholder")}
                    aria-invalid={projectSubmitAttempted && !projectPath.trim()}
                  />
                  <label htmlFor="project-provider">{tr("home.defaultProvider")}</label>
                  <select
                    id="project-provider"
                    value={newProjectProvider}
                    onChange={(e) => {
                      setNewProjectProvider(e.target.value);
                      setProjectFormError("");
                    }}
                  >
                    <option value="claude">claude</option>
                    <option value="codex">codex</option>
                  </select>
                  <label htmlFor="project-failure-policy">{tr("project.failurePolicy")}</label>
                  <select
                    id="project-failure-policy"
                    value={newProjectFailurePolicy}
                    onChange={(e) => {
                      setNewProjectFailurePolicy(e.target.value);
                      setProjectFormError("");
                    }}
                  >
                    <option value="block">{tr("project.failurePolicyBlock")}</option>
                    <option value="continue">{tr("project.failurePolicyContinue")}</option>
                  </select>
                  <p className="run-info">
                    {newProjectFailurePolicy === "continue"
                      ? tr("project.failurePolicyContinueHint")
                      : tr("project.failurePolicyBlockHint")}
                  </p>
                  <label htmlFor="project-model-create">{tr("home.modelOptional")}</label>
                  <input
                    id="project-model-create"
                    value={newProjectModel}
                    onChange={(e) => {
                      setNewProjectModel(e.target.value);
                      setProjectFormError("");
                    }}
                    placeholder={tr("home.modelPlaceholder")}
                  />
                  <div className="modal-actions">
                    <button type="button" className="btn-ghost" onClick={closeProjectModal}>
                      {tr("common.cancel")}
                    </button>
                    <button type="submit">{tr("common.create")}</button>
                  </div>
                </form>
              </div>
            </div>
          )}

          {showTaskModal && (
            <div className="modal-mask" onClick={closeTaskModal}>
              <div
                className="modal"
                onClick={(e) => e.stopPropagation()}
                role="dialog"
                aria-modal="true"
                aria-labelledby="task-modal-title"
              >
                <h3 id="task-modal-title">{editingTaskID ? tr("modal.editTaskTitle") : tr("modal.newTaskTitle")}</h3>
                <form className="form" onSubmit={onCreateTask}>
                  {taskFormError && (
                    <p className="field-error" role="alert">
                      {taskFormError}
                    </p>
                  )}
                  <label htmlFor="task-title">{tr("modal.taskTitle")}</label>
                  <input
                    id="task-title"
                    ref={taskTitleInputRef}
                    value={taskTitle}
                    onChange={(e) => {
                      setTaskTitle(e.target.value);
                      setTaskFormError("");
                    }}
                    placeholder={tr("modal.taskTitlePlaceholder")}
                    aria-invalid={taskSubmitAttempted && !taskTitle.trim()}
                  />
                  <label htmlFor="task-desc">{tr("modal.taskDescription")}</label>
                  <textarea
                    id="task-desc"
                    value={taskDesc}
                    onChange={(e) => {
                      setTaskDesc(e.target.value);
                      setTaskFormError("");
                    }}
                    placeholder={tr("modal.taskDescriptionPlaceholder")}
                    aria-invalid={taskSubmitAttempted && !taskDesc.trim()}
                  />
                  <label htmlFor="task-priority">{tr("modal.taskPriority")}</label>
                  <input
                    id="task-priority"
                    type="number"
                    min={1}
                    step={1}
                    value={taskPriority}
                    onChange={(e) => {
                      setTaskPriority(e.target.value);
                      setTaskFormError("");
                    }}
                    placeholder={tr("modal.taskPriorityPlaceholder")}
                    aria-invalid={taskSubmitAttempted && taskPriority.trim() !== "" && (!Number.isInteger(Number(taskPriority)) || Number(taskPriority) <= 0)}
                  />
                  <div className="modal-actions">
                    <button type="button" className="btn-ghost" onClick={closeTaskModal}>
                      {tr("common.cancel")}
                    </button>
                    <button type="submit">{editingTaskID ? tr("common.save") : tr("common.create")}</button>
                  </div>
                </form>
              </div>
            </div>
          )}
        </main>
      )}
      {pendingDeleteProject && (
        <div className="modal-mask" onClick={closeDeleteProjectConfirm}>
          <div
            className="modal modal-confirm"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-project-confirm-title"
          >
            <h3 id="delete-project-confirm-title">{tr("project.deleteProject")}</h3>
            <p>{tr("project.confirmDelete", { name: pendingDeleteProject.projectName })}</p>
            <div className="modal-actions">
              <button type="button" className="btn-ghost" onClick={closeDeleteProjectConfirm}>
                {tr("common.cancel")}
              </button>
              <button type="button" className="btn-danger" onClick={() => void onDeleteProjectConfirmed()}>
                {tr("project.deleteProject")}
              </button>
            </div>
          </div>
        </div>
      )}

      {pendingDeleteTask && (
        <div className="modal-mask" onClick={closeDeleteTaskConfirm}>
          <div
            className="modal modal-confirm"
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-task-confirm-title"
          >
            <h3 id="delete-task-confirm-title">{tr("home.deleteTask")}</h3>
            <p>{tr("home.confirmDeleteTask", { name: pendingDeleteTask.taskName })}</p>
            <div className="modal-actions">
              <button type="button" className="btn-ghost" onClick={closeDeleteTaskConfirm}>
                {tr("common.cancel")}
              </button>
              <button type="button" className="btn-danger" onClick={() => void onDeleteTaskConfirmed()}>
                {tr("home.deleteTask")}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default App;


