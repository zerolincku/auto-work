# Auto Work MVP 验收清单（V1）

## 1. 功能验收

- 可以创建任务（title/description/priority）。
- 任务支持依赖关系；依赖未完成时不会被调度。
- 调度器按优先级自动派发空闲 agent。
- 同一 agent 同时仅 1 个 running run。
- Claude Runner 可启动并完成一次 headless 执行。
- 执行事件被记录并可在 UI 查看。
- AI 能通过 MCP `auto-work.report_result` 上报成功/失败结果。
- 上报后任务状态自动更新为 `done/failed/blocked`。

## 2. 异常验收

- Claude 进程异常退出时，run 被标记 `failed`，并记录退出码。
- heartbeat 超时后，run 自动失败并释放 agent。
- MCP 重复上报（同 idempotency_key）不会重复写入。
- 非法 run_id 上报会被拒绝并记录审计日志。

## 3. 安全验收

- 默认仅启用最小工具集（可配置）。
- 禁止任务写出 workspace 根目录。
- 敏感配置（token）不明文存储在日志中。
- 所有执行日志与上报日志可追溯到 run_id。

## 4. 性能验收（本地单机）

- 任务从 `pending` 到被派发延迟 < 60 秒（默认 tick=30 秒）。
- 连续 20 个任务串行执行，应用无崩溃。
- UI 打开 run 详情页面时，日志加载时间 < 2 秒（10k 行以内）。

## 5. DoD（完成定义）

- 核心模块（Task/Scheduler/Runner/MCP）均有单元测试。
- 至少一条端到端自动化用例覆盖完整闭环。
- 文档齐全：架构、配置说明、故障排查。
- 可打包出可安装桌面版本并在目标系统运行。
