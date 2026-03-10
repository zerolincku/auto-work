DROP INDEX IF EXISTS idx_runs_agent_running;
CREATE INDEX IF NOT EXISTS idx_runs_agent_running
  ON runs(agent_id) WHERE status = 'running';
