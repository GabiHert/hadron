-- A card's session can sleep: Hadron stops its claude process to give its memory back and resumes
-- the conversation on the next message. The session row stays "exited" with asleep set, and the
-- card keeps liveness "ended" with asleep set; the app reports such a card's liveness as "asleep".
-- (The status and liveness columns have CHECK constraints, so the state is a column of its own.)
ALTER TABLE sessions ADD COLUMN asleep INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cards ADD COLUMN asleep INTEGER NOT NULL DEFAULT 0;
