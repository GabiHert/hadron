-- Prompt layers: a launch profile and a card can each add instructions on top of the workspace prompt.
ALTER TABLE launch_profiles ADD COLUMN prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN prompt TEXT NOT NULL DEFAULT '';
