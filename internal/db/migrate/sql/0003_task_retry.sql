ALTER TABLE tasks ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN max_retries INTEGER NOT NULL DEFAULT 5;
ALTER TABLE tasks ADD COLUMN next_retry_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_tasks_failed_retry_due
  ON tasks(status, next_retry_at);
