# Hadron: technical spec

This spec holds the contracts builders implement without asking. The product brief and the UX prototype it was written from are kept in private notes.
- The UX reference is the original single-file prototype (private). The built app is now the reference for layout, copy and behaviour.
- `docs/work-items.md` lists the work in build order.

## 1. Stack and repo

| Concern | Choice |
|---|---|
| Module | `github.com/GabiHert/hadron`, Go 1.26 |
| Desktop shell | Wails v2.12 (the same as Deputy), macOS arm64, minimum macOS 14 |
| Frontend | React 18, TypeScript, Vite, plain CSS with design tokens (no Tailwind), Vitest and Testing Library |
| Terminal | xterm.js 5 with the fit addon (frontend). `github.com/creack/pty` (backend). A VT screen model in Go for `session_read_screen` (`github.com/hinshun/vt10x` or `github.com/charmbracelet/x/vt`, whichever builds cleanly) |
| Storage | SQLite (`github.com/mattn/go-sqlite3`, WAL) at `~/.hadron/hadron.db`. `HADRON_HOME` overrides `~/.hadron` (tests always set it). |
| MCP | `github.com/modelcontextprotocol/go-sdk`, Streamable HTTP |
| Expressions | `github.com/google/cel-go` for rule conditions and filters |
| Scripts | `github.com/dop251/goja` sandbox for integrations and user scripts |
| Updates | `internal/update` copied **unchanged** from `~/.claude/skills/release-updater/templates/update/`. Follow that skill's SKILL.md. |

Layout, modelled on `~/Documents/Personal/deputy`:

```
main.go                 Wails bootstrap (window, menu, bind App)
internal/run            Version (set by ldflags), paths (HADRON_HOME)
internal/domain         plain types: Workspace, Column, Card, Message, Question, Gate, Link, Rule, View, Block, Prompt, Script, Change
internal/store          SQLite + migrations (embed *.sql), one repo struct per aggregate
internal/actions        the action registry (§3). Every user-visible operation lives here.
internal/board          columns, cards, placement, rules engine, filters (CEL)
internal/pty            session manager: spawn, write, resize, scrollback, screen snapshot, liveness
internal/chat           channels, routing card chat → planner PTY, planner/session replies
internal/prompts        built-in harness (core prompts, guide topics), handoff defaults, rendering
internal/fields         field schema registry (native + integration-contributed)
internal/script         goja sandbox + strict host API (§8)
internal/dash           dashboard blocks + named queries
internal/mcpserver      MCP HTTP server; registers every action as a tool
internal/update         release-updater template (unchanged)
internal/app            Wails-bound App: Invoke(action, json), PTY bridge, events
frontend/               React app
docs/                   spec, work items, MCP tool list, logo
scripts/                release.sh, build-release.sh (release-updater templates), sign/check helpers
Makefile                dev, build, test, test-go, test-fe, release
release.conf            repo=GabiHert/hadron, gh account GabiHert
NOTICE.md               credits: quant (MIT, Luis Brienze) PTY ideas; Deputy/Notted design system
```

## 2. Data model (SQLite)

Every table has `id TEXT PRIMARY KEY` (a ULID or UUID), `created_at` and `updated_at` (RFC 3339). Workspace-scoped tables carry `workspace_id`.

| Table | Columns (besides the common ones) |
|---|---|
| `workspaces` | `name`, `color`, `root` (absolute folder), `position`, `archived`, `integrations` (JSON array of enabled integration ids), `filter_bar` (JSON array of field paths), `ui` (JSON: collapsed panels, main view), `default_model` and `planner_model` (empty means not chosen), `planner_cwd` (the planner's folder; empty means `root`); several workspaces may share one `root`, and then every workspace after the oldest names its card worktrees `<nick>-<first 6 of its id>` |
| `columns` | `workspace_id`, `name`, `role` (`needs-you`, `active`, `blocked`, `done`, `backlog`), `position`, `wip` (int, 0 means no limit), `fold` (bool: collapsed to a rail), `color` |
| `cards` | `workspace_id`, `column_id`, `position`, `nick` (kebab-case, unique per workspace), `key` (optional, such as `ACME-508`), `title`, `assignee`, `stage`, `liveness` (`working`, `idle`, `waiting`, `ended`), `return_column_id`, `launch_profile`, `prompt` (the card's prompt layer), `model` (empty means not chosen), `cwd` (the folder its session runs in; empty means the workspace root), `fields` (JSON of custom fields), `archived` |
| `sessions` | `card_id` (null for the planner), `workspace_id`, `kind` (`planner`, `card`), `command`, `cwd`, `pid`, `status` (`running`, `exited`), `exit_code`, `claude_session_id` (when known), `started_at`, `ended_at` |
| `messages` | `workspace_id`, `channel` (`planner`, or `card:<cardId>`), `author` (`you`, `planner`, `session`, `rule`, `system`), `kind` (`text`, `report`, `widget`, `event`, `answer`, `command`), `report_kind` (`progress`, `question`, `blocked`, `done`), `text`, `widget_title`, `widget_html`, `delivered` (bool), `reason` |
| `questions` | `workspace_id`, `card_id`, `seq` (Q-01, Q-02 per card), `kind` (`decision`, `unblock`, `approval`), `text`, `context`, `options` (JSON `[{label, description}]`), `multi` (bool), `approval` (JSON `{target, runs_as, reversible, command}`), `status` (`open`, `answered`, `withdrawn`), `answer` (JSON `{selections, notes, via}`), `answered_at` |
| `gates` | `workspace_id`, `card_id`, `kind` (`merge`, `promote`, `deploy-confirm`, `close`), `summary`, `since` |
| `links` | `workspace_id`, `card_id`, `integration`, `item_id`, `title`, `url`, `fields` (JSON snapshot), `synced_at` |
| `discovered` | `workspace_id`, `integration`, `item_id`, `card_id`, `created_at`: items discovery made a card of or found linked; never removed with the card |
| `routines` | `workspace_id`, `name`, `enabled`, `cron` (five fields, local time) or `every_minutes` (5 to 10080), `target` (`planner` or `card`), `card_id`, `prompt`, `catch_up` (bool), `next_run_at`, `last_run_at`, `last_result` |
| `rules` | `workspace_id`, `position`, `name`, `enabled`, `trigger` (JSON `{on: field_changed, field}` or `{on: column_entered, column}`, `card_created`, `report`, `question_opened`, `idle_for {minutes}`, `no_report_for {minutes}`, `cron {spec}`), `condition` (CEL string, may be empty), `actions` (JSON list: `move {column_id}`, `set_field {path, value}`, `notify {text}`, `tell_planner {text}`, `run_action {script, action}`), `builtin` (bool) |
| `views` | `workspace_id`, `name`, `filters` (JSON `{vals: {path: value}, q, expr}`), `position` |
| `dash_blocks` | `workspace_id`, `position`, `type`, `title`, `span` (1 or 2), `source` (JSON `{query, args}`), `html`, `schedule`, `text`, `text_at` |
| `prompts` | `workspace_id`, `kind` (`planner`, `session`, `handoff`, `handoff_planner`), `template` (a null row means the default; for `planner` and `session` that is empty, the workspace layer, with the core harness underneath) |
| `launch_profiles` | `workspace_id`, `name`, `command`, `args` (JSON), `first_prompt` (template), `prompt` (the profile's prompt layer), `model`, `env` (JSON), `worktree` (bool) |
| `scripts` | `workspace_id` (null means global), `name`, `kind` (`integration`, `script`), `manifest` (JSON), `source` (JS text), `status` (`pending`, `enabled`, `disabled`, `rejected`), `approved_by`, `approved_at`, `test_result` (JSON) |
| `changes` | `workspace_id`, `actor` (`you`, `planner`, `session:<nick>`, `rule:<name>`), `action`, `args` (JSON), `before` (JSON), `after` (JSON), `reverted` (bool) |
| `mcp_calls` | `workspace_id`, `actor`, `tool`, `args` (JSON, truncated to 2 KB), `ok`, `error`, `duration_ms` |
| `settings` | `key`, `value` (JSON). Global settings: theme, MCP port, update token reference. |

**Config history:** every action that changes config, meaning workspaces, columns, rules, views, dashboard, prompts, launch profiles and scripts, writes a `changes` row with before and after. `config_revert {change_id}` applies `before`. This takes the place of the brief's "config is a git repo" for v1. A later `pack_export` can write the same config as files.

**Seeding:** a new workspace gets the planner-default columns (Needs you, Working, Blocked, Done, Backlog), the two built-in rules, the default dashboard, and the default prompts by reference.

## 3. The action registry: one source for the UI and MCP

Every operation is an **action**: `name` (snake_case, which is also the MCP tool name), `description`, typed `In` and `Out` structs with JSON tags and `jsonschema` descriptions, a `Mutates` flag, and a `func(ctx, Actor, In) (Out, error)`.

- **The frontend** calls one Wails method: `App.Invoke(name string, argsJSON string) (string, error)`. A typed TS client (`frontend/src/api.ts`) wraps it per action. The TS types are hand-written to match the action structs, and a Go test lists the action names so the TS client can be checked against them.
- **The MCP server** registers every action as a tool, with the input schema derived from `In`.
- **Parity by construction:** the UI cannot do anything MCP can't. A test asserts that `len(registry) == number of registered MCP tools`. A second test fails if any `App` method other than `Invoke` and the PTY bridge is bound.
- **The actor** comes from the caller:
  - the UI is `you`;
  - MCP uses the `X-Hadron-Session` header: the planner's session id maps to `planner`, a card's session to `session:<nick>`, and a missing header to `external`.
  - **User-only actions** (`planner_policy_set`, `planner_set_profile`, `models_set`, `chat_send`, `chat_command`, `integration_secret_set`/`_clear`, `settings_update` of `planner.policy`, `widgets.scripts` and `mcp.user_actions`, and the planner-control denials of `mayControl`/`planner_set_model`) all pass `Call.UserOnly`, which reads the setting `mcp.user_actions`: `external` (default) allows the UI and `external` actors, `all` also the planner and sessions, `off` only the UI. A non-UI caller that passes is written to the config history (`user-only action over MCP`, with the actor) as well as `mcp_calls`. `planner_keys` presses keys in the planner like `session_keys`.
  - Rules run as `rule:<name>`.
- **Every mutating action:**
  - validates its input, returning errors that say what to fix;
  - writes `changes` when it touches config;
  - writes `mcp_calls` (both UI and MCP calls are logged; the UI rows show `you`);
  - emits a Wails event `changed` with `{workspace_id, entities: [...]}`, so the frontend refetches.

### Action list (v1)

- **Workspaces:** `workspace_list`, `workspace_get`, `workspace_create`, `workspace_update`, `workspace_archive`, `workspace_delete`, `workspace_switch`.
- **Columns:** `column_list`, `column_create`, `column_update` (name, role, wip, `collapsed` or its older name `fold`, color), `column_reorder`, `column_delete` (moves its cards to the first other column).
- **Cards:**
  - `card_list` takes a filter `{column_id?, role?, liveness?, expr?}`.
  - The rest: `card_get`, `card_create` (starts the session unless `start: false`), `card_update`, `card_move`, `card_archive`, `card_delete`. `card_create` and `card_update` take an optional `cwd` (§4, folders).
  - `card_report {kind, text}` may only be called by a card's own session (or `you`).
- **Sessions:** `session_start`, `session_stop`, `session_restart`, `session_send {card, text, enter=true}`, `session_keys {card, keys}`, `session_read_screen {card, lines=60, scrollback=false}`, `session_interrupt`, `session_status`.
- **Planner:** `planner_get`, `planner_start`, `planner_restart`, `planner_send`, `planner_read_screen`.
- **Adoption:** `session_adoptable_list {cwd? | all?}`, `session_adopt {claude_session_id, nick, cwd?}`, `planner_adopt {claude_session_id, cwd?}` (§4, folders).
- **Chat:**
  - `chat_list`, `chat_read {channel, since?}`;
  - `chat_send {channel, text}`, sent as you and routed (§5);
  - `chat_command {channel, text}`, a `/command` typed straight into that channel's session terminal (§5);
  - `session_commands_list {card | planner}` and `files_search {card | planner, q, limit}`, read-only, for the composer's `/` and `@` menus (§5);
  - `chat_post {channel, text}`, the planner's reply;
  - `widget_post {channel, title, html}`, `widget_pin {message_id}` (the user or the planner; the planner only when the user asks, §5).
- **Questions and gates:**
  - `question_ask {kind, text, context, options, multi, approval?}`: a session asks about its own card;
  - `question_list`, `question_get`;
  - `question_answer {question_id, selections, notes}`;
  - `question_withdraw`;
  - `gate_set`, `gate_clear`, `gate_list`.
- **Fields and links:** `field_schema_get`, `card_fields_get`, `card_field_set {card, path, value}`, `link_search {integration, q}`, `link_add`, `link_remove`, `link_refresh`, `link_action_run`.
- **Remote sessions:** `remote_session_list`, `remote_session_output`, `remote_session_send`, `remote_session_start`, `remote_session_stop`. These delegate to integrations that declare `sessions`.
- **Routines:** `routine_list`, `routine_create`, `routine_update` (`enabled: false` pauses), `routine_delete`, `routine_run` (now, the schedule unchanged). A session may only list them.
- **Rules:** `rule_list`, `rule_create`, `rule_update`, `rule_reorder`, `rule_delete`, `rule_dry_run`, `rule_log`.
- **Filters and views:** `filter_eval {expr}` (returns card ids), `filter_bar_set`, `view_list`, `view_save`, `view_apply`, `view_delete`.
- **Dashboard:** `dash_get`, `dash_set`, `dash_block_add`, `dash_block_update`, `dash_block_move`, `dash_block_remove`, `dash_block_refresh`, `dash_query_run`.
- **Prompts and launch profiles:** `prompt_get`, `prompt_set`, `prompt_reset`, `prompt_render {kind, card?}`, `prompt_layers_get {kind, card?}`, `hadron_guide {topic?}`, and `launch_profile_*`. Cards also carry a `prompt` layer.
- **Scripts and integrations:**
  - `integration_list`, `integration_scaffold`, `integration_write_file`, `integration_read_file`, `integration_validate`, `integration_test`, `integration_run_op`;
  - `integration_approve` (its outcome follows the planner policy), `integration_disable`, `integration_remove`, `integration_logs`;
  - the `script_*` mirror of these for user scripts.
- **Policy, config and UI:** `planner_policy_get`, `planner_policy_set`, `config_history`, `config_diff`, `config_revert`, `ui_panel_collapse`, `ui_fullscreen`, `ui_main_view`.
- **App:** `app_status`, `settings_get`, `settings_update`, `get_update_status`, `check_for_update`, `start_update`, `set_update_token`, `notify {title, body}`.

## 4. Sessions and terminals (`internal/pty`)

- **Spawning:**
  - One PTY per card session and one per workspace planner.
  - Spawned with `creack/pty` in the session's folder (or the card's worktree), at 120×36 by default and resized by the frontend.
  - The login shell's environment is used, because GUI apps start with a minimal PATH. Read it once with `$SHELL -l -c env`, as Deputy does.
- **Command** (from the launch profile, default `claude`):

  ```
  claude --append-system-prompt <composed prompt> --mcp-config <file> [--resume <id> | --session-id <new uuid>] [--model <model>]
  ```

  - **Model:** `--model` is passed only when a model resolves: the card's `model`, else its launch profile's, else the workspace's `default_model`. The planner uses `planner_model`, else `default_model`. With none, claude uses its own default. The `models` setting is the list the pickers offer (`models_list`, `models_set`; default opus, sonnet, haiku), and a card may hold an id that is not in it. `card_set_model` and `planner_set_model` types `/model <id>` into a running claude session (`/model default` when the choice is cleared and nothing else resolves); otherwise the model applies at the next start. A card change posts an event on the card and writes a `changes` row.

  - The MCP config file sits at `~/.hadron/run/<session-id>.mcp.json`:
    `{"mcpServers":{"hadron":{"type":"http","url":"http://127.0.0.1:<port>/mcp","headers":{"Authorization":"Bearer <token>","X-Hadron-Session":"<session-id>"}}}}`
  - Env: `HADRON_SESSION_ID`, `HADRON_CARD_ID`, `HADRON_WORKSPACE_ID`.
  - The first prompt from the launch profile is sent after a 1.5 s readiness delay, or as soon as the screen shows Claude's input box, whichever comes first.
  - The header the server reads is **`X-Hadron-Session`**; the server maps the session id to its card and actor.
- **Output:**
  - Each output chunk goes to:
    - a 2 MB ring buffer (scrollback);
    - the VT screen model;
    - the frontend, as the Wails event `pty:data:<sessionId>` (base64).
  - When a terminal mounts, the frontend calls `PtyAttach(sessionId)`, which returns the scrollback, so the terminal repaints.
- **Input:**
  - `PtyWrite(sessionId, base64)` from the frontend. Wails runs every bound call on its own goroutine, so two calls in flight could reach the PTY in either order; the frontend (`pty.ts`) therefore sends a session's writes one at a time, each after the previous one returned.
  - `session_send` writes text with bracketed paste (`ESC[200~ … ESC[201~`), then `\r` when `enter`. Writes to one PTY are serialized through a per-session mutex queue, so lines never interleave.
- **Liveness:** derived as follows, and changes emit `changed`.
  - `working`: output in the last 3 s;
  - `waiting`: the card has an open question;
  - `idle`: neither;
  - `ended`: the process exited.
- **Exit:** the session is marked `exited`. Cards keep their history, and `session_restart` starts again (with `claude --resume <id>`). Every claude launch has an id Hadron knows: a fresh start passes `--session-id <uuid>` and stores it on the session row, so a restart always resumes the same conversation unless `fresh: true`.
- **Folders:**
  - A card's session runs in its `cwd`, else the workspace root; the planner runs in the workspace's `planner_cwd`, else the root. Start, restart, resume and restore all use that folder, and so do the `--resume` check and `HasConversation`, because Claude keeps a conversation under `~/.claude/projects/<folder with every non-alphanumeric character replaced by ->/<id>.jsonl` (or under `CLAUDE_CONFIG_DIR`) and resumes it only in that folder.
  - A launch profile with `worktree` makes the card's worktree from the card folder's repository, under `<folder>/.hadron-worktrees/<nick>`.
  - `card_create`, `card_update`, `workspace_update {planner_cwd}`, `session_adopt` and `planner_adopt` take a folder: `~` is expanded, it must be an absolute path to an existing folder, and the workspace root is stored as empty. Validation errors of one call are reported together. Changing a running card's folder applies at its next start, and the result's `note` says so.
- **Adoption:**
  - `session_adoptable_list` lists conversations of 2 KB or more, newest first, with `cwd`, the folder each was run in. By default it reads the workspace root's project folder; `cwd` reads another folder's; `all: true` reads every folder under `projects/`. The `cwd` comes from the transcript's JSON lines, never from decoding the folder name, because the encoding is lossy; a transcript with no `cwd` is left out of `all`. `already_in_hadron` and `held_by` look at every workspace.
  - `session_adopt` makes a card that runs in `cwd` (default the root) and resumes the conversation with `--resume`; `planner_adopt` does the same for the planner and stores `cwd` as `planner_cwd`. Both refuse when the transcript is not under that folder's project folder, or when a card or planner already holds the conversation.
- **Planner:** one per workspace, started when the workspace opens if the setting `planner.autostart` is true (the default).
- **Tests:** use a fake command (`/bin/cat`, or a small Go helper binary in `internal/pty/testdata`) instead of `claude`.

## 5. Chat routing (`internal/chat`)

**Channels:** `planner`, and `card:<id>` for each card.
- **`chat_send` as you on `card:X`:**
  1. Store the message.
  2. Type `[<KEY> <nick>] <text>` into the planner PTY, or `[<nick>] <text>` when the card has no key, using bracketed paste and Enter.
  3. Mark the message `delivered`, or record `reason: planner_not_running`.
- **`chat_send` as you on `planner`:** type `[planner] <text>`.
- **Slash commands (`chat_command`):** in the card chat and the planner chat, a message that starts with one `/` (such as `/compact` or `/model sonnet`) is Claude Code's, not the planner's. The composer sends it with `chat_command {channel, text}`, which types it into that channel's session terminal (the card's, or the planner's own) and presses Enter, as `session_send`/`planner_send` do, and stores it as `author: you, kind: command`; the thread draws it as a "you → terminal" line with the command in a mono pill. It is never typed into the planner as a chat line. A session that is not running is an error and nothing is stored or queued: the composer shows the error inline with a Start button. A leading `//` is the escape: the composer drops one slash and sends the rest with `chat_send`, so `//etc/hosts` reaches the planner as `/etc/hosts`. Only the user may call `chat_command` (`Call.UserOnly`).
- **The `/` and `@` menus:** typing `/` at the start of the message opens a popover above the composer with the session's slash commands from `session_commands_list {card | planner}`: Claude Code's built-ins (a maintained list in `internal/complete`, with one-line descriptions), the project's `<folder>/.claude/commands/**/*.md` (a subfolder makes `ns:cmd`; the description is the frontmatter `description`, else the first line), the user's `~/.claude/commands` (or `CLAUDE_CONFIG_DIR`), and skills (`.claude/skills/*/SKILL.md`, project and user) unless their frontmatter says `user-invocable: false`. A name that appears twice keeps the most specific (project, then user, then built-in). Typing `@` at the start of a word opens the workspace's cards (`@nick`, with key, title and column) and `@planner` (not in the planner's own chat), then the folder's files and folders from `files_search {card | planner, q, limit}`. Picking a file inserts `@relative/path`, quoted (`@"a b/c.md"`) when it has a space; a folder inserts `@dir/` and keeps the menu open inside it. A card mention is plain text: when the planner relays a message it stays as it is.
  - **The folder:** a card's working folder is its worktree when its latest session ran in one, else its `cwd`, else the workspace root; the planner's is its `planner_cwd`, else the root.
  - **`files_search`:** in a git repository it lists `git ls-files --cached --others --exclude-standard` and the folders that hold them; elsewhere it walks the folder without following symlinks, skipping `node_modules`, `.git`, `.hadron-worktrees` and build folders (`dist`, `build`, `target`, `.next` and the like). A listing holds at most 20,000 entries and is cached for 10 seconds. Results are ranked fuzzily (a substring beats a scattered match, the last path segment and word starts score higher) and capped at `limit` (default 50, at most 200). A query that is absolute, starts with `~` or has a `..` segment is refused, and an entry that is a symlink leading out of the folder is never returned. Both actions refuse a session asking about another card, or about the planner.
  - **Keys:** arrows move the selection, Enter or Tab picks, Esc closes the menu for that word. Enter on a command that is already typed out in full sends it, as in Claude Code. The textarea keeps the focus: a click on a row is a mousedown that never blurs it, and blurring the field closes the menu. The menu never opens while an input method is composing (dead keys and accents included), and opens once the composition ends. At most 8 rows show before it scrolls; matched characters are highlighted, and commands and paths are in mono.
- **Sanitizing:** text is stripped of control characters other than newline and capped at 8,000 characters. A leading `/`, `!` or `#` is impossible, because of the tag.
- **Rendering:** every message, report, question text and context, and the dashboard's `prose` and `planner` blocks are drawn as GitHub-flavoured Markdown (`react-markdown` + `remark-gfm`, `components/Markdown.tsx`): tables in a sideways-scrolling box, task lists, headings, quotes, inline and fenced code (fenced blocks have a Copy button), and links. A link opens only for `http`, `https` and `mailto`, in the system browser through Wails' `BrowserOpenURL`; the webview never navigates. Raw HTML is shown as text, never rendered, and images are never loaded (the alt text shows instead).
- **Widgets in chat:** a widget explains; it is not a dashboard candidate. `components/Widget.tsx` draws it as part of the conversation: a quiet caption (11.5px, `--fg3`) when it has a title and no bar at all when it has none, then the frame with a subtle border. Its width is the bubble width (92%, 72% in a full-screen chat), or the full chat width when the HTML starts with `<svg` (a diagram). A "⋯" button in the top-right corner shows only on hover or keyboard focus (`opacity:0` otherwise, still in the tab order) and opens a menu (`role=menu`; arrows move, Esc closes and returns focus) with Open full screen (a portal dialog, `.modal.wide.wfull`, up to 1400px, same sandbox and srcdoc; Esc, Close or a click outside closes it), Copy HTML, and, when the thread can pin, Add to dashboard, which calls `widget_pin` and then reads "On the dashboard ✓", disabled. A dashboard `custom` block uses the same component with `variant="block"`: no menu, no border of its own.
  - **Height:** with scripts on, the frame posts its height (`hadronHeight`). With scripts off, `estimateHeight` sizes a widget that starts with `<svg>` exactly from its `height`, or from its `viewBox` and the frame's measured width (a numeric `width` caps it); other HTML gets a guess from its rows and text length, at most 480px, with a "Show more" toggle (to at least 600px). An explicit `height` (a dashboard block's) wins over both.
- **Attachments:** the composer takes images and PDFs pasted, dropped or picked with its clip button. Each is kept by `attachment_save` (below) and shown as a chip; on send the text gets one `Attached: <absolute path>` line per file, so the planner and the session it relays to can read it. The thread shows a thumbnail for each image path under an attachments folder.
- **`attachment_save {name?, data}`:** only the user may call it. `data` is base64, at most 20 MB; the type is read from the bytes and must be PNG, JPEG, GIF, WebP or PDF. The file goes to `~/.hadron/attachments/<YYYY-MM-DD>/<uuid>.<ext>` (0600) and the result is `{path, url, mime, size, name}`; `url` (images only) is `/attachments/<date>/<file>`, which the asset server's middleware (`internal/attachments`) serves from that folder alone, for GET and HEAD, to names that match `<date>/<uuid>.<png|jpg|gif|webp>`, and nothing else.
- **`chat_post`:** only the planner (or `you` in tests) may call it. It stores an `author: planner` message.
- **`card_report`:** stores an `author: session, kind: report` message on the card's channel. It also types a one-line notice into the planner PTY: `[<KEY> <nick> · report] <kind>: <first 200 chars>`. That is how the planner hears about it.
- **Unforgeable notices:** Hadron's own lines carry ` · ` (a middle dot) inside the brackets: `[<KEY> <nick> · report]`, `[<KEY> <nick> · answer]` (`answer (relayed)` for a relayed answer) and `[rule · <name>] [<KEY> <nick>] <text>`. `chat.Sanitize` and the tag builders remove that character and its look-alikes from everything a user, a session or a rule typed, so a line without the dot is the user typing.
- **`question_ask`:**
  - opens a question, adds a `report_kind: question` message, and fires the `question_opened` trigger;
  - the built-in rule moves the card to the first `needs-you` column and remembers `return_column_id`.
- **`question_answer`:**
  - stores the answer and a `kind: answer` message;
  - the built-in rule moves the card back;
  - types to the planner: `[<KEY> <nick> · answer] Q-NN: <labels>; notes: <notes>`.
  - **Policy:** the planner may answer approvals only when the planner policy allows it. The default policy, set in v1 settings, is `allow_all`; the other values are `decisions_only`, `ask_for_prd` and `user_only` (the planner answers and clears nothing itself). A session may never answer its own question. The policy is global (`planner.policy`) with an optional per-workspace override (`planner.policy.workspace.<id>`, set with `planner_policy_set {workspace_id}`, removed with `inherit`); the workspace's own wins.
  - **Relay:** the planner may pass `on_behalf_of: user` and `source_message_id` to record an answer the user already gave in chat. The message must be the user's own, in the card's channel or the planner channel, newer than the question. It works under every policy and is stored `via: planner-relay`.
  - **Question fields:** `meanwhile` (what the session does while it waits) and, for approvals, `approval.reversible` (`yes`, `no` or `partial`), `reversible_note` (required for `partial`) and `command_lang`.
- **`gate_withdraw`:** takes back a gate that no longer applies; the session that set it, the planner and the user may. `gate_clear` still means the user did or declined it.

## 6. Prompts (`internal/prompts`)

A session's system prompt is built from **layers**, composed in this order and joined under a `## <title>` header each (empty layers are left out):
1. **Core harness** (`core_planner.md`, `core_session.md`, embedded, versioned by `prompts.CoreVersion`). It is never stored and no action edits it. It teaches the role, the reporting and question protocol, explaining with widgets ("Explain with widgets": when a visual makes a message clearer, with one or two lines of text, theme variables, under 256 KB, not for one-line answers, and onto the dashboard only when the user asks), and where to find `hadron_guide`.
2. **Workspace layer:** the `prompts` row for `planner` or `session`. Its default is empty, and a user's older customised row becomes this layer unchanged.
3. **Launch profile layer:** `launch_profiles.prompt` (session kind only).
4. **Card layer:** `cards.prompt` (session kind only).

Every layer supports the variables below. The handoff kinds have no core: they are one text typed once into an adopted conversation. `prompt_layers_get {kind, card?}` returns each layer (template and rendered) and the composed text, and `prompt_render` returns the composed text. `hadron_guide {topic?}` serves twelve embedded topics (`internal/prompts/guide/*.md`: overview, chat, questions, reports, widgets, framed around teaching, with examples that use the theme's CSS variables (a flow diagram with arrows, a before/after table, a timeline, a plan's steps with states, a status table, a bar chart, a sparkline), dashboard, fields-links, rules-filters, integrations, prompts, feedback, debugging), and the MCP server's `instructions` point at it. Settings → Harness shows the core read-only, the workspace layer, and the composed preview.
- **Variables:** `{{workspace.name}}`, `{{workspace.root}}`, `{{columns}}`, `{{integrations}}`, `{{rules}}`, `{{policy}}`, `{{card.nick}}`, `{{card.key}}`, `{{card.links}}`.
- An unknown variable is left as is, and `prompt_set` returns a warning listing it.

## 7. Fields, rules, filters (`internal/fields`, `internal/board`)

- **Field paths:** `card.<name>` (native: `column`, `assignee`, `liveness`, `key`, `nick`, `stage`), `custom.<name>`, and `<integration>.<field>`.
- **Schema entry:** `{path, label, type (text|number|enum|user|date|bool), options?, filterable, editable, card_chip, source}`.
- **Tag colours:** the pills on a card (its key, stage, `card_chip` fields and model) take a colour per field path and value, kept per workspace in the setting `chip_colors:<workspace>` (`{colors: {path: {value: colour}}, plain: [paths]}`). `chip_color_list`, `chip_color_set {path, value, color}` (empty removes it) and `chip_color_auto_set {path, auto}`; sessions may only list. Colours are palette names (grey, blue, teal, green, lime, amber, orange, red, pink, purple) that `tokens.css` defines as `--tag-<name>` and `--tag-<name>-b` for both themes, never raw hex. A value with no colour gets a stable automatic one (FNV-1a of its text over the palette without grey) unless its path is in `plain`; `card.key` is coloured by the key's prefix (ACME for ACME-508) and has no automatic colour. The card panel's tag row and Settings → Tags open a swatch picker on each tag.
- **CEL:** the environment exposes one map variable per namespace (`card`, `custom`, and one per enabled integration), so `jira.status == "Finished"` works. Compile errors are returned verbatim.
- **Rules:**
  - Rules run after every event that matches their trigger, in `position` order.
  - A rule whose action moves a card to the column it is already in is a no-op.
  - Loop guard: each event allows at most 10 rule actions per card. Past that, the rule is logged and skipped.
  - Each firing writes `changes` as `rule:<name>` and a `kind: event` message on the card.
- **Built-in rules** (they can be switched off, but not deleted):
  1. An open question or gate moves the card to the first `needs-you` column.
  2. When the last question is answered or the last gate cleared, the card returns to `return_column_id`.
- **`rule_dry_run`:** returns `[{card, rule, from, to}]` without applying anything.
- **Routines** (Settings → Routines): a prompt typed into the planner or one card's session on a schedule. Cron rules were not enough: they fire once per card, can only notify, tell the planner, move, set a field or run a script action, read the spec in UTC and drop times missed while Hadron was closed.
  - The rule timer (every 30 s) runs each enabled routine whose `next_run_at` has passed, once, then sets the next run after now. A running session gets the prompt typed in (as `session_send`); a stopped one is started with the prompt as its handoff, typed once Claude shows its input box.
  - A run more than two minutes late was missed (Hadron closed, the Mac asleep). With `catch_up` (the default) it runs once, however many times were missed; without, it is skipped and `last_result` says so.
  - At most every 5 minutes: `every_minutes` is at least 5, and a cron spec whose next 200 times ever come closer is refused. At most 50 routines per workspace. Pausing clears `next_run_at`; resuming starts from the next time after now, so paused runs are not caught up.
  - Each run posts a `system` event in the target's chat with the result. Routines are not in the config history (`config_revert` does not restore them).

## 8. Script sandbox (`internal/script`)

An integration is `manifest` + `source`. The manifest's JSON Schema lives in `internal/script/manifest.schema.json`:
- `name`, `version`, `fields` (schema entries without the namespace), `ops` (`search`, `get`, `update`, `actions`, `sessions.*`, `poll`, `discover`), `config` (keys and types), `permissions` (`network: [url patterns]`, `secrets: [names]`, `api: [host method names]`, `exec: [binaries]`), `poll_seconds`.

**Runtime:**
- **Engine:** goja, one runtime per call.
- **Limits:** a 10 s timeout (interrupt), 64 MB of memory via a check on runtime size, no `require`, and no timers beyond the call's lifetime.
- **Host API** (the global `host`; every input and output is validated against a JSON Schema; calls outside `permissions` throw a `PermissionError` naming the missing permission):
  - `http.request({method, url, headers, json, body, auth})`: URL must match a `network` pattern. It returns `{status, headers, json, text}`.
  - `secret(name)`: returns an opaque handle that is valid only as `auth` or a header value. Its value is never returned to JS as a string, and never logged.
  - `mcp.call(server, tool, args)`: calls an MCP server from the user's Claude config. It needs `mcp: <server>` in `network`.
  - `exec(binary, args)`: allowed only for binaries in `exec`; 20 s timeout; returns stdout and stderr.
  - App data: `link.upsert`, `link.list`, `field.set`, `card.read`, `card.move`, `card.list`, `card.create`, `session.list_remote`, `notify`, `log`.
  - `card.create({nick?, key?, title?, column?, item_id?, url?, fields?})`: a card with no session in a column (id, name or role; default the backlog column, else the first active one), linked to `item_id` of this integration. The nick defaults to one made from the key or title (`PROJ-123` → `proj-123`), with `-2`, `-3` … when taken. An item a card already links returns that card with `created: false`. At most 25 per run. It returns `{card_id, nick, created}` and fires `card_created` rules.
- **Discovery** (`ops.discover`, which needs `card.create` in `permissions.api` and a `poll_seconds`): switched on per workspace with `integration_discover_set {name, enabled, column?, max_per_run? (1 to 50, default 10), forget?}` or in Settings → Integrations, and run now with `integration_discover_run`. Every `poll_seconds` the op gets `{limit}` and returns `{items: [{item_id, title, key?, url?, fields?}]}`; Hadron makes a card (no session) for each item that is new, up to the cap, and leaves the rest for the next run. The `discovered` table remembers every item it made a card of, or found already linked, so an item never comes back, even after its card is archived or deleted; `forget: true` clears it. The last run's time, count and error are kept with the setting (`integration_discover:<workspace>:<name>`) and shown in `integration_list` as `discover`. The `jira` template's discover searches `discover_jql` (by default `sprint in openSprints() AND status != Cancelled ORDER BY rank`).
  - `config`: the integration's config values (read-only).
- **Tests:** `integration_test` runs each recorded test in the manifest (`{op, input, http_fixtures, expect}`) with `http.request` answered from the fixtures, and compares the output. Status becomes `pending` until approved. An edit that keeps the same permissions re-tests, and if the tests pass, re-enables.
- **Starter templates** (embedded): `linear` (through `mcp.call` to the Linear MCP), `jira` (REST), `github` (`exec gh`), `empty`.

## 9. Dashboard (`internal/dash`)

- **Block types:** `stats`, `bars`, `table`, `heatmap`, `prose`, `planner`, `activity` and `custom`, following Deputy's `internal/board` validation style. It lists every error, rejects unknown types, requires a source query for data types, and allows heights from 80 to 1200.
- **Named queries:**
  - `cards.count {by: column|<field path>}` returns `groups`;
  - `cards.list {expr?}` returns `rows`;
  - `questions.open` returns `rows`;
  - `mcp.calls {since, by?}` returns `rows` or `groups`;
  - `rules.fired {since}` returns `rows`;
  - integrations may add `<integration>.<query>`.
- **`planner` blocks:** the text is written by asking the planner through `planner_send`, with a generated instruction to call `dash_block_update` with text. That happens on the schedule (`daily HH:MM`, `hourly`) or on refresh. The default dashboard has no `planner` block that runs on a schedule, so it spends no tokens.
- **The default dashboard** matches the prototype.

**Dashboard queries from integrations, and the calendar heatmap.** `ops.queries.<name>` in a manifest and in `index.js` registers the dashboard query `<integration>.<name>` (groups of `{key,label,count}`, optional `ttl_s`). It runs in the sandbox like any op and is served from a per-args cache (default TTL 10 minutes): the first call returns `pending`, an old result is returned at once with `age_s` and `stale` while a background refresh runs, and a failed refresh keeps the last good data and sets `error`. A heatmap block takes `layout` (`strip` or `calendar`; calendar is automatic when every key is YYYY-MM-DD), `weeks` (1 to 53, default 26) and `unit`.

## 10. MCP server (`internal/mcpserver`)

- **Listening:** `127.0.0.1` only, on port `47820` (setting `mcp.port`; `HADRON_MCP_PORT` overrides). If the port is busy, the server starts on a free one and records the port it uses in `~/.hadron/mcp.json` with mode 0600.
- **Auth:** a bearer token in `~/.hadron/mcp-token` (mode 0600), created on first run and compared in constant time. A `Host` header other than `127.0.0.1:<port>` or `localhost:<port>` is rejected.
- **Tools:** one tool per action. Every call writes `mcp_calls`.
- **External clients:** the server info and the README document how to add Hadron to your own Claude config: the same URL, the token, and no session header, so the actor is `external`.

## 11. Frontend

- **Design system:**
  - Copy the tokens and base components from Deputy (`frontend/src/styles/tokens.css`, the components in `App.css` and `v2.css`) into `frontend/src/styles/`. The tokens are the ones the prototype uses.
  - Fonts: Inter Tight for the UI and headings, Unbounded for the wordmark only, and IBM Plex Mono for code and the terminal, bundled locally as woff2 (no CDN). No serif.
  - Icons: inline SVG `Icon` component.
- **Shell:**
  - macOS hidden-inset titlebar (`--wails-draggable: drag`), with traffic lights over the header, as in Deputy and Notted.
  - The layout is the prototype's: header, collapsible planner panel, the main area (Board or Dashboard), the collapsible card panel, modals for New workspace, New session, Settings and the palette, and toasts.
  - **Folders:** the New session form has an optional Folder field (placeholder: the root). The adopt pickers (Bring in a session, Adopt planner conversation) have a folder field, default the root, and an "All folders" switch; each row shows its folder in All folders, and adopting passes the chosen conversation's `cwd`. The card panel shows the card's folder under its name, in mono and shortened with `~`, when it is not the root, and the card menu edits it. A native folder dialog would need a second bound App method, which the parity rule (§3) forbids, so the folder is typed.
  - **Collapsed columns:** a column's header has a collapse button (and the ⋯ menu a Collapse item); `fold` on the column keeps it, so it survives a restart. A collapsed column is a rail: its name, its card count, a `+N` badge for the cards that arrived since it was collapsed (the cards it held then are kept per column in `localStorage`; a column with no record starts with no badge), and a red `?` when one of its cards has an open question. A rail is a drop target. Needs you does not expand by itself when a card arrives: the user chose to collapse it, and moving the board under them would undo that; the red rail and the usual notification carry the signal instead.
- **State:** hooks plus a small store per workspace, refetched on `changed` events. No Redux.
- **Terminal component:** xterm.js plus fit, its theme from tokens, and `PtyAttach` on mount. Typing goes through `PtyWrite` (one write at a time, see §4), and resizes through `PtyResize`.
  - **Keys:** while the terminal has focus every key is the program's: printable characters including `@`, `/`, `~`, `^`, accents and dead keys (through xterm's composition handling), Esc, Shift+Tab, Tab, arrows and Ctrl+letter (Ctrl+V too, so Claude Code reads the clipboard itself). Cmd+letter is left to the app and the webview (palette, new session, copy, paste, select all), and so are the keyboard registry's reserved ⌘ chords (`terminal: true` in `keys.ts`: zones, panels, workspaces, zoom, ⌘Esc to leave). Shift+Enter sends ESC CR, as Option+Enter does, which Claude Code reads as a new line. Shift+Esc leaves full screen. The dispatcher runs nothing else from inside a terminal, so Ctrl+K, Ctrl+N and Esc stay Claude Code's.
  - **Zoom:** the terminal undoes the window's CSS zoom on its host (`zoom: 1/z`) and sets its font to 12px × z instead, then refits; the new size reaches the PTY through `onResize` → `PtyResize`. Glyphs stay sharp and xterm's mouse and size measures stay in plain pixels.
  - **Option as Meta:** a switch in the terminal's tool row sets the global setting `terminal.mac_option_is_meta` (xterm's `macOptionIsMeta`, default off, because some layouts type `@` and other characters with Option).
  - **Attachments:** Cmd+V with an image or PDF on the clipboard keeps it with `attachment_save` and types its path; dropping files types their shell-quoted absolute paths (Wails' `OnFileDrop`, with `DragAndDrop.EnableFileDrop` on), and the tool row's Attach button picks files with an `<input type=file>` (Wails implements the open panel) and types the saved copies' paths. Paths are typed as a paste, so Claude Code attaches an image. No App method was added for this: the parity rule holds.
- **Keyboard (no mouse needed):**
  - **Registry** (`frontend/src/keys.ts`): one line per command with its id, title, group, scope, default chords and flags (`terminal` escapes a focused terminal, `dialog` fires with a dialog open, `input` fires in a text field, `prefix` waits for a second key, `confirm` asks first, `menu` is the native menu it is in). Settings → Keyboard, the `?`/⌘/ cheat sheet, the ⌘K palette, tooltips and the native menus all read it. Rebindings are the setting `keys.bindings` (`{id: [chords]}`, validated by `settings_update`).
  - **Handlers** (`frontend/src/commands.ts`): `runCommand(env, id)` for every registry id; a test checks none is missing. They act on what is rendered: zones are `[data-zone]` (workspaces, planner, board or dashboard, card), cards `[data-card-id]` in `[data-col-id]` columns (so the filter and folds are respected), chat lines `[data-msg]`, dashboard blocks `[data-block]`. A handler returns false when it did nothing, and the key goes on to the page.
  - **Dispatcher** (`state/useKeyboard.ts`): one window keydown listener. Scopes: a dialog lets only `dialog` commands through; a terminal only `terminal` ⌘ chords; a text field only ⌘/⌃ chords (and Esc); board, panel and dashboard single keys only when that zone has the focus. Destructive commands (stop, restart, archive, delete, approve, reject, remove block) open a confirm dialog whose button has the focus.
  - **Focus:** `:focus-visible` draws an accent ring in both themes. `Scrim` (every dialog) traps Tab, closes on Esc, and gives the focus back on close. Popover menus close on Esc and refocus their button.
  - **Native menus** (`internal/app/menu.go`): View, Go and Card list the registry's `menu` commands with their ⌘ chord as accelerator (single-letter keys never become accelerators) and send `menu:command` with the id; the page runs it through the same handler. A chord seen by both the page and the menu runs once (400 ms dedupe). The menus are rebuilt when `keys.bindings` changes. `menu_test.go` checks the table against `keys.ts`. ⌘W hides and ⌘Q quits as before.
  - **Zoom:** ⌘= / ⌘+ in, ⌘- out, ⌘0 reset, steps 70 80 90 100 110 120 130 150 170 200%, a toast says the new level. CSS `zoom` on `.win` scales the whole UI; the value is the setting `ui.zoom` (0.7 to 2), also in Settings → Appearance and the View menu.
- **Tests:** Vitest and Testing Library for the board's column logic, the question card (with notes), the filter bar, and `api.ts` argument shaping.

## 12. Build, test, release

- **Makefile:**
  - `make dev`;
  - `make build`: `wails build -platform darwin/arm64 -ldflags "-X github.com/GabiHert/hadron/internal/run.Version=$(VERSION)"`, then ad hoc signing;
  - `make test`: `go vet`, `go test -race ./...`, then frontend `npm ci && npm run typecheck && npm test`;
  - `make release V=vX.Y.Z`: refuses unless `CHANGELOG.md` has entries under `[Unreleased]` (or a section for the version); after the tests it moves `[Unreleased]` under `## [X.Y.Z] - date`, builds (so the app embeds it), commits `docs: changelog for vX.Y.Z`, tags, pushes, and uses that section as the GitHub release notes. A failed build puts the file back.
- **`release.conf`:** `REPO=GabiHert/hadron`, `GH_ACCOUNT=GabiHert`, `APP_NAME=Hadron`, `BUNDLE_ID=tech.bhlabs.hadron`.
- **Updater:** `internal/update` is wired as in the release-updater skill's `reference.md` → "Wiring (Wails)". Updates appear in Settings → Updates, and the running version shows in the Settings title and Settings → Updates (the header shows no name, mark or version).
- **Changelog:** `CHANGELOG.md` (Keep a Changelog format) is embedded in the app (`internal/changelog`, set from `main.go`). `changelog_get {version?}` returns the running version's notes (a dev build shows Unreleased), the release list, the highlights and whether the user has seen them (`changelog.seen` setting). The "What's new" window opens once after an update, from Help, the ⌘K palette and Settings → Updates; the update-available pill shows highlights in its tooltip. The repository's `CLAUDE.md` makes a changelog line part of every user-visible change.
- **Wails gotchas:** don't run `wails dev` from a folder synced by iCloud. If you hit Wails' known `~/Documents` problem, run `wails build` only. Run `wails generate module` after changing App methods.
