-- Sessions that should come back after Hadron restarts, updates or crashes. A session is flagged
-- when it starts and unflagged when it ends on its own or is stopped by the user; quitting the app
-- keeps the flag. Sessions that were running when this migration ran (an update from an older
-- version killed them) are flagged so the update itself restores them.
ALTER TABLE sessions ADD COLUMN restore INTEGER NOT NULL DEFAULT 0;
UPDATE sessions SET restore = 1 WHERE status = 'running';
