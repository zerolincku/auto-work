CREATE TABLE IF NOT EXISTS global_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  telegram_enabled INTEGER NOT NULL DEFAULT 0,
  telegram_bot_token TEXT NOT NULL DEFAULT '',
  telegram_chat_ids TEXT NOT NULL DEFAULT '',
  telegram_poll_timeout INTEGER NOT NULL DEFAULT 30,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

