package migrate_test

import (
	"context"
	"path/filepath"
	"testing"

	"auto-work/internal/db"
	"auto-work/internal/db/migrate"
	"auto-work/internal/systemprompt"
)

func TestUp_CreatesCoreTables(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	if err := migrate.Up(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	required := []string{
		"schema_migrations",
		"projects",
		"global_settings",
		"tasks",
		"agents",
		"runs",
		"run_events",
		"artifacts",
	}
	for _, table := range required {
		var count int
		row := sqlDB.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, table)
		if err := row.Scan(&count); err != nil {
			t.Fatalf("scan sqlite_master for %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s not found", table)
		}
	}

	var systemPromptColumnCount int
	if err := sqlDB.QueryRow(`
SELECT COUNT(1)
FROM pragma_table_info('global_settings')
WHERE name = 'system_prompt'`).Scan(&systemPromptColumnCount); err != nil {
		t.Fatalf("query global_settings columns: %v", err)
	}
	if systemPromptColumnCount != 1 {
		t.Fatalf("global_settings.system_prompt column not found")
	}

	var projectSystemPromptColumnCount int
	if err := sqlDB.QueryRow(`
SELECT COUNT(1)
FROM pragma_table_info('projects')
WHERE name = 'system_prompt'`).Scan(&projectSystemPromptColumnCount); err != nil {
		t.Fatalf("query projects columns: %v", err)
	}
	if projectSystemPromptColumnCount != 1 {
		t.Fatalf("projects.system_prompt column not found")
	}

	var projectFailurePolicyColumnCount int
	if err := sqlDB.QueryRow(`
SELECT COUNT(1)
FROM pragma_table_info('projects')
WHERE name = 'failure_policy'`).Scan(&projectFailurePolicyColumnCount); err != nil {
		t.Fatalf("query projects failure_policy column: %v", err)
	}
	if projectFailurePolicyColumnCount != 1 {
		t.Fatalf("projects.failure_policy column not found")
	}

	var projectFrontendScreenshotColumnCount int
	if err := sqlDB.QueryRow(`
SELECT COUNT(1)
FROM pragma_table_info('projects')
WHERE name = 'frontend_screenshot_report_enabled'`).Scan(&projectFrontendScreenshotColumnCount); err != nil {
		t.Fatalf("query projects frontend_screenshot_report_enabled column: %v", err)
	}
	if projectFrontendScreenshotColumnCount != 1 {
		t.Fatalf("projects.frontend_screenshot_report_enabled column not found")
	}

	var taskDependsOnColumnCount int
	if err := sqlDB.QueryRow(`
SELECT COUNT(1)
FROM pragma_table_info('tasks')
WHERE name = 'depends_on'`).Scan(&taskDependsOnColumnCount); err != nil {
		t.Fatalf("query tasks depends_on column: %v", err)
	}
	if taskDependsOnColumnCount != 0 {
		t.Fatalf("tasks.depends_on column should be removed")
	}
}

func TestUp_ReplacesLegacyGlobalSystemPromptAtVersion19(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("initial migrate up: %v", err)
	}

	legacyPrompt := `Mandatory Workflow:
1) First, create or update project-root file curret_task.md with current task info: task_id, run_id, title, description, start_time, and status=running.
2) Inspect actual repository files/code/tests related to this task (not task status metadata) to decide whether task outcome is already present.
3) If the task is already satisfied in code/files, do not repeat changes; update curret_task.md status=success with reason=already_done.
4) Do not ask user questions. Do not wait for user confirmation. Make the safest non-destructive assumptions and continue execution directly.
5) If required information is truly missing and task cannot be completed reliably, call todo.report_result with status=failed and include the exact missing info in details.
6) If not completed, execute only this task scope. Keep curret_task.md updated when status changes (running/success/failed).
7) MCP server alias in this run is auto-work (NOT todo). Use MCP tools from auto-work: todo.list_pending_tasks, todo.list_history_tasks, todo.get_task_detail.
8) If you find follow-up work, call MCP tool todo.create_tasks (supports batch items) on server auto-work.
9) Do NOT run codex mcp list to determine availability; that shows global config, while this run injects MCP via CLI overrides.
10) Before calling todo.report_result, you must run git commit for this task: run "git status --porcelain"; if there are changes then "git add -A" and "git commit -m ""任务 {{task_id}}：{{task_title}}"""; if no changes then create an empty commit "git commit --allow-empty -m ""任务 {{task_id}}：{{task_title}}（空提交）""".
11) Put git commit hash into report details.
12) Before exit, call MCP tool todo.report_result exactly once on server auto-work.
13) todo.report_result fields: status(success|failed), summary, details.
14) If failed, explain reason in details and update curret_task.md status=failed.
15) If tool call failed, stop and explain what failed.`

	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO global_settings(
  id, telegram_enabled, telegram_bot_token, telegram_chat_ids, telegram_poll_timeout, telegram_proxy_url, system_prompt, created_at, updated_at
) VALUES (1, 0, '', '', 30, '', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET system_prompt = excluded.system_prompt, updated_at = CURRENT_TIMESTAMP
`, legacyPrompt); err != nil {
		t.Fatalf("seed legacy global_settings row: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 19`); err != nil {
		t.Fatalf("delete version 19 migration record: %v", err)
	}

	if err := migrate.Up(ctx, sqlDB); err != nil {
		t.Fatalf("re-run migrate up: %v", err)
	}

	var got string
	if err := sqlDB.QueryRowContext(ctx, `SELECT system_prompt FROM global_settings WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("query system_prompt: %v", err)
	}
	if got != systemprompt.DefaultGlobalSystemPromptTemplate {
		t.Fatalf("unexpected system prompt after version 19 migration: %q", got)
	}
}
