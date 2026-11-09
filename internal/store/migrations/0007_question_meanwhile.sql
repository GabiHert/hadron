-- What the session does while a question waits for the user.
ALTER TABLE questions ADD COLUMN meanwhile TEXT NOT NULL DEFAULT '';
