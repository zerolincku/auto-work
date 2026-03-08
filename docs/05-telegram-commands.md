# Telegram 指令手册

本文档说明 `auto-work` Telegram Bot 的可用指令、参数格式与常见用法。

## 前置条件

- 已完成 Bot 配置并启用（见 [`04-telegram-bot-setup.md`](./04-telegram-bot-setup.md)）。
- 当前会话 `chat_id` 在允许列表中（`允许的 Chat IDs`）。

## 指令总览

- 自动通知：任务启动、任务完成、任务失败/阻塞会主动推送到白名单 Chat。
- `/help`：查看帮助。
- `/addproject ...`：创建项目。
- `/newproject ...`：`/addproject` 的别名。
- `/setprovider ...`：修改项目默认 Provider。
- `/provider ...`：`/setprovider` 的别名。
- `/projects [n]`：查看项目列表与 AI 配置摘要，默认 8，最大 20。
- `/project <项目ID或项目名>`：查看单个项目详情、运行中/待执行/失败任务预览。
- `/recent [n]`：查看最近任务（执行中/已完成），默认 5，最大 20。
- `/pending [项目ID或项目名] [n]`：查看待执行任务队列，默认 5，最大 20。
- `/queue [项目ID或项目名] [n]`：同 `/pending`。
- `/failed [项目ID或项目名] [n]`：查看失败/阻塞任务，默认 5，最大 20。
- `/running [项目ID或项目名] [n]`：查看运行中的任务，默认 5，最大 20。
- `/dispatch <task_id>`：派发指定任务。
- `/task <task_id>`：查看任务详情与最新运行状态。
- `/status <task_id>`：同 `/task`。
- `/logs <run_id|task_id> [n]`：查看缩略日志，默认 20，最大 80。
- `/tasklogs <task_id> [n]`：查看任务最新一次运行日志，默认 20，最大 80。
- `/latestlogs <task_id> [n]`：同 `/tasklogs`。
- `/addtask ...`：创建任务（自动分配优先级到队尾）。
- `/newtask ...`：`/addtask` 的别名。

## 项目管理指令

### 1) 创建项目

```text
/addproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]
/newproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]
```

示例：

```text
/addproject 支付系统 | /srv/payments
/addproject 自动工作台 | /Users/me/work/auto-work | codex | continue
```

- 若不填 Provider，默认 `claude`。
- 若不填失败策略，默认 `block`。

### 2) 修改项目默认 Provider

```text
/setprovider <项目ID或项目名> | <claude|codex>
/provider <项目ID或项目名> | <claude|codex>
```

示例：

```text
/setprovider 支付系统 | codex
/provider proj-123 | claude
```

- 该命令只修改项目默认 Provider。
- 任务进入执行时会按项目默认 Provider 选执行器，不看任务里历史记录的 provider。

## 任务创建指令

### 1) 单项目场景（系统中仅 1 个项目）

```text
/addtask <标题> | <描述> [| claude|codex]
```

示例：

```text
/addtask 修复登录超时 | 排查 timeout 根因并补测试
/addtask 新增健康检查 | 增加 /healthz 与单测 | claude
```

### 2) 多项目场景（推荐显式指定项目）

```text
/addtask p:<项目ID或项目名> | <标题> | <描述> [| claude|codex]
```

示例：

```text
/addtask p:proj-123 | 优化重试策略 | 引入指数退避并补测试 | claude
/addtask p:支付系统 | 对账任务补偿 | 新增失败补偿任务 | codex
```

### 3) 规则说明

- 分隔符使用英文竖线 `|`。
- Provider 参数当前仅用于命令格式兼容；任务创建后 provider 字段保持未分配，实际执行时按项目 `默认 Provider` 决定（未配置则回落 `claude`）。
- 优先级不需要填写，系统会自动按当前项目队尾优先级递增分配。
- 标题最长 200 字符；描述最长 4000 字符。
- 多项目且未指定 `p:` 时，Bot 会返回可用项目列表并提示重试。

## 常见查询指令示例

```text
/addproject 支付系统 | /srv/payments | codex
/setprovider 支付系统 | claude
/projects
/project 支付系统
/dispatch 8f4f8c2a-xxxx-xxxx-xxxx-xxxxxxxxxxxx
/pending proj-123 10
/failed 支付系统 5
/running 5
/recent 5
/task 8f4f8c2a-xxxx-xxxx-xxxx-xxxxxxxxxxxx
/logs 3c2f1e5b-xxxx-xxxx-xxxx-xxxxxxxxxxxx 30
/tasklogs 8f4f8c2a-xxxx-xxxx-xxxx-xxxxxxxxxxxx 30
```

## 项目与队列指令说明

### 1) 项目概览

```text
/projects [n]
```

- 展示项目 ID、默认 Provider、Model、自动派发状态、失败策略和路径摘要。
- 适合先确认项目配置，再决定后续查询或下发 `/addtask`。

### 2) 单项目详情

```text
/project <项目ID或项目名>
```

- 展示项目基础配置。
- 同时预览该项目的运行中任务、待执行任务、失败/阻塞任务。

### 3) 队列/失败/运行中查询

```text
/pending [项目ID或项目名] [n]
/queue [项目ID或项目名] [n]
/failed [项目ID或项目名] [n]
/running [项目ID或项目名] [n]
```

- 不带项目参数时，返回全局视角。
- 带项目参数时，仅返回指定项目。
- `n` 为可选条数；若项目名包含空格，建议直接用项目 ID。

## 派发与日志指令

### 1) 派发指定任务

```text
/dispatch <task_id>
```

- 只会尝试派发该任务，不会改派其他 pending 任务。
- 派发时会按项目默认 Provider 选择执行器。
- 若依赖未完成、被失败策略阻塞，或对应 agent 正忙，会返回不可派发提示。

### 2) 查看任务最新一次运行日志

```text
/tasklogs <task_id> [n]
/latestlogs <task_id> [n]
```

- 只接受 `task_id`。
- 会自动定位该任务最新一次 run，再返回该 run 的日志摘要。

## 常见报错与处理

- `未授权的 Chat ID，已拒绝请求。`
  - 说明当前 chat_id 不在白名单，去“全局设置”补充并保存。

- `存在多个项目，请在命令里指定项目`
  - 使用 `p:<项目ID或项目名>` 重新发送 `/addtask`。

- `未找到项目 "..."`
  - 项目标识错误，先在应用中确认项目 ID/名称。

- `创建任务失败: 标题过长/描述过长`
  - 按限制精简文本后重试。
