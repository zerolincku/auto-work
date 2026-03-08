CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_name
  ON projects(name);

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_path
  ON projects(path);

ALTER TABLE tasks ADD COLUMN project_id TEXT REFERENCES projects(id);

CREATE INDEX IF NOT EXISTS idx_tasks_project_status_priority_created
  ON tasks(project_id, status, priority DESC, created_at ASC);

