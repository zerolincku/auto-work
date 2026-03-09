# auto-work

本项目是一个基于 `Go + Wails + SQLite` 的本地 AI 任务编排工具。

V1 目标：
- 任务管理（创建、查看、状态更新）
- 调度派发（空闲 agent 领取下一个任务）
- provider：`Claude` / `Codex`
- 任务完成通过 MCP 回调 `auto-work.report_result` 回写状态，且支持通过 MCP 创建、修改、删除任务

## 当前已实现

- SQLite 基础设施
  - 连接初始化（WAL、foreign keys）
  - 迁移系统（`schema_migrations` + 版本 SQL）
  - 核心表：`tasks/agents/runs/run_events/artifacts`

- 后端服务层
  - Project Service：项目创建与查询（项目名 + 项目路径）
  - Task Service：创建、列表、状态更新（按项目隔离）
  - Dispatcher：原子领取任务、运行完成回写
  - MCP 服务（同进程 HTTP）：`auto-work.report_result`、`auto-work.create_tasks`（支持批量与按任务后插入）、`auto-work.update_task`、`auto-work.delete_task`、`auto-work.list_pending_tasks`、`auto-work.list_history_tasks`、`auto-work.get_task_detail`
  - App Facade：供 Wails 直接绑定的方法
  - Claude Runner：在任务所属项目路径中执行
  - 运行态查询：`ListRunningRuns`、`ListRunLogs`（实时日志轮询）
  - 失败重试：`RetryTask`（failed/blocked -> pending）
  - 失败原因：写入 `runs.result_summary/result_details` 与 `run_events(system.failure)`

- Wails 桌面壳
  - 初始化 React-TS 前端
  - 最小操作 UI（Create Task / Dispatch Once / Finish Run / Task List）

## 运行

### 1) 安装前端依赖
```bash
cd frontend
npm install
cd ..
```

### 2) 开发模式
```bash
wails dev
```
如果未安装：
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 3) 仅运行 Go 测试
```bash
make test
```

### 4) 后端数据库路径
默认：`./data/auto-work.db`

可通过环境变量覆盖：
```bash
AUTO_WORK_DB_PATH=/abs/path/auto-work.db wails dev
```

自动调用 Claude CLI（默认开启）：
```bash
wails dev
```

如需关闭默认自动派发：
```bash
AUTO_WORK_RUN_CLAUDE_ON_DISPATCH=0 wails dev
```

Codex 执行开关（默认开启）：
```bash
AUTO_WORK_RUN_CODEX_ON_DISPATCH=0 wails dev
```

可选模型环境变量（也可在项目配置里设置）：
```bash
AUTO_WORK_CLAUDE_MODEL=claude-sonnet-4-6
AUTO_WORK_CODEX_MODEL=gpt-5.3-codex
```

说明：
- 这个环境变量仅用于启动默认值。
- 运行后可在页面点击“启用自动派发 / 停用自动派发”按钮实时控制。
- 当自动执行未开启时，系统会阻止“派发一次”，避免出现“任务显示执行中但没有实时日志”的假运行状态。

MCP 回调相关（默认开启）：
```bash
AUTO_WORK_ENABLE_MCP_CALLBACK=1 \
AUTO_WORK_REQUIRE_MCP_CALLBACK=1 \
AUTO_WORK_RUN_CLAUDE_ON_DISPATCH=1 \
wails dev
```

MCP HTTP 地址：
```bash
AUTO_WORK_MCP_HTTP_URL=http://127.0.0.1:39123/mcp
```

说明：
- MCP HTTP Server 由后端同进程自动启动。
- Runner 会按每次 run 自动注入 `run_id/task_id` 到 URL query。
- 默认固定端口为 `39123`；如需改端口，可设定：`AUTO_WORK_MCP_HTTP_URL=http://127.0.0.1:47081/mcp`。
- 如需把 MCP 全局安装到 `Claude Code / Codex`，见 [`docs/06-mcp-config.md`](docs/06-mcp-config.md) 第 3 节。

## 主要后端绑定方法

- `Health()`
- `CreateProject(req)`
- `ListProjects(limit)`
- `CreateTask(req)`
- `ListTasks(status, provider, projectID, limit)`
- `UpdateTaskStatus(taskID, status)`
- `ListAgents()`
- `DispatchOnce(agentID, projectID)`
- `FinishRun(req)`

## 下一步

- 完善 `ClaudeRunner` 输出事件解析（提取更细粒度阶段）
- 补充 Run Monitor 和日志详情界面

## 文档

- 架构设计：[`docs/01-architecture.md`](docs/01-architecture.md)
- 开发计划：[`docs/02-dev-plan.md`](docs/02-dev-plan.md)
- MVP 验收：[`docs/03-mvp-acceptance.md`](docs/03-mvp-acceptance.md)
- Telegram 配置与 Chat ID 获取：[`docs/04-telegram-bot-setup.md`](docs/04-telegram-bot-setup.md)
- Telegram 指令手册：[`docs/05-telegram-commands.md`](docs/05-telegram-commands.md)
- MCP 配置与排障：[`docs/06-mcp-config.md`](docs/06-mcp-config.md)
