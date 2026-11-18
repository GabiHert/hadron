-- Whether a session was working (Claude busy on a task) the last time Hadron saw it. It is kept
-- up to date as the session runs and frozen when Hadron quits, so after a restart, an update or a
-- crash Hadron knows which restored sessions were mid-task and can ask them to carry on.
ALTER TABLE sessions ADD COLUMN working INTEGER NOT NULL DEFAULT 0;
