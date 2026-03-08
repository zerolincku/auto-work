UPDATE tasks
SET max_retries = 0
WHERE max_retries != 0;

UPDATE tasks
SET next_retry_at = DATETIME('now', '+30 seconds')
WHERE status = 'failed'
  AND next_retry_at IS NULL;
