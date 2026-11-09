# Chat

Channels: `planner`, and `card:<id>` per card.

- The user's messages are typed into the planner's terminal as `[planner] text` or `[KEY nick] text` (no key: `[nick] text`). Text is stripped of control characters and capped at 8,000 characters.
- Reply with `chat_post {channel, text}`: `planner` for the planner chat, `card:<id>` for a card's chat. Only the planner (and the user) may call it.
- `chat_read {channel, since?}` reads a channel. `chat_list` lists channels.
- Keep messages short. Put structure in a widget (`widget_post`), not in a wall of text.
- A message the planner never received (planner not running) is stored with a reason and shown as not delivered.
