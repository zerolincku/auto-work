DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS run_events;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS agents;

CREATE TABLE projects_rekeyed (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  auto_dispatch_enabled INTEGER NOT NULL DEFAULT 0,
  default_provider TEXT NOT NULL DEFAULT 'claude',
  model TEXT NOT NULL DEFAULT '',
  system_prompt TEXT NOT NULL DEFAULT '',
  failure_policy TEXT NOT NULL DEFAULT 'block',
  frontend_screenshot_report_enabled INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

INSERT INTO projects_rekeyed (
  id,
  name,
  path,
  auto_dispatch_enabled,
  default_provider,
  model,
  system_prompt,
  failure_policy,
  frontend_screenshot_report_enabled,
  created_at,
  updated_at
)
SELECT
  CAST(ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS TEXT),
  name,
  path,
  auto_dispatch_enabled,
  default_provider,
  model,
  system_prompt,
  failure_policy,
  frontend_screenshot_report_enabled,
  created_at,
  updated_at
FROM projects;

DROP TABLE projects;
ALTER TABLE projects_rekeyed RENAME TO projects;

CREATE UNIQUE INDEX idx_projects_name
  ON projects(name);

CREATE UNIQUE INDEX idx_projects_path
  ON projects(path);

CREATE INDEX idx_projects_auto_dispatch_enabled
  ON projects(auto_dispatch_enabled, created_at DESC);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed', 'blocked')),
  provider TEXT NOT NULL DEFAULT 'claude',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  project_id TEXT REFERENCES projects(id),
  retry_count INTEGER NOT NULL DEFAULT 0,
  max_retries INTEGER NOT NULL DEFAULT 0,
  next_retry_at DATETIME
);

CREATE INDEX idx_tasks_status_priority_created
  ON tasks(status, priority DESC, created_at ASC);

CREATE INDEX idx_tasks_project_status_priority_created
  ON tasks(project_id, status, priority DESC, created_at ASC);

CREATE INDEX idx_tasks_failed_retry_due
  ON tasks(status, next_retry_at);

CREATE TABLE agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
  enabled INTEGER NOT NULL DEFAULT 1,
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency >= 1),
  last_seen_at DATETIME,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
  status TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed', 'needs_input', 'cancelled')),
  pid INTEGER,
  heartbeat_at DATETIME,
  started_at DATETIME NOT NULL,
  finished_at DATETIME,
  exit_code INTEGER,
  provider_session_id TEXT,
  prompt_snapshot TEXT NOT NULL,
  result_summary TEXT,
  result_details TEXT,
  idempotency_key TEXT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id),
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE UNIQUE INDEX idx_runs_agent_running
  ON runs(agent_id) WHERE status = 'running';

CREATE UNIQUE INDEX idx_runs_task_running
  ON runs(task_id) WHERE status = 'running';

CREATE INDEX idx_runs_task_status_started
  ON runs(task_id, status, started_at DESC);

CREATE TABLE run_events (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  ts DATETIME NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);

CREATE INDEX idx_run_events_run_ts
  ON run_events(run_id, ts);

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('file', 'url', 'log')),
  value TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);

CREATE INDEX idx_artifacts_run
  ON artifacts(run_id);

CREATE TABLE IF NOT EXISTS id_sequences (
  scope TEXT PRIMARY KEY,
  next_id INTEGER NOT NULL CHECK (next_id >= 1),
  updated_at DATETIME NOT NULL
);

DELETE FROM id_sequences;

INSERT INTO id_sequences(scope, next_id, updated_at)
SELECT 'projects', COALESCE(MAX(CAST(id AS INTEGER)), 0) + 1, CURRENT_TIMESTAMP
FROM projects;

INSERT INTO id_sequences(scope, next_id, updated_at)
VALUES
  ('tasks', 1, CURRENT_TIMESTAMP),
  ('agents', 3, CURRENT_TIMESTAMP),
  ('runs', 1, CURRENT_TIMESTAMP),
  ('run_events', 1, CURRENT_TIMESTAMP),
  ('artifacts', 1, CURRENT_TIMESTAMP);
