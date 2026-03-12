# auto-work

[English](./README.md) | 简体中文

`auto-work` 是一个本地优先的 AI 任务编排桌面应用，帮助你把“给 Claude / Codex 派活、看执行过程、收结果、补后续任务”这套流程统一到一个工作台里。

它基于 `Go + Wails + React + SQLite` 构建，适合管理多个代码仓库中的开发任务：你可以为每个项目维护任务队列，选择默认 Provider，控制自动派发策略，并通过内置 MCP 回调让 AI 在任务执行完成后结构化回写结果、创建后续任务或更新任务状态。

## 适合什么场景

- 同时维护多个代码仓库，希望统一管理 AI 开发任务
- 希望把任务从“聊天式交互”切换成“队列式执行”
- 希望任务完成后自动回写状态、保留运行日志和历史记录
- 希望通过 Telegram 在移动端查看任务、创建任务、接收完成通知
- 希望在本地运行，不依赖云端任务中心

## 核心能力

- 项目级任务看板：按项目管理任务、优先级、状态和运行历史
- 双 Provider 支持：支持 `Claude` 和 `Codex`，可按项目设置默认执行器
- 自动派发：空闲 agent 自动领取任务，也支持手动“派发一次”
- 内置 MCP Server：同进程提供 `report_result/create_tasks/update_task/delete_task` 等工具
- 结果可追踪：保留任务摘要、详细结果、运行事件和日志
- Telegram 集成：支持通知、查询、创建任务、查看日志摘要
- 项目级 AI 配置：支持项目默认模型、项目提示词、失败策略、截图上报
- 本地持久化：数据保存在 SQLite，配置简单，迁移成本低

## 文档语言

- English: [README.md](./README.md)
- 简体中文: [README-zh.md](./README-zh.md)

## 界面预览

### 任务列表

![任务列表](docs/任务列表.png)

主界面按项目展示任务队列、运行控制和任务卡片，适合日常批量管理与派发。

### 任务详情

![任务详情](docs/任务详情.png)

任务详情页可以查看最近运行记录、结果摘要、详细说明，以及实时/历史日志。

### 项目详情

![项目详情](docs/项目详情.png)

项目维度可以配置默认 Provider、模型、失败处理策略、项目提示词，以及前端截图上报能力。

### 全局配置

![全局配置](docs/全局配置.png)

全局设置页集中管理 Telegram Bot、系统通知和全局系统提示词。

### Telegram 指令

![Telegram 指令](docs/telegram指令.png)

启用 Telegram 后，可以直接在聊天窗口查询项目、创建任务、查看待办和运行状态。

### Telegram 任务通知

![Telegram 任务通知](docs/telegram任务通知.png)

任务开始、完成、失败或阻塞后，可以把结果摘要和截图地址推送到 Telegram。

## 技术栈

- 后端：Go 1.23、Wails v2、SQLite
- 前端：React 18、TypeScript、Vite
- 集成：Claude CLI、Codex CLI、Telegram Bot、MCP HTTP Server

## 快速开始

### 1. 准备依赖

请先确保本机已安装：

- Go `1.23+`
- Node.js 与 npm
- Wails CLI
- 可选：`claude` CLI、`codex` CLI（需要真正执行 AI 任务时）

安装 Wails CLI：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

安装前端依赖：

```bash
make frontend-install
```

### 2. 启动开发模式

```bash
make dev
```

如果只想运行 Go 测试：

```bash
make test
```

如果要打包桌面应用：

```bash
make build
```

### 3. 首次使用流程

1. 启动应用后先新建项目，填写项目名称和本地仓库路径
2. 进入项目详情，设置默认 Provider、模型和失败策略
3. 回到首页创建任务
4. 选择手动“派发一次”，或开启“自动派发”
5. 在任务详情页查看执行日志、结果摘要和历史记录
6. 如需移动端通知，再去全局设置中配置 Telegram

## 使用说明

### 项目与任务管理

- 项目是任务的顶层容器，每个项目绑定一个本地仓库路径
- 任务默认追加到项目队尾，也可以通过 MCP 在指定任务后插入
- 支持任务编辑、删除、重试、手动标记完成
- 任务状态覆盖 `pending / running / done / failed / blocked`

### Provider 与调度

- 项目可以选择默认 Provider：`claude` 或 `codex`
- 自动派发开启后，系统会持续检查当前项目并调度待执行任务
- 手动派发适合临时执行单个任务或调试运行环境
- 失败策略支持：
  - `block`：出现失败任务后阻塞该项目后续调度
  - `continue`：失败任务不阻塞其他待执行任务

### 运行结果与回写

- 每次运行都会创建 run 记录，并保留事件流和关键日志
- AI 完成任务后，会通过内置 MCP 工具 `auto-work.report_result` 回写最终状态
- AI 还可以通过 MCP 继续创建后续任务、更新任务内容或删除无效任务

### Telegram 集成

- 可接收任务开始、完成、失败、阻塞通知
- 可通过 Telegram 直接查询项目、待办、最近任务和运行日志
- 可通过 `/addtask` 在聊天中创建任务

详细配置见：

- [Telegram Bot 配置指南](docs/04-telegram-bot-setup.md)
- [Telegram 指令手册](docs/05-telegram-commands.md)

### 前端截图上报

- 可在项目详情页开启“任务完成后开启截图上报”
- 当任务涉及前端改动时，AI 会在结果详情中补充截图地址
- Telegram 通知会优先发送截图地址，本地图片还会尝试作为图片消息发送

## 常用启动方式

### 默认启动

```bash
make dev
```

### 指定数据库路径

```bash
AUTO_WORK_DB_PATH=/abs/path/auto-work.db make dev
```

### 关闭默认自动执行

```bash
AUTO_WORK_RUN_CLAUDE_ON_DISPATCH=0 make dev
AUTO_WORK_RUN_CODEX_ON_DISPATCH=0 make dev
```

### 指定默认模型

```bash
AUTO_WORK_CLAUDE_MODEL=claude-sonnet-4-6 \
AUTO_WORK_CODEX_MODEL=gpt-5.3-codex \
make dev
```

### 开启并强制要求 MCP 回调

```bash
AUTO_WORK_ENABLE_MCP_CALLBACK=1 \
AUTO_WORK_REQUIRE_MCP_CALLBACK=1 \
make dev
```

### 指定 MCP HTTP 地址

```bash
AUTO_WORK_MCP_HTTP_URL=http://127.0.0.1:39123/mcp make dev
```

说明：

- 数据库默认路径为 `./data/auto-work.db`
- 应用日志默认写入 `./data/log/auto-work.log`
- 环境变量主要用于启动默认值，运行后也可以在界面中修改部分配置

## MCP 能力

当前内置 MCP Server 由应用同进程启动，默认提供以下工具：

- `auto-work.report_result`
- `auto-work.create_tasks`
- `auto-work.update_task`
- `auto-work.delete_task`
- `auto-work.list_pending_tasks`
- `auto-work.list_history_tasks`
- `auto-work.get_task_detail`

如果你希望在终端里直接运行 Claude Code / Codex 时也能访问 `auto-work` MCP，可参考：

- [MCP 配置与排障](docs/06-mcp-config.md)

## 仓库文档

- [English README](./README.md)
- [中文 README](./README-zh.md)
- [架构设计](docs/01-architecture.md)
- [开发计划](docs/02-dev-plan.md)
- [MVP 验收](docs/03-mvp-acceptance.md)
- [Telegram Bot 配置指南](docs/04-telegram-bot-setup.md)
- [Telegram 指令手册](docs/05-telegram-commands.md)
- [MCP 配置与排障](docs/06-mcp-config.md)

## 开发命令

```bash
make help
make tidy
make fmt
make test
make frontend-install
make frontend-build
make dev
make build
```

## License

如需开源发布，建议补充 `LICENSE` 文件后再发布到 GitHub。
