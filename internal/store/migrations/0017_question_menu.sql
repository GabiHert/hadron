-- A question can stand for one of Claude Code's menus on a card's terminal (an MCP server
-- approval, a permission prompt, an AskUserQuestion). menu holds {"sig":..,"kind":..} as JSON:
-- the menu it was made from, so the same menu never makes a second question. Empty for questions
-- a session asked with question_ask.
ALTER TABLE questions ADD COLUMN menu TEXT NOT NULL DEFAULT '';
