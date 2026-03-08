# Auto Work 开发计划（历史里程碑草案）

> 说明：本文档用于留存早期里程碑拆解，包含阶段性表述（如 “Claude only”）。当前实际能力请以 `README.md` 与 `docs/06-mcp-config.md` 为准（已支持 Claude + Codex、同进程 HTTP MCP 多工具）。

## 1. 里程碑总览

### M0（1-2 天）项目初始化
- 初始化 Wails 项目、目录结构、基础日志与配置加载。
- 引入 SQLite + migration 工具。
- 建立基础 CI（lint + test）。

交付物：
- 可启动桌面应用骨架。
- 数据库初始化成功。

### M1（3-5 天）任务管理闭环
- 完成 `tasks` CRUD。
- 完成基础任务列表/详情 UI。
- 支持优先级、依赖、状态展示。

交付物：
- 用户可手动创建/编辑/删除任务。
- UI 可查看任务状态流转。

### M2（5-7 天）Scheduler + Run 管理
- 完成 scheduler tick 与 claim 机制。
- 完成 `runs` 生命周期管理、互斥控制、heartbeat。
- 完成 run 日志落库与展示。

交付物：
- 系统可自动挑选下一任务并创建 run。
- 同一个 agent 不会并发执行多个任务。

### M3（7-10 天）Claude Runner 接入
- 接入 `claude -p --output-format stream-json`。
- 解析事件流并同步到 run_events。
- 处理异常：超时、退出码、中断恢复。

交付物：
- 任务可由 Claude CLI 自动执行。
- 失败可追踪到事件与日志。

### M4（4-6 天）MCP 上报闭环
- 实现 MCP Server（V1 工具：`auto-work.report_result`）。
- Runner 端注入任务 Prompt 契约。
- 上报成功后自动更新 task/run 终态。

交付物：
- AI 完成任务后可结构化上报并落库。
- UI 可展示 summary/details/artifacts。

### M5（3-5 天）稳定性与安全加固
- 幂等处理、重试策略、异常兜底。
- 路径隔离、安全白名单、敏感配置存储。
- 增加 E2E 回归用例。

交付物：
- 可连续运行 24h 无异常崩溃。
- 关键异常路径可恢复。

## 2. 建议目录结构

```text
auto-work/
├─ app/                      # Wails frontend
├─ internal/
│  ├─ domain/                # 实体与状态机
│  ├─ repository/            # sqlite 持久化
│  ├─ service/
│  │  ├─ task/
│  │  ├─ scheduler/
│  │  └─ run/
│  ├─ runner/
│  │  ├─ provider.go         # ProviderRunner 接口
│  │  ├─ claude/
│  │  └─ codex/              # 预留
│  ├─ mcp/
│  ├─ eventbus/
│  └─ config/
├─ migrations/
├─ docs/
└─ main.go
```

## 3. 周计划（可直接执行）

### 第 1 周
- 完成 M0 + M1。
- 明确任务模型与状态机。
- 完成任务看板 UI 与本地持久化。

验收：
- 手工可完成“新建任务 -> 修改优先级 -> 标记完成”。

### 第 2 周
- 完成 M2。
- 实现调度器和 run 互斥。
- 实现 heartbeat 和 run 日志页。

验收：
- 新建 3 个任务后可自动串行执行占位 runner（mock）。

### 第 3 周
- 完成 M3。
- Claude CLI 真机接入 + 事件解析。
- 处理 CLI 异常路径（中断、超时、工具拒绝）。

验收：
- 至少 10 次连续自动任务执行成功率 >= 90%。

### 第 4 周
- 完成 M4 + M5。
- MCP 上报闭环、安全与稳定性强化。
- 准备内测发布包。

验收：
- “自动派发 -> Claude 执行 -> MCP 上报 -> 任务闭环”稳定跑通。

## 4. 工程规范与质量门禁

- 每个里程碑必须有：
- 单元测试覆盖核心服务（Task/Scheduler/Runner/MCP）。
- 至少 1 条集成测试（CLI mock 或真实命令）。
- 失败场景回归测试（超时、重复上报、非法 run_id）。

- CI 建议：
- `go test ./...`
- `golangci-lint run`
- 前端 `pnpm test`（如使用 React/Vue 测试框架）

## 5. 风险与预案

- 风险：Claude CLI 输出格式版本变化。
- 预案：事件解析层做 schema 兼容与未知事件透传落库。

- 风险：MCP 调用漏报导致任务悬挂。
- 预案：进程退出兜底；超时后自动失败并提示重试。

- 风险：自动工具权限过大。
- 预案：默认最小权限 + 明确白名单 + 可视化风险提示。

## 6. 发布策略

- Alpha（内部）：仅本地 SQLite、单 agent。
- Beta（小范围）：加入崩溃恢复、导出日志。
- GA：增加 Codex provider（复用同一 Runner 接口）。
