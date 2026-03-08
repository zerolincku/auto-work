# Telegram Bot 配置与 Chat ID 获取指南

本文档用于完成以下流程：
- 新建 Telegram Bot
- 获取 Bot Token
- 给 Bot 发消息
- 在 `auto-work` 控制台日志中获取 `chat_id`
- 回填到“全局设置”完成授权

## 1. 新建 Bot（BotFather）

1. 打开 Telegram，搜索并进入 `@BotFather`。
2. 发送命令：`/newbot`。
3. 按提示输入：
   - Bot 显示名（可中文）
   - Bot 用户名（必须以 `bot` 结尾，例如 `auto_work_helper_bot`）
4. 创建成功后，BotFather 会返回一段 Token，格式类似：

```text
123456789:AAxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

请妥善保存该 Token，不要泄露。

## 2. 在 auto-work 中配置 Bot

打开应用“全局设置”，填写：
- 勾选 `启用 Telegram Bot`
- `Bot Token`：粘贴上一步的 token
- `轮询超时`：默认 `30`
- `代理地址（可选）`：在网络受限环境下填写，例如：
  - `http://127.0.0.1:7890`
  - `socks5://127.0.0.1:1080`
- `允许的 Chat IDs`：先留空（先获取 chat_id）

点击 `保存设置`。

## 3. 给 Bot 发消息

1. 在 Telegram 搜索你刚创建的 Bot 用户名。
2. 打开聊天窗口后，发送任意消息（推荐先发 `/start`）。

## 4. 从日志获取 chat_id

应用已实现“收到消息即打印 chat_id”。

### 4.1 开发模式（推荐）
如果你通过 `wails dev` 启动，直接看启动终端输出，示例：

```text
[telegram] incoming message: chat_id=123456789 chat_type=private from=@yourname command=/start text="/start"
```

`chat_id=...` 即你要填的值。

### 4.2 打包后运行
请从终端启动应用再观察日志输出（不要直接双击图标）：

```bash
/Applications/auto-work.app/Contents/MacOS/auto-work
```

收到消息后同样会打印 `chat_id=...`。

## 5. 回填允许的 Chat IDs

把上一步得到的 chat_id 填回“全局设置”中的 `允许的 Chat IDs`，多个用逗号分隔：

```text
123456789,-1009876543210
```

说明：
- 私聊通常是正数 ID。
- 群组/超级群通常是负数 ID（常见 `-100...`）。

保存后，只有白名单 chat_id 可以调用 bot 指令。

## 6. 快速验证

在 Telegram 对 Bot 发送：
- `/help`
- `/recent 5`
- `/projects`
- `/tasklogs <task_id>`

更多指令（含 `/addtask` 任务创建）请见：
- [`05-telegram-commands.md`](./05-telegram-commands.md)

若返回正常，说明配置完成。

## 7. 常见问题

### 7.1 超时（`dial tcp ... i/o timeout`）
- 一般是网络不可达 `api.telegram.org`。
- 在“全局设置”中配置 `代理地址`，然后保存重试。

### 7.2 发了消息但没有日志
- 确认应用在运行且 `启用 Telegram Bot` 已勾选并保存。
- 确认使用终端启动并查看的是该进程日志。
- 确认消息确实发给了正确的 Bot。

### 7.3 配完后无响应
- 先看应用内提示（保存时会返回具体错误）。
- 检查 token 是否正确、是否包含多余空格。
- 检查代理地址格式（仅支持 `http/https/socks5/socks5h`）。

## 8. 前端改动后自动发截图

从 2026-03-05 起，系统新增了如下行为：
- 当任务执行成功（`task=done`）后，会检查本次 run 是否新增了前端文件改动。
- 若命中前端改动，系统会从 AI 的 `auto-work.report_result.details` 中提取截图地址（文件路径/URL），并通过 Telegram 发送地址摘要。
- 对于本地可读的图片路径（如 `.png/.jpg/.jpeg/.webp`），会额外作为图片消息发送。

判定与前提：
- 仅统计本次 run 相对“启动时基线”的新增改动（避免把历史脏改动重复通知）。
- 项目路径需要是 Git 仓库（通过 `git diff/ls-files` 判断变更）。
- 前端文件识别包括：`frontend/` 目录、常见前端扩展（`tsx/jsx/vue/svelte/css/html` 等）与前端构建配置文件。

默认策略：
- 截图能力默认开启，无需配置固定 URL、截图数量或截图间隔。
- 页面地址和截图数量由 AI 在执行任务时自行判断（例如先启动前端项目并按改动页面逐个截图）。
- 系统要求 AI 在 `report_result.details` 里新增 `Screenshots` 段落并逐行给出地址。

可选环境变量：
- `AUTO_WORK_TELEGRAM_FRONTEND_SCREENSHOT_ENABLED`
  - 默认 `1`（开启）
  - 设为 `0` 或 `false` 可关闭截图发送

前置条件：
- AI 任务执行需要在结果详情里写出截图地址，否则 Telegram 只能提示“检测到前端改动但未发现截图地址”。
