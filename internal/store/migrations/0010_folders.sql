-- The folder a card's session runs in, and the planner's. Empty means the workspace root, which is
-- where every session ran before this migration.
ALTER TABLE cards ADD COLUMN cwd TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN planner_cwd TEXT NOT NULL DEFAULT '';
