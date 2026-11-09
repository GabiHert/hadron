# Hadron: work items

Build them in order. Each WI ends green on `make test`, plus `make build` for any WI that touches the app shell, and as one commit (`feat(<area>): …`).
- **"AC"** is the acceptance criteria.
- **"Proof"** is what the builder reports: the commands it ran and their results.

## M1: Shell, data, updater

**WI-01: Repo skeleton**
- **What:** Wails v2 app `Hadron`, plus the Makefile, `release.conf`, `NOTICE.md`, `README.md`, `.gitignore` (including `build/release/`), `internal/run` and the frontend scaffold (React, TS, Vite, Vitest).
- **AC:**
  - `make test` passes on an empty test suite;
  - `make build` produces `build/bin/Hadron.app`;
  - the app opens a window with the hidden-inset titlebar and the Hadron mark.

**WI-02: Store and migrations**
- **What:** `internal/store` with embedded migrations for every table in spec §2, one repo per aggregate, `HADRON_HOME`, and WAL.
- **AC:** CRUD and migration tests against a temp home pass with `-race`.

**WI-03: Action registry and Invoke**
- **What:** `internal/actions`, with actor, validation, the `changes` and `mcp_calls` writes, and the `changed` event. `App.Invoke` is the only bound method besides the PTY bridge.
- **AC:**
  - a test proves an unknown action and an invalid input return readable errors;
  - a test proves `changes` gets before and after for config actions.

**WI-04: Workspaces, columns, cards**
- **What:** `workspace_*`, `column_*`, `card_*` (sessions not started yet), and seeding of default columns, built-in rules, the default dashboard and prompts.
- **AC:**
  - table-driven tests for every action, including deleting a column, which moves its cards;
  - rename keeps the column id.

**WI-05: Frontend shell and board**
- **What:**
  - design tokens and fonts copied from Deputy;
  - the header with workspace tabs and New workspace;
  - the board with columns: inline rename, the ⋯ menu (role, WIP, reorder, fold, delete), add column, and drag-and-drop cards;
  - a card panel placeholder;
  - the collapsible left and right panels;
  - the theme toggle;
  - ⌘K palette with its basic commands.
- **AC:**
  - it matches the prototype's board;
  - Vitest covers column rename and WIP display;
  - every UI mutation goes through `api.ts`, and so through `Invoke`.

**WI-06: Updater**
- **What:**
  - `internal/update` copied unchanged and wired;
  - Settings → Updates with the running version, and the version in the header;
  - `scripts/release.sh` and `build-release.sh` from the templates;
  - `make release`.
- **AC:**
  - `go test ./internal/update/` passes;
  - `make build` with `VERSION=v0.0.1-test` shows that version in the app (verified from the binary with `strings`, or with `app_status`).

## M2: Sessions and terminals

**WI-07: PTY manager**
- **What:** `internal/pty`: spawn, serialized writes, bracketed paste, resize, ring-buffer scrollback, VT screen snapshot, liveness and exit handling, and the login-shell env.
- **AC:** tests with a fake command cover send, read screen, resize, exit, restart and liveness transitions, with `-race`.

**WI-08: Sessions bound to cards and the planner**
- **What:**
  - `session_*` and `planner_*` actions;
  - launch profiles: three defaults (`brief`, `orchestrate`, `plain`);
  - the MCP config file per session;
  - prompts rendered and passed with `--append-system-prompt`;
  - first-prompt delivery;
  - `card_create` starts its session, and the planner autostarts per workspace.
- **AC:** with a fake `claude` script that echoes its args to a file, tests prove:
  - the exact argv (prompt and MCP config path);
  - the env;
  - first-prompt delivery.

**WI-09: Terminals in the UI**
- **What:**
  - a Terminal component (xterm.js), the Chat ⇄ Terminal switch on the planner panel and the card panel, `PtyAttach` replay, resize, and a direct-input banner;
  - the full-screen toggle on both panels;
  - New session dialog with launch profiles.
- **AC:**
  - `make build`, open the app, create a session with the profile `plain` and command `/bin/zsh`, type `echo hi`, and see the output;
  - the builder records that check in its report with a screenshot path, using `screencapture` on the window.

## M3: MCP

**WI-10: MCP server**
- **What:** `internal/mcpserver`: 127.0.0.1, the token, the Host check, the `X-Hadron-Session` actor mapping, one tool per action, and `mcp_calls`.
- **AC:**
  - a test with the go-sdk client lists the tools, and their count equals the registry's;
  - a call without a token gets a 401;
  - a call from a card's session records the actor `session:<nick>`.

**WI-11: MCP activity in the UI**
- **What:** Settings → MCP server (endpoint, masked token with copy, tool count, live call log), plus the "how to connect your own Claude" snippet.
- **AC:** a Vitest renders the log rows from fixture data.

## M4: Chat, questions, reports

**WI-12: Chat routing**
- **What:** `chat_*`, `card_report`, the tagged lines into the planner PTY, delivery status, and sanitizing.
- **AC:**
  - tests with a fake planner PTY assert the exact bytes typed for card chat, planner chat, reports and answers;
  - an undelivered message records its reason.

**WI-13: Questions and gates**
- **What:**
  - `question_*` and `gate_*`, with Notes on answers;
  - the built-in rules (to Needs you and back);
  - planner-policy enforcement.
- **AC:** tests cover:
  - the move there and back with a custom-named needs-you column;
  - a session answering its own question is refused;
  - the planner answering an approval under `decisions_only` is refused.

**WI-14: Chat UI**
- **What:**
  - the planner chat and the card chat (Conversation tab) with bubbles, reports, events, the typing indicator, and an auto-growing composer;
  - the question card with options, Notes and Answer, and the approval details;
  - the card face with chips, the question excerpt and liveness;
  - full screen for chats.
- **AC:** Vitest for the question card (pick, notes, answer payload) and the composer (Enter sends, Shift+Enter adds a newline).

## M5: Fields, rules, filters, prompts

**WI-15: Field schema and CEL**
- **What:** `internal/fields`, CEL environment, `field_schema_get`, `card_field_set` (custom fields), `filter_eval`.
- **AC:** CEL tests over every namespace, and compile errors surfaced verbatim.

**WI-16: Rules engine**
- **What:** triggers, conditions, actions, the loop guard, `rule_*`, `rule_dry_run` and `rule_log`.
- **AC:** tests cover:
  - "linked `jira.status` becomes Finished moves the card to Finished", with a fake integration field;
  - the loop guard;
  - the dry run applying nothing.

**WI-17: Rules, filters, views and prompts UI**
- **What:**
  - Settings → Rules (builder, list, toggle, dry run) and Settings → Columns;
  - the filter bar (pinned field chips, Customize filters, CEL expression, saved views);
  - Settings → Prompts (editor, variable chips, live preview, reset).
- **AC:** Vitest for the filter bar and the rule builder's payload.

## M6: Integrations, dashboard, widgets

**WI-18: Script sandbox and integrations**
- **What:**
  - `internal/script` (goja, limits, the strict host API, permissions);
  - the manifest schema;
  - `integration_*` and `script_*`;
  - recorded tests;
  - the approval flow under the planner policy;
  - starter templates (linear, jira, github, empty);
  - links: `link_*` and the Links tab with editable fields, and `remote_session_*`.
- **AC:** tests cover:
  - a call outside the permissions throws `PermissionError`;
  - a secret's value never appears in an op's output or the logs;
  - the timeout is enforced;
  - the jira template passes its recorded tests;
  - `card_field_set` on a linked field calls `update`, then runs the rules.

**WI-19: Dashboard and widgets**
- **What:**
  - `internal/dash` (block validation, named queries) and `dash_*`;
  - the Dashboard view with add, resize, move and remove;
  - `planner` blocks through the planner;
  - `widget_post` and `widget_pin`, and widgets rendered in a sandboxed iframe (`sandbox="allow-scripts"` only when the policy allows it, `srcdoc`, a CSP meta tag with no network, and theme tokens injected).
- **AC:**
  - Go tests for validation and every query;
  - a Vitest proves the widget iframe has no `allow-same-origin`.

**WI-20: Polish and release v0.1.0**
- **What:** the README (install, connect your own Claude, MCP tools list), the empty and error states, and `make release V=v0.1.0`, publishing to the public `GabiHert/hadron` repo.
- **AC:**
  - `curl -s https://api.github.com/repos/GabiHert/hadron/releases/latest` lists the zip and `SHA256SUMS`;
  - the app's Check for updates sees v0.1.0 when it runs v0.0.x.

## v0.2.0

**WI-21: Adopt existing Claude sessions**
- **What:**
  - `session_adoptable_list` scans `~/.claude/projects/<root with every non-alphanumeric character replaced by "-">/*.jsonl` (`CLAUDE_CONFIG_DIR` overrides `~/.claude`) and returns the id, the first thing the user typed, the last activity, the size and whether Hadron already holds it; files under 2 KB are skipped;
  - `session_adopt` makes a card and resumes the conversation with `claude --resume`; `planner_adopt` does the same for the planner;
  - the New session dialog has "Bring in a session"; the planner panel menu has "Adopt planner conversation". Both warn that the conversation must be exited elsewhere first.
- **AC:** tests cover the folder-name encoding, the listing (order, size filter, first message), adopting (argv has `--resume <id>` and Hadron's prompt and MCP flags), refusing a conversation another card holds, and the planner variant.

**WI-22: Import a planner folder**
- **What:** `workspace_import_planner_dir` reads a planner run folder (`sessions/*.md`, `log.md`, `questions/<nick>/Q-NN.json`, `gates/<nick>.json`) into cards, messages, questions and gates, as a dry run first; Settings, Import.
- **AC:** a synthetic folder imports to the planned cards, columns, questions and gates, and importing twice changes nothing.

**WI-23: Handoff message**
- **What:** a third and fourth prompt, `handoff` and `handoff_planner`, editable in Settings, Prompts. Hadron types the handoff once after an adopted conversation resumes, and records it as an event on the card.
- **AC:** the fake claude receives the rendered handoff after an adopt; `handoff=false` sends nothing.

**WI-24: Debug logging**
- **What:** the `debug.logging` setting, JSON lines in `$HADRON_HOME/logs` with 7-day retention and a 50 MB cap, error-only when off, `client_log` for frontend errors, Settings, Diagnostics, and `logs_tail`, `diagnostics_get`, `debug_logging_set`.
- **AC:** a planted secret and message text never reach the log; the cap and retention hold.

**WI-25: Feedback inbox**
- **What:** a `feedback` table, `feedback_create`, `feedback_list` and `feedback_update`, a Feedback button in the header, and Settings, Feedback with "Open as GitHub issue".
- **AC:** create, filter and update through the actions; the issue URL is prefilled and fits the URL limit.

**WI-26: Built-in harness and layered prompts**
- **What:** the core planner and session prompts are embedded, versioned and read-only; a session prompt is composed from the core, the workspace prompt, the launch profile's `prompt` and the card's `prompt`, all with `{{variables}}`. New `hadron_guide {topic?}` (twelve embedded topics, with widget HTML that uses the theme's CSS variables), new `prompt_layers_get {kind, card?}`, `prompt_render` returns the composed text, the MCP server `instructions` point at `hadron_guide`. Settings, Prompts becomes Settings, Harness.
- **AC:** tests cover layer order and variable filling in the fake claude's argv, the core surviving `prompt_set` and `prompt_reset`, the card and profile layers, every guide topic and the unknown-topic error, and the MCP instructions. Spec section 6 is updated.

**WI-27: Harness-integration fixes**
- **What:**
  - every claude launch passes `--session-id` (or `--resume`) and stores the conversation id;
  - session-control actions are checked per actor: a card session reads only its own screen and status, the planner drives card sessions but not itself, other workspaces are refused;
  - Hadron's notices to the planner carry a middle dot that nothing a user, session or rule types can contain: `[KEY nick · report]`, `[KEY nick · answer]`, `[rule · name]`;
  - `question_answer` takes `on_behalf_of` and `source_message_id` to relay the user's chat answer (`via: planner-relay`); the `user_only` policy; `gate_withdraw`;
  - the `no_report_for {minutes}` trigger, with `{{quiet.minutes}}` and `{{report.last*}}` in `tell_planner` and `notify` text;
  - `question_ask` takes `meanwhile`, and an approval takes `reversible` (yes, no or partial), `reversible_note` and `command_lang`, shown in the question card;
  - `card_move` keeps `return_column_id` true; a card session may set its own `card.stage`, `custom.*` and linked-item fields.
- **AC:** a test per item, in `internal/actions` and `internal/chat`, plus Vitest for the question card and the rule builder. `docs/mcp-tools.md` is regenerated, and the handoff prompts mention the notice prefixes.

**WI-28: Per-card model**
- **What:** a card, a launch profile and a workspace (`default_model`, and the planner's `planner_model`) can each name a Claude model. A session runs the card's, else its profile's, else the workspace default, else none. `--model` goes on the claude command line, a `models` setting lists what the pickers offer, `card_set_model` and `planner_set_model` type `/model <id>` into a running session, and `card_create` and `card_update` take `model`. The card face shows a chip, the card panel and planner panel have pickers, and Settings has a Models tab.
- **AC:** argv tests prove each step of the resolution order and that nothing is passed when nothing is chosen; a running session receives `/model`; a stopped one applies it at the next start; the event and the `changes` row exist; permissions (a session may not, the planner may for cards but not for itself) and id validation hold; Vitest covers the Models tab, the chip, the pickers and New session. Spec sections 2 and 4 are updated.
