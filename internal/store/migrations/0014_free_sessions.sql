-- Free sessions: terminals in the Sessions space that belong to no card. They are global; the
-- workspace is only a tag. A shell or a claude, in a folder, with an optional name. Restore says
-- the session is meant to be running, so it comes back after Hadron restarts.
CREATE TABLE free_sessions (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('shell', 'claude')),
  command TEXT NOT NULL DEFAULT '', cwd TEXT NOT NULL DEFAULT '',
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
  pid INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'exited')),
  exit_code INTEGER NOT NULL DEFAULT 0, claude_session_id TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT,
  restore INTEGER NOT NULL DEFAULT 0
);
