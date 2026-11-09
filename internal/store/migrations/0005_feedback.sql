-- The feedback inbox: what the user (or the planner on their behalf) noted about Hadron itself.
CREATE TABLE feedback (
  id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('bug', 'idea', 'annoyance', 'praise')),
  text TEXT NOT NULL, area TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
  diagnostics TEXT NOT NULL DEFAULT 'null',
  status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'planned', 'in_progress', 'done', 'wontfix')),
  note TEXT NOT NULL DEFAULT '', github_url TEXT NOT NULL DEFAULT ''
);
CREATE INDEX feedback_status ON feedback (status, created_at);
