CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed', 'blocked')),
  provider TEXT NOT NULL DEFAULT 'claude',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status_priority_created
  ON tasks(status, priority DESC, created_at ASC);

CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
  enabled INTEGER NOT NULL DEFAULT 1,
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency >= 1),
  last_seen_at DATETIME,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_agent_running
  ON runs(agent_id) WHERE status = 'running';

CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_task_running
  ON runs(task_id) WHERE status = 'running';

CREATE INDEX IF NOT EXISTS idx_runs_task_status_started
  ON runs(task_id, status, started_at DESC);

CREATE TABLE IF NOT EXISTS run_events (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  ts DATETIME NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);

CREATE INDEX IF NOT EXISTS idx_run_events_run_ts
  ON run_events(run_id, ts);

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('file', 'url', 'log')),
  value TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id)
);

CREATE INDEX IF NOT EXISTS idx_artifacts_run
  ON artifacts(run_id);
