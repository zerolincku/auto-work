# Auto Work 架构设计（当前实现）

## 1. 目标与范围

### 1.1 V1 目标
- 使用 `Go + Wails` 构建本地桌面应用。
- 实现 TODO 管理（创建、排队、执行、追踪、归档）。
- 接入 `Claude Code` 与 `Codex` 两种 provider，实现自动派发与结果回传。
- 通过内置 MCP Server（同进程 HTTP）实现 AI 结构化回写与任务查询/创建能力。

### 1.2 非目标（V1 不做）
- 多机分布式调度。
- 云端托管与多租户。
- 自动代码审查策略引擎（仅保留日志与结果）。

## 2. 总体架构

```text
Wails Desktop App
├─ Frontend (React/Vue)
│  ├─ Task Board
│  ├─ Run Monitor
│  └─ Settings (Claude CLI/MCP/安全策略)
└─ Go Backend
   ├─ Task Service
   ├─ Scheduler
   ├─ Runner Manager
   │  ├─ Claude Runner
   │  └─ Codex Runner
   ├─ MCP Server (HTTP: report_result/create_tasks/list_pending/list_history/get_task_detail)
   ├─ Event Bus
   └─ SQLite (tasks/runs/agents/events/artifacts)
```

## 3. 核心模块设计

### 3.1 Task Service
- 职责：任务 CRUD、优先级、依赖、状态迁移校验。
- 输入：UI 请求、Scheduler 派发请求、MCP 回调。
- 输出：任务状态、可执行任务列表。

### 3.2 Scheduler
- 触发：每 30-60 秒 tick。
- 策略：
- 每个 agent 并发度默认 1。
- 若存在 `running` 且 heartbeat 未超时，跳过该 agent。
- 若空闲，按规则选取下一个可执行任务并 claim。
- 规则（建议顺序）：`priority DESC -> created_at ASC`，并过滤依赖未完成任务。

### 3.3 Runner Manager
- 职责：创建 run、拉起 CLI 子进程、维护 pid 与 heartbeat、收集 stdout/stderr。
- Provider：
- `ClaudeRunner`：`claude -p ... --output-format stream-json`。
- `CodexRunner`：`codex exec --json ...`，并在启动参数注入 MCP URL。

### 3.4 MCP Server
- Transport：同进程 `HTTP`（默认）。
- 工具：
- `auto-work.report_result`：完成态上报。
- `auto-work.create_tasks`：批量创建后续任务（当前项目队尾追加）。
- `auto-work.list_pending_tasks`：查询当前项目 `pending` 任务。
- `auto-work.list_history_tasks`：查询当前项目历史任务（`done/failed/blocked`）。
- `auto-work.get_task_detail`：查询任务详情（含最近运行记录）。
- 安全：
- 通过 `run_id/task_id` 作用域绑定运行上下文（query/header 注入）。
- 写操作要求 run 处于 `running` 且 run/task 关系匹配。
- `report_result` 支持 `idempotency_key` 去重。

### 3.5 Event Bus
- 统一内部事件：
- `task.created`、`run.started`、`run.heartbeat`、`run.finished`、`run.failed`、`mcp.*`（如 `mcp.report_result.applied`）。
- 用途：UI 实时刷新、审计、故障排查。

## 4. 数据模型（SQLite）

```sql
-- tasks
id TEXT PRIMARY KEY
title TEXT NOT NULL
description TEXT NOT NULL
priority INTEGER NOT NULL DEFAULT 100
status TEXT NOT NULL -- pending/running/done/failed/blocked
depends_on TEXT NOT NULL DEFAULT '[]' -- JSON array
provider TEXT NOT NULL DEFAULT 'claude' -- claude/codex
created_at DATETIME NOT NULL
updated_at DATETIME NOT NULL

-- agents
id TEXT PRIMARY KEY
name TEXT NOT NULL
provider TEXT NOT NULL -- claude/codex
enabled INTEGER NOT NULL DEFAULT 1
concurrency INTEGER NOT NULL DEFAULT 1
last_seen_at DATETIME
created_at DATETIME NOT NULL
updated_at DATETIME NOT NULL

-- runs
id TEXT PRIMARY KEY
task_id TEXT NOT NULL
agent_id TEXT NOT NULL
attempt INTEGER NOT NULL DEFAULT 1
status TEXT NOT NULL -- running/done/failed/needs_input/cancelled
pid INTEGER
heartbeat_at DATETIME
started_at DATETIME NOT NULL
finished_at DATETIME
exit_code INTEGER
provider_session_id TEXT
prompt_snapshot TEXT NOT NULL
result_summary TEXT
result_details TEXT
idempotency_key TEXT
created_at DATETIME NOT NULL
updated_at DATETIME NOT NULL

-- run_events
id TEXT PRIMARY KEY
run_id TEXT NOT NULL
ts DATETIME NOT NULL
kind TEXT NOT NULL
payload TEXT NOT NULL -- JSON

-- artifacts
id TEXT PRIMARY KEY
run_id TEXT NOT NULL
kind TEXT NOT NULL -- file/url/log
value TEXT NOT NULL
created_at DATETIME NOT NULL
```

约束建议：
- `runs(task_id, status='running')` 逻辑唯一（可通过事务保证）。
- `runs(agent_id, status='running')` 逻辑唯一（并发=1）。
- MCP 上报按 `(run_id, idempotency_key)` 幂等去重。

## 5. 状态机

### 5.1 Task 状态
- `pending -> running -> done`
- `pending -> running -> failed`
- `pending -> running -> blocked`
- `failed/blocked -> pending`（人工重试）

### 5.2 Run 状态
- `running -> done`
- `running -> failed`
- `running -> needs_input`
- `running -> cancelled`

### 5.3 超时规则
- `heartbeat_timeout`: 默认 10 分钟。
- 连续超时：标记 `failed` 并写入原因 `heartbeat_expired`。

## 6. Runner 设计（Claude + Codex）

### 6.1 命令模板
```bash
claude -p "<TASK_PROMPT>" \
  --verbose \
  --output-format stream-json \
  --debug-file "<debug_log_path>" \
  --allowedTools "Read,Edit,Bash" \
  --permission-mode acceptEdits
```

```bash
codex exec --json \
  --skip-git-repo-check \
  --dangerously-bypass-approvals-and-sandbox \
  -C "<project_path>" \
  "<TASK_PROMPT>"
```

说明：
- 两类 Runner 都支持按任务覆盖 model（任务未设置则回落全局默认）。
- 两类 Runner 都会在每次 run 启动时注入 run 级 MCP 回调地址。
- `allowedTools` 与权限模式主要用于 Claude，可在设置中调整。

### 6.2 Prompt 契约（硬约束）
- 每次任务都注入统一 System 指令片段：
- 必须先更新 `auto_work_current_task.md` 为 `running`，并在结束时写入 `success/failed`。
- 必须先检查仓库现状，若已满足则标记 `already_done`，不重复改动。
- 必须在结束时调用 `auto-work.report_result`。
- 失败也必须上报（status=failed + reason）。
- 不得提交 `auto_work_current_task.md`、`current_task.md`、`curret_task.md` 以及 Playwright 生成的截图、trace、日志、临时目录等产物。
- 只允许在任务目录内操作。
- 输出要包含验证步骤。

### 6.3 事件处理
- 读取 `stream-json` 行事件并落库到 `run_events`。
- 事件驱动刷新 `heartbeat_at`。
- 进程退出但未收到必需 MCP 回调时，系统会记录失败原因，并在可判定场景执行兜底收敛。

## 7. MCP 工具契约（当前）

### 7.1 `auto-work.report_result`
请求体：
```json
{
  "status": "success|failed|blocked",
  "summary": "简短总结",
  "details": "详细说明（可选）",
  "exit_code": 0,
  "idempotency_key": "uuid"
}
```

返回：
```json
{
  "content": [
    {"type": "text", "text": "ok|already-finished"}
  ]
}
```

校验：
- `run_id/task_id` 由运行时注入的 MCP URL 作用域确定，不在参数体里传。
- 非运行态上报返回 `already-finished`（不会重复改写终态）。
- 重复 `idempotency_key` 返回已处理结果，不重复写入。

### 7.2 `auto-work.create_tasks`
请求体：
```json
{
  "items": [
    {
      "title": "后续任务标题",
      "description": "后续任务描述",
      "depends_on": ["task_id_a"],
      "provider": "claude|codex"
    }
  ]
}
```

说明：
- `items` 范围：1~50。
- 新任务归属当前项目，优先级自动追加到队尾。
- `provider` 字段当前保留兼容，创建时不会直接指定执行 provider（由调度时策略决定）。

### 7.3 `auto-work.list_pending_tasks` / `auto-work.list_history_tasks`
请求体（可选）：
```json
{"limit": 20}
```

说明：
- 默认 `20`，最大 `100`。
- `list_pending_tasks` 返回当前项目 `pending`。
- `list_history_tasks` 返回当前项目 `done/failed/blocked`。

### 7.4 `auto-work.get_task_detail`
请求体：
```json
{"task_id": "task_xxx"}
```

说明：
- 仅允许查询与当前 run 同项目的任务。
- 返回任务摘要与最近运行记录（最多 20 条 runs）。

## 8. 安全设计

- MCP 写操作强约束 `run_id/task_id` 与运行态，避免跨 run 误写。
- 所有运行事件与 MCP 回调事件写入 `run_events`，便于审计与排障。
- Runner 工作目录绑定到任务所属项目路径（`project_path`）。
- Telegram token 当前保存在本地 SQLite `global_settings`（明文），依赖本机访问控制；如需更高安全级别应迁移到系统密钥链。
- Codex 自动派发当前使用 `--dangerously-bypass-approvals-and-sandbox`，需在受信任项目中使用。

## 9. Runner 抽象扩展

定义统一接口：

```go
type ProviderRunner interface {
  Start(ctx context.Context, run Run, task Task, agent Agent) (pid int, err error)
  Stop(ctx context.Context, runID string) error
  Probe(ctx context.Context, runID string) (RunHealth, error)
}
```

当前已落地 `ClaudeRunner` 与 `CodexRunner`，可在该接口下继续扩展其他 provider。

## 10. 部署与运行模式

- 运行模式：本地单机。
- DB：`sqlite`（WAL 模式）。
- 调度器：进程内 goroutine + ticker。
- 应用生命周期：
- App 启动时恢复未完成 run（心跳检查后重建状态）。
- App 关闭前尝试优雅停止运行中的子进程（超时强杀可配置）。
