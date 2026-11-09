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
| `workspaces` | `name`, `color`, `root` (absolute folder), `position`, `archived`, `integrations` (JSON array of enabled integration ids), `filter_bar` (JSON array of field paths), `ui` (JSON: collapsed panels, main view), `default_model` and `planner_model` (empty means not chosen) |
| `columns` | `workspace_id`, `name`, `role` (`needs-you`, `active`, `blocked`, `done`, `backlog`), `position`, `wip` (int, 0 means no limit), `fold` (bool), `color` |
| `cards` | `workspace_id`, `column_id`, `position`, `nick` (kebab-case, unique per workspace), `key` (optional, such as `ACME-508`), `title`, `assignee`, `stage`, `liveness` (`working`, `idle`, `waiting`, `ended`), `return_column_id`, `launch_profile`, `prompt` (the card's prompt layer), `model` (empty means not chosen), `fields` (JSON of custom fields), `archived` |
| `sessions` | `card_id` (null for the planner), `workspace_id`, `kind` (`planner`, `card`), `command`, `cwd`, `pid`, `status` (`running`, `exited`), `exit_code`, `claude_session_id` (when known), `started_at`, `ended_at` |
| `messages` | `workspace_id`, `channel` (`planner`, or `card:<cardId>`), `author` (`you`, `planner`, `session`, `rule`, `system`), `kind` (`text`, `report`, `widget`, `event`, `answer`), `report_kind` (`progress`, `question`, `blocked`, `done`), `text`, `widget_title`, `widget_html`, `delivered` (bool), `reason` |
| `questions` | `workspace_id`, `card_id`, `seq` (Q-01, Q-02 per card), `kind` (`decision`, `unblock`, `approval`), `text`, `context`, `options` (JSON `[{label, description}]`), `multi` (bool), `approval` (JSON `{target, runs_as, reversible, command}`), `status` (`open`, `answered`, `withdrawn`), `answer` (JSON `{selections, notes, via}`), `answered_at` |
| `gates` | `workspace_id`, `card_id`, `kind` (`merge`, `promote`, `deploy-confirm`, `close`), `summary`, `since` |
| `links` | `workspace_id`, `card_id`, `integration`, `item_id`, `title`, `url`, `fields` (JSON snapshot), `synced_at` |
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
  - Rules run as `rule:<name>`.
- **Every mutating action:**
  - validates its input, returning errors that say what to fix;
  - writes `changes` when it touches config;
  - writes `mcp_calls` (both UI and MCP calls are logged; the UI rows show `you`);
  - emits a Wails event `changed` with `{workspace_id, entities: [...]}`, so the frontend refetches.

### Action list (v1)

- **Workspaces:** `workspace_list`, `workspace_get`, `workspace_create`, `workspace_update`, `workspace_archive`, `workspace_delete`, `workspace_switch`.
- **Columns:** `column_list`, `column_create`, `column_update` (name, role, wip, fold, color), `column_reorder`, `column_delete` (moves its cards to the first other column).
- **Cards:**
  - `card_list` takes a filter `{column_id?, role?, liveness?, expr?}`.
  - The rest: `card_get`, `card_create` (starts the session unless `start: false`), `card_update`, `card_move`, `card_archive`, `card_delete`.
  - `card_report {kind, text}` may only be called by a card's own session (or `you`).
- **Sessions:** `session_start`, `session_stop`, `session_restart`, `session_send {card, text, enter=true}`, `session_keys {card, keys}`, `session_read_screen {card, lines=60, scrollback=false}`, `session_interrupt`, `session_status`.
- **Planner:** `planner_get`, `planner_start`, `planner_restart`, `planner_send`, `planner_read_screen`.
- **Chat:**
  - `chat_list`, `chat_read {channel, since?}`;
  - `chat_send {channel, text}`, sent as you and routed (§5);
  - `chat_post {channel, text}`, the planner's reply;
  - `widget_post {channel, title, html}`, `widget_pin {message_id}`.
- **Questions and gates:**
  - `question_ask {kind, text, context, options, multi, approval?}`: a session asks about its own card;
  - `question_list`, `question_get`;
  - `question_answer {question_id, selections, notes}`;
  - `question_withdraw`;
  - `gate_set`, `gate_clear`, `gate_list`.
- **Fields and links:** `field_schema_get`, `card_fields_get`, `card_field_set {card, path, value}`, `link_search {integration, q}`, `link_add`, `link_remove`, `link_refresh`, `link_action_run`.
- **Remote sessions:** `remote_session_list`, `remote_session_output`, `remote_session_send`, `remote_session_start`, `remote_session_stop`. These delegate to integrations that declare `sessions`.
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
  - Spawned with `creack/pty` in the workspace root (or the card's worktree), at 120×36 by default and resized by the frontend.
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
  - `PtyWrite(sessionId, base64)` from the frontend.
  - `session_send` writes text with bracketed paste (`ESC[200~ … ESC[201~`), then `\r` when `enter`. Writes to one PTY are serialized through a per-session mutex queue, so lines never interleave.
- **Liveness:** derived as follows, and changes emit `changed`.
  - `working`: output in the last 3 s;
  - `waiting`: the card has an open question;
  - `idle`: neither;
  - `ended`: the process exited.
- **Exit:** the session is marked `exited`. Cards keep their history, and `session_restart` starts again (with `claude --resume <id>`). Every claude launch has an id Hadron knows: a fresh start passes `--session-id <uuid>` and stores it on the session row, so a restart always resumes the same conversation unless `fresh: true`.
- **Planner:** one per workspace, started when the workspace opens if the setting `planner.autostart` is true (the default).
- **Tests:** use a fake command (`/bin/cat`, or a small Go helper binary in `internal/pty/testdata`) instead of `claude`.

## 5. Chat routing (`internal/chat`)

**Channels:** `planner`, and `card:<id>` for each card.
- **`chat_send` as you on `card:X`:**
  1. Store the message.
  2. Type `[<KEY> <nick>] <text>` into the planner PTY, or `[<nick>] <text>` when the card has no key, using bracketed paste and Enter.
  3. Mark the message `delivered`, or record `reason: planner_not_running`.
- **`chat_send` as you on `planner`:** type `[planner] <text>`.
- **Sanitizing:** text is stripped of control characters other than newline and capped at 8,000 characters. A leading `/`, `!` or `#` is impossible, because of the tag.
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
  - **Policy:** the planner may answer approvals only when the planner policy allows it. The default policy, set in v1 settings, is `allow_all`; the other values are `decisions_only`, `ask_for_prd` and `user_only` (the planner answers and clears nothing itself). A session may never answer its own question.
  - **Relay:** the planner may pass `on_behalf_of: user` and `source_message_id` to record an answer the user already gave in chat. The message must be the user's own, in the card's channel or the planner channel, newer than the question. It works under every policy and is stored `via: planner-relay`.
  - **Question fields:** `meanwhile` (what the session does while it waits) and, for approvals, `approval.reversible` (`yes`, `no` or `partial`), `reversible_note` (required for `partial`) and `command_lang`.
- **`gate_withdraw`:** takes back a gate that no longer applies; the session that set it, the planner and the user may. `gate_clear` still means the user did or declined it.

## 6. Prompts (`internal/prompts`)

A session's system prompt is built from **layers**, composed in this order and joined under a `## <title>` header each (empty layers are left out):
1. **Core harness** (`core_planner.md`, `core_session.md`, embedded, versioned by `prompts.CoreVersion`). It is never stored and no action edits it. It teaches the role, the reporting and question protocol, widgets and where to find `hadron_guide`.
2. **Workspace layer:** the `prompts` row for `planner` or `session`. Its default is empty, and a user's older customised row becomes this layer unchanged.
3. **Launch profile layer:** `launch_profiles.prompt` (session kind only).
4. **Card layer:** `cards.prompt` (session kind only).

Every layer supports the variables below. The handoff kinds have no core: they are one text typed once into an adopted conversation. `prompt_layers_get {kind, card?}` returns each layer (template and rendered) and the composed text, and `prompt_render` returns the composed text. `hadron_guide {topic?}` serves twelve embedded topics (`internal/prompts/guide/*.md`: overview, chat, questions, reports, widgets with HTML snippets that use the theme's CSS variables, dashboard, fields-links, rules-filters, integrations, prompts, feedback, debugging), and the MCP server's `instructions` point at it. Settings → Harness shows the core read-only, the workspace layer, and the composed preview.
- **Variables:** `{{workspace.name}}`, `{{workspace.root}}`, `{{columns}}`, `{{integrations}}`, `{{rules}}`, `{{policy}}`, `{{card.nick}}`, `{{card.key}}`, `{{card.links}}`.
- An unknown variable is left as is, and `prompt_set` returns a warning listing it.

## 7. Fields, rules, filters (`internal/fields`, `internal/board`)

- **Field paths:** `card.<name>` (native: `column`, `assignee`, `liveness`, `key`, `nick`, `stage`), `custom.<name>`, and `<integration>.<field>`.
- **Schema entry:** `{path, label, type (text|number|enum|user|date|bool), options?, filterable, editable, card_chip, source}`.
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

## 8. Script sandbox (`internal/script`)

An integration is `manifest` + `source`. The manifest's JSON Schema lives in `internal/script/manifest.schema.json`:
- `name`, `version`, `fields` (schema entries without the namespace), `ops` (`search`, `get`, `update`, `actions`, `sessions.*`, `poll`), `config` (keys and types), `permissions` (`network: [url patterns]`, `secrets: [names]`, `api: [host method names]`, `exec: [binaries]`), `poll_seconds`.

**Runtime:**
- **Engine:** goja, one runtime per call.
- **Limits:** a 10 s timeout (interrupt), 64 MB of memory via a check on runtime size, no `require`, and no timers beyond the call's lifetime.
- **Host API** (the global `host`; every input and output is validated against a JSON Schema; calls outside `permissions` throw a `PermissionError` naming the missing permission):
  - `http.request({method, url, headers, json, body, auth})`: URL must match a `network` pattern. It returns `{status, headers, json, text}`.
  - `secret(name)`: returns an opaque handle that is valid only as `auth` or a header value. Its value is never returned to JS as a string, and never logged.
  - `mcp.call(server, tool, args)`: calls an MCP server from the user's Claude config. It needs `mcp: <server>` in `network`.
  - `exec(binary, args)`: allowed only for binaries in `exec`; 20 s timeout; returns stdout and stderr.
  - App data: `link.upsert`, `link.list`, `field.set`, `card.read`, `card.move`, `card.list`, `session.list_remote`, `notify`, `log`.
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

## 10. MCP server (`internal/mcpserver`)

- **Listening:** `127.0.0.1` only, on port `47820` (setting `mcp.port`; `HADRON_MCP_PORT` overrides). If the port is busy, the server starts on a free one and records the port it uses in `~/.hadron/mcp.json` with mode 0600.
- **Auth:** a bearer token in `~/.hadron/mcp-token` (mode 0600), created on first run and compared in constant time. A `Host` header other than `127.0.0.1:<port>` or `localhost:<port>` is rejected.
- **Tools:** one tool per action. Every call writes `mcp_calls`.
- **External clients:** the server info and the README document how to add Hadron to your own Claude config: the same URL, the token, and no session header, so the actor is `external`.

## 11. Frontend

- **Design system:**
  - Copy the tokens and base components from Deputy (`frontend/src/styles/tokens.css`, the components in `App.css` and `v2.css`) into `frontend/src/styles/`. The tokens are the ones the prototype uses.
  - Fonts: IBM Plex Sans, Newsreader and IBM Plex Mono, bundled locally as woff2 (no CDN), as Deputy does.
  - Icons: inline SVG `Icon` component.
- **Shell:**
  - macOS hidden-inset titlebar (`--wails-draggable: drag`), with traffic lights over the header, as in Deputy and Notted.
  - The layout is the prototype's: header, collapsible planner panel, the main area (Board or Dashboard), the collapsible card panel, modals for New workspace, New session, Settings and the palette, and toasts.
- **State:** hooks plus a small store per workspace, refetched on `changed` events. No Redux.
- **Terminal component:** xterm.js plus fit, its theme from tokens, and `PtyAttach` on mount. Typing goes through `PtyWrite`, and resizes through `PtyResize`.
- **Tests:** Vitest and Testing Library for the board's column logic, the question card (with notes), the filter bar, and `api.ts` argument shaping.

## 12. Build, test, release

- **Makefile:**
  - `make dev`;
  - `make build`: `wails build -platform darwin/arm64 -ldflags "-X github.com/GabiHert/hadron/internal/run.Version=$(VERSION)"`, then ad hoc signing;
  - `make test`: `go vet`, `go test -race ./...`, then frontend `npm ci && npm run typecheck && npm test`;
  - `make release V=vX.Y.Z`.
- **`release.conf`:** `REPO=GabiHert/hadron`, `GH_ACCOUNT=GabiHert`, `APP_NAME=Hadron`, `BUNDLE_ID=tech.bhlabs.hadron`.
- **Updater:** `internal/update` is wired as in the release-updater skill's `reference.md` → "Wiring (Wails)". Updates appear in Settings → Updates, and the running version shows in the header.
- **Wails gotchas:** don't run `wails dev` from a folder synced by iCloud. If you hit Wails' known `~/Documents` problem, run `wails build` only. Run `wails generate module` after changing App methods.
