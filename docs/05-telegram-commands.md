# Telegram 指令手册

本文档只列出当前 Telegram Bot 支持的主指令，不再提供重复别名。

## 前置条件

- 已完成 Bot 配置并启用（见 [`04-telegram-bot-setup.md`](./04-telegram-bot-setup.md)）。
- 当前会话 `chat_id` 已加入允许列表。

## 指令总览

- `/help`：查看帮助。
- `/addproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]`：创建项目。
- `/setprovider <项目ID或项目名> | <claude|codex>`：修改项目默认 Provider。
- `/autodisp <项目ID或项目名> | on|off`：开启或关闭项目自动派发。
- `/projects [n]`：查看项目列表，默认 8，最大 20。
- `/project <项目ID或项目名>`：查看单个项目详情。
- `/addtask <标题> | <描述>`：单项目场景下创建任务。
- `/addtask <项目ID或项目名> | <标题> | <描述>`：多项目场景下创建任务。
- `/dispatch <task_id>`：派发指定任务。
- `/recent [n]`：查看最近执行中或已完成任务，默认 5，最大 20。
- `/pending [项目ID或项目名] [n]`：查看待执行任务，默认 5，最大 20。
- `/failed [项目ID或项目名] [n]`：查看失败/阻塞任务，默认 5，最大 20。
- `/running [项目ID或项目名] [n]`：查看运行中任务，默认 5，最大 20。
- `/task <task_id>`：查看任务详情与最近一次运行状态。
- `/logs <run_id|task_id> [n]`：查看日志摘要，默认 20，最大 80。
- `/tasklogs <task_id> [n]`：查看任务最近一次运行日志，默认 20，最大 80。

## 项目管理

### 创建项目

```text
/addproject <项目名称> | <项目路径> [| claude|codex] [| block|continue]
```

示例：

```text
/addproject 支付系统 | /srv/payments
/addproject 自动工作台 | /Users/me/work/auto-work | codex | continue
```

- 默认 Provider 为 `claude`。
- 默认失败策略为 `block`。

### 修改默认 Provider

```text
/setprovider <项目ID或项目名> | <claude|codex>
```

示例：

```text
/setprovider 支付系统 | codex
/setprovider proj-123 | claude
```

### 修改自动派发

```text
/autodisp <项目ID或项目名> | on|off
```

示例：

```text
/autodisp 支付系统 | on
/autodisp proj-123 | off
```

### 查看项目

```text
/projects [n]
/project <项目ID或项目名>
```

- `/projects` 展示项目 ID、默认 Provider、Model、自动派发状态、失败策略和路径摘要。
- `/project` 展示项目详情，以及运行中、待执行、失败/阻塞任务预览。

## 任务创建与派发

### 创建任务

```text
/addtask <标题> | <描述>
/addtask <项目ID或项目名> | <标题> | <描述>
```

示例：

```text
/addtask 修复登录超时 | 排查 timeout 根因并补测试
/addtask proj-123 | 优化重试策略 | 引入指数退避并补测试
/addtask 支付系统 | 对账任务补偿 | 新增失败补偿任务
```

规则：

- 分隔符使用英文竖线 `|`。
- 优先级不需要填写，系统会自动追加到当前项目队尾。
- 执行 Provider 不再通过 `/addtask` 指定，统一按项目默认 Provider 决定。
- 标题最长 200 字符；描述最长 4000 字符。
- 多项目且未指定项目时，Bot 会返回项目列表并提示重试。

### 派发任务

```text
/dispatch <task_id>
```

- 只尝试派发指定任务。
- 派发时按项目默认 Provider 选择执行器。
- 若失败策略阻塞或 agent 正忙，会返回对应提示。

## 查询与日志

```text
/recent [n]
/pending [项目ID或项目名] [n]
/failed [项目ID或项目名] [n]
/running [项目ID或项目名] [n]
/task <task_id>
/logs <run_id|task_id> [n]
/tasklogs <task_id> [n]
```

示例：

```text
/projects
/project 支付系统
/pending proj-123 10
/failed 支付系统 5
/running 5
/recent 5
/task 8f4f8c2a-xxxx-xxxx-xxxx-xxxxxxxxxxxx
/logs 3c2f1e5b-xxxx-xxxx-xxxx-xxxxxxxxxxxx 30
/tasklogs 8f4f8c2a-xxxx-xxxx-xxxx-xxxxxxxxxxxx 30
```

说明：

- 不带项目参数时，`/pending`、`/failed`、`/running` 返回全局视角。
- `/logs` 可接受 `run_id` 或 `task_id`；传入 `task_id` 时会自动定位最近一次运行。
- `/tasklogs` 只接受 `task_id`，并返回该任务最近一次运行的日志摘要。

## 常见报错

- `未授权的 Chat ID，已拒绝请求。`
  - 当前 `chat_id` 不在白名单里。

- `存在多个项目，请在命令里指定项目`
  - 使用 `/addtask <项目ID或项目名> | <标题> | <描述>` 重新发送。

- `未找到项目 "..."`
  - 项目标识错误，先用 `/projects` 或界面确认项目 ID。

- `创建任务失败: 标题过长/描述过长`
  - 按限制精简文本后重试。
