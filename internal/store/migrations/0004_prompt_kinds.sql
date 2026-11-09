-- Prompts gain two kinds, handoff and handoff_planner. A CHECK cannot be altered in SQLite, so the table is rebuilt.
CREATE TABLE prompts_new (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('planner', 'session', 'handoff', 'handoff_planner')), template TEXT,
  UNIQUE (workspace_id, kind)
);
INSERT INTO prompts_new SELECT id, created_at, updated_at, workspace_id, kind, template FROM prompts;
DROP TABLE prompts;
ALTER TABLE prompts_new RENAME TO prompts;
