-- Every table: id, created_at, updated_at (RFC 3339). Workspace-scoped tables carry workspace_id.
CREATE TABLE workspaces (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  name TEXT NOT NULL, color TEXT NOT NULL DEFAULT '', root TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0,
  integrations TEXT NOT NULL DEFAULT '[]', filter_bar TEXT NOT NULL DEFAULT '[]', ui TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE columns (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL, role TEXT NOT NULL DEFAULT '' CHECK (role IN ('', 'needs-you', 'active', 'blocked', 'done', 'backlog')),
  position INTEGER NOT NULL DEFAULT 0, wip INTEGER NOT NULL DEFAULT 0, fold INTEGER NOT NULL DEFAULT 0, color TEXT NOT NULL DEFAULT ''
);
CREATE INDEX columns_ws ON columns (workspace_id, position);

CREATE TABLE cards (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  column_id TEXT NOT NULL REFERENCES columns(id),
  position INTEGER NOT NULL DEFAULT 0, nick TEXT NOT NULL, key TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '',
  assignee TEXT NOT NULL DEFAULT '', stage TEXT NOT NULL DEFAULT '',
  liveness TEXT NOT NULL DEFAULT 'idle' CHECK (liveness IN ('working', 'idle', 'waiting', 'ended')),
  return_column_id TEXT, launch_profile TEXT NOT NULL DEFAULT '', fields TEXT NOT NULL DEFAULT '{}', archived INTEGER NOT NULL DEFAULT 0,
  UNIQUE (workspace_id, nick)
);
CREATE INDEX cards_col ON cards (column_id, position);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  card_id TEXT REFERENCES cards(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('planner', 'card')),
  command TEXT NOT NULL DEFAULT '', cwd TEXT NOT NULL DEFAULT '', pid INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'exited')),
  exit_code INTEGER NOT NULL DEFAULT 0, claude_session_id TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT
);
CREATE INDEX sessions_card ON sessions (card_id);

CREATE TABLE messages (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel TEXT NOT NULL, author TEXT NOT NULL CHECK (author IN ('you', 'planner', 'session', 'rule', 'system')),
  kind TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'report', 'widget', 'event', 'answer')),
  report_kind TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', widget_title TEXT NOT NULL DEFAULT '',
  widget_html TEXT NOT NULL DEFAULT '', delivered INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX messages_channel ON messages (workspace_id, channel, created_at);

CREATE TABLE questions (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL, kind TEXT NOT NULL CHECK (kind IN ('decision', 'unblock', 'approval')),
  text TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', options TEXT NOT NULL DEFAULT '[]', multi INTEGER NOT NULL DEFAULT 0,
  approval TEXT NOT NULL DEFAULT 'null', status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'withdrawn')),
  answer TEXT NOT NULL DEFAULT 'null', answered_at TEXT,
  UNIQUE (card_id, seq)
);

CREATE TABLE gates (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('merge', 'promote', 'deploy-confirm', 'close')),
  summary TEXT NOT NULL DEFAULT '', since TEXT NOT NULL
);

CREATE TABLE links (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  integration TEXT NOT NULL, item_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '',
  fields TEXT NOT NULL DEFAULT '{}', synced_at TEXT,
  UNIQUE (card_id, integration, item_id)
);

CREATE TABLE rules (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  trigger TEXT NOT NULL DEFAULT '{}', condition TEXT NOT NULL DEFAULT '', actions TEXT NOT NULL DEFAULT '[]',
  builtin INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX rules_ws ON rules (workspace_id, position);

CREATE TABLE views (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL, filters TEXT NOT NULL DEFAULT '{}', position INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE dash_blocks (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0, type TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', span INTEGER NOT NULL DEFAULT 1 CHECK (span IN (1, 2)),
  source TEXT NOT NULL DEFAULT '{}', html TEXT NOT NULL DEFAULT '', schedule TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT '', text_at TEXT
);

CREATE TABLE prompts (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('planner', 'session')), template TEXT,
  UNIQUE (workspace_id, kind)
);

CREATE TABLE launch_profiles (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL, command TEXT NOT NULL DEFAULT 'claude', args TEXT NOT NULL DEFAULT '[]', first_prompt TEXT NOT NULL DEFAULT '',
  env TEXT NOT NULL DEFAULT '{}', worktree INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE scripts (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL, kind TEXT NOT NULL CHECK (kind IN ('integration', 'script')),
  manifest TEXT NOT NULL DEFAULT '{}', source TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'enabled', 'disabled', 'rejected')),
  approved_by TEXT NOT NULL DEFAULT '', approved_at TEXT, test_result TEXT NOT NULL DEFAULT 'null'
);

CREATE TABLE changes (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL, action TEXT NOT NULL, args TEXT NOT NULL DEFAULT 'null', before TEXT NOT NULL DEFAULT 'null', after TEXT NOT NULL DEFAULT 'null',
  reverted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX changes_ws ON changes (workspace_id, created_at);

CREATE TABLE mcp_calls (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL, tool TEXT NOT NULL, args TEXT NOT NULL DEFAULT 'null', ok INTEGER NOT NULL DEFAULT 1, error TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX mcp_calls_ws ON mcp_calls (workspace_id, created_at);

CREATE TABLE settings (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  key TEXT NOT NULL UNIQUE, value TEXT NOT NULL DEFAULT 'null'
);
