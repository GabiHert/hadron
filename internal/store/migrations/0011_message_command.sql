-- Messages gain the kind command: a slash command the user sent from a chat straight to a session's
-- terminal. A CHECK cannot be altered in SQLite, so the table is rebuilt.
CREATE TABLE messages_new (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  channel TEXT NOT NULL, author TEXT NOT NULL CHECK (author IN ('you', 'planner', 'session', 'rule', 'system')),
  kind TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'report', 'widget', 'event', 'answer', 'command')),
  report_kind TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', widget_title TEXT NOT NULL DEFAULT '',
  widget_html TEXT NOT NULL DEFAULT '', delivered INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
  question_id TEXT NOT NULL DEFAULT ''
);
INSERT INTO messages_new (id, created_at, updated_at, workspace_id, channel, author, kind, report_kind, text, widget_title, widget_html, delivered, reason, question_id)
  SELECT id, created_at, updated_at, workspace_id, channel, author, kind, report_kind, text, widget_title, widget_html, delivered, reason, question_id FROM messages;
DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;
CREATE INDEX messages_channel ON messages (workspace_id, channel, created_at);
