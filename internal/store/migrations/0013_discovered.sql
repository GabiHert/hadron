-- Items an integration's discover op returned and Hadron made a card for (or found already linked).
-- Kept after the card is archived or deleted, so the same item never comes back as a new card.
CREATE TABLE discovered (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  integration TEXT NOT NULL, item_id TEXT NOT NULL, card_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (workspace_id, integration, item_id)
);
