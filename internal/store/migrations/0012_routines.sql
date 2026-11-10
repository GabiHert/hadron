-- Routines: a prompt sent to the planner or a card's session on a schedule (cron or every N minutes).
CREATE TABLE routines (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  cron TEXT NOT NULL DEFAULT '', every_minutes INTEGER NOT NULL DEFAULT 0,
  target TEXT NOT NULL CHECK (target IN ('planner', 'card')),
  card_id TEXT REFERENCES cards(id) ON DELETE CASCADE,
  prompt TEXT NOT NULL DEFAULT '', catch_up INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT, last_run_at TEXT, last_result TEXT NOT NULL DEFAULT ''
);
CREATE INDEX routines_ws ON routines (workspace_id, created_at);
