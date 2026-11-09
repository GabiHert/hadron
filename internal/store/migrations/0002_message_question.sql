-- A question's card and its answer in the conversation point at the question they belong to.
ALTER TABLE messages ADD COLUMN question_id TEXT NOT NULL DEFAULT '';
