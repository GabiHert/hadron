-- What integrations and scripts logged and did: one row per op run, plus the lines their code logged.
CREATE TABLE script_logs (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  script_name TEXT NOT NULL, op TEXT NOT NULL, ok INTEGER NOT NULL DEFAULT 1,
  duration_ms INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX script_logs_name ON script_logs (script_name, created_at);
