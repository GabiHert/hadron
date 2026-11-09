# Fields and links

Field paths: `card.<name>` (column, assignee, liveness, key, nick, stage), `custom.<name>`, and `<integration>.<field>` (such as `jira.status`). `field_schema_get` lists them.

- `card_field_set {card, path, value}` writes a field. For a linked field Hadron calls the integration's update and then runs the rules that watch it.
- `card_fields_get {card}` reads every value.
- Links tie a card to an item in an integration: `link_search {integration, q}`, `link_add {card, integration, item_id}`, `link_refresh`, `link_action_run {link_id, action}`, `link_remove`.
- A card session may set `card.stage`, `custom.*` and the fields of its linked items on its own card, and nothing else. `card.assignee`, `card.key`, `card.nick` and the column belong to the planner and the user.
