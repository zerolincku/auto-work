# MCP 配置与验证（同进程 HTTP）

更新时间：2026-03-05

本文档对应当前实现：**MCP Server 与 auto-work 后端同进程启动**，仅使用内置 HTTP 端点。

## 1. 关键约定

- MCP server 别名固定：`auto-work`
- 工具名固定：`auto-work.report_result`、`auto-work.create_tasks`、`auto-work.update_task`、`auto-work.delete_task`、`auto-work.list_pending_tasks`、`auto-work.list_history_tasks`、`auto-work.get_task_detail`
- `auto-work` 同时是 server 别名与工具名前缀

## 2. 运行原理（现在是怎样工作的）

1. `auto-work` 启动后端时，会在同一进程里启动 MCP HTTP Server。
2. Runner（Claude/Codex）在每次 run 启动时，自动注入：
   - `mcpServers.auto-work.url=<base_url>?run_id=...&task_id=...`
3. AI 通过该 URL 调用 MCP 工具，后端直接回写 runs/tasks/event。

这意味着：

- 不需要单独再启动一个 MCP 进程
- 自动派发链路直接使用当前 run 注入的 MCP HTTP URL

## 3. 全局安装 MCP（Claude Code + Codex）

先说明边界：

- auto-work 自动派发链路仍然是“每次 run 注入”，不依赖全局配置。
- 全局配置只用于你在终端里直接运行 Claude Code/Codex 时，能直接访问 `auto-work` MCP。

### 3.1 前置条件：固定 MCP HTTP 端口

全局配置要求 MCP URL 稳定，所以请固定端口，不要用 `:0`：

```bash
AUTO_WORK_MCP_HTTP_URL=http://127.0.0.1:39123/mcp \
wails dev
```

### 3.2 Claude Code（全局 user scope）

先清理旧项，再添加：

```bash
claude mcp remove --scope user auto-work || true
claude mcp add --scope user --transport http auto-work http://127.0.0.1:39123/mcp
claude mcp get auto-work
```

### 3.3 Codex（全局）

先清理旧项，再添加：

```bash
codex mcp remove auto-work || true
codex mcp remove todo || true
codex mcp add auto-work --url http://127.0.0.1:39123/mcp
codex mcp list --json
```

说明：  
`codex exec -c 'mcp_servers....'` 属于单次注入，不会写入全局。

## 4. 应该怎么配置

### 4.1 推荐默认（无需额外 MCP 启动命令）

```bash
AUTO_WORK_ENABLE_MCP_CALLBACK=1 \
AUTO_WORK_REQUIRE_MCP_CALLBACK=1 \
wails dev
```

说明：

- `AUTO_WORK_MCP_HTTP_URL` 默认为 `http://127.0.0.1:39123/mcp`（固定端口）

### 4.2 指定固定端口（可选）

```bash
AUTO_WORK_MCP_HTTP_URL=http://127.0.0.1:47081/mcp \
wails dev
```

## 5. 一步一步验证

### Step 1：启动应用

```bash
wails dev
```

启动日志中会看到类似：

```text
[mcp-http] in-process server listening at http://127.0.0.1:39123/mcp
```

### Step 2：做一次真实派发

在 UI 中：

1. 新建任务
2. 点击“派发一次”
3. 等待 run 结束

### Step 3：确认 MCP 回写事件（可选）

```bash
sqlite3 ./data/auto-work.db \
  "SELECT run_id, kind, substr(payload,1,120) FROM run_events WHERE kind LIKE 'mcp.%' ORDER BY ts DESC LIMIT 20;"
```

看到 `mcp.report_result.applied` 即表示回调成功。

## 6. 常见问题

1. `unknown MCP server 'todo'`  
   原因：把工具前缀当成 server 名。  
   处理：server 必须是 `auto-work`。

2. `unknown MCP server 'auto-work'`  
   原因：注入 key 写错。  
   处理：使用 `mcp_servers.auto-work...`。

3. `...without mcp report_result callback`  
   原因：run 结束前未成功调用 `auto-work.report_result`。  
   处理：检查模型侧工具权限、MCP URL 注入、调用参数。

4. `start in-process mcp http server failed (listen ...): address already in use`  
   原因：端口冲突。  
   处理：改 `AUTO_WORK_MCP_HTTP_URL` 为其他可用端口（例如 `http://127.0.0.1:47081/mcp`）。
