ALTER TABLE projects ADD COLUMN auto_dispatch_enabled INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_projects_auto_dispatch_enabled
  ON projects(auto_dispatch_enabled, created_at DESC);
