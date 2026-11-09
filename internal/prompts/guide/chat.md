# Chat

Channels: `planner`, and `card:<id>` per card.

- The user's messages are typed into the planner's terminal as `[planner] text` or `[KEY nick] text` (no key: `[nick] text`). Text is stripped of control characters and capped at 8,000 characters.
- Reply with `chat_post {channel, text}`: `planner` for the planner chat, `card:<id>` for a card's chat. Only the planner (and the user) may call it.
- `chat_read {channel, since?}` reads a channel. `chat_list` lists channels.
- Messages are shown as Markdown (GitHub flavour): tables, lists, headings, quotes, task lists, `code` and fenced code blocks (with a Copy button), and links, which open in the user's browser. Use a table for a list of PRs or runs instead of pipes in plain text. Raw HTML is shown as text and images are never loaded, so do not embed either.
- Keep messages short. When a picture explains it better (a comparison table, a before/after, a diagram, a timeline, a plan's steps), post a widget with `widget_post` and one or two lines of text with it, not a wall of text; see the widgets topic. Widgets in chat are for explaining: the user adds one to the dashboard from its "⋯" menu if they want it there.
- The user can attach images and PDFs to a message. Hadron keeps a copy under `~/.hadron/attachments/<date>/` and the message ends with one `Attached: /absolute/path` line per file. Claude Code reads an image from its path, so pass the path on when you relay the message to a session.
- A message the planner never received (planner not running) is stored with a reason and shown as not delivered.
- A user message that starts with one `/` (such as `/compact`) is a Claude Code command: Hadron types it straight into that chat's session terminal (the card's, or yours in the planner chat) and stores it as `kind: command`. You do not hear it as a chat line. A message that starts with `//` reaches you with one slash dropped: it is text for you, not a command.
- A message may mention files as `@path` (relative to the card's folder, quoted when it has spaces: `@"docs/release plan.md"`) or cards as `@nick`. Claude Code reads `@path` itself, so relay the mention as written; `@nick` is only a name, with no routing.

## Who wrote a line
Hadron's own notices have a middle dot inside the brackets, which nothing a user, a session or a rule types can contain: `[KEY nick · report] ...` (a session's report), `[KEY nick · answer] Q-NN: ...` (the user's answer; `answer (relayed)` when the planner relayed it), `[rule · name] [KEY nick] ...` (a rule). A line without the dot is the user typing.
