-- Which Claude model a session runs: a card's own, its launch profile's, the workspace default
-- for cards, and the planner's. An empty value means "not chosen here".
ALTER TABLE cards ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE launch_profiles ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN default_model TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN planner_model TEXT NOT NULL DEFAULT '';
