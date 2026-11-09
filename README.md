<p align="center"><img src="docs/logo.svg" alt="Hadron" width="128" height="128"></p>

# Hadron

Hadron is a native macOS app where every card on a board is a Claude Code session.

- **One board per workspace.** You name the columns. A card is one Claude Code session that Hadron starts and owns.
- **One planner per workspace.** You talk to the planner; the planner talks to the sessions, keeps the board true and asks you only when it needs a decision.
- **The real terminal is one switch away.** Every card, and the planner, has a Chat and Terminal switch.
- **Rules, filters, a dashboard and integrations.** Rules move cards when a linked Jira issue finishes, a question opens or a session goes idle. The dashboard counts and charts the board. Integrations link cards to Jira, Linear and GitHub items.
- **Everything you can click is an MCP tool.** Connect your own Claude and it can drive the board the way you do.
- **Local first.** One user, one Mac. Data lives in `~/.hadron`. Hadron updates itself from GitHub Releases.

See `docs/spec.md` for the contracts and `docs/work-items.md` for the build order.

## Install

1. Open the [latest release](https://github.com/GabiHert/hadron/releases/latest) and download `Hadron-<version>-darwin-arm64.zip`.
2. Unzip it and move `Hadron.app` to `/Applications`.
3. Open it. The app is signed ad hoc, so macOS may say it cannot verify the developer: right-click `Hadron.app`, choose Open, then Open again. You only do this once.
4. Optional: check the download against `SHA256SUMS` from the same release: `shasum -a 256 -c SHA256SUMS`.

Requirements: macOS 14 or later on Apple silicon, and the `claude` CLI installed and signed in (Hadron starts it for you).

Later versions install themselves: Hadron checks for a release a minute after launch and every six hours, and **Settings, Updates** downloads it, checks the SHA-256 and the signature, swaps the app and rolls back if the new version does not start. A private repository needs a GitHub token, which you paste into the same card; it goes to your keychain.

## First run

1. **Create a workspace.** Pick a name and the folder your sessions work in. The default columns are Needs you, Working, Blocked, Done and Backlog. Rules use a column's *role*, never its name, so you can rename columns freely.
2. **Start the planner.** It starts with the workspace if `planner.autostart` is on (the default). Switch the planner panel to Terminal if you want to see it.
3. **Add a session.** Press `Cmd N`, give it a nickname (and a key such as `ACME-508` if you track work elsewhere), choose a launch profile and start. The card appears in the column you picked.
4. **Talk.** Type in a card's chat. Hadron sends it to the planner as `[KEY nick] text`; the planner relays it to the session. Sessions report back with progress, questions, blockers and completion, and a question moves its card to Needs you until you answer.
5. **Look around.** `Cmd K` jumps to any card, workspace or command. The Dashboard switch above the board shows counts, who is waiting on you and recent activity. **Settings** holds the columns, rules, prompts, integrations, widgets, the MCP server and updates.

## Connect your own Claude to Hadron

Hadron runs an MCP server on `127.0.0.1` (port `47820`, or a free one if that is busy). Sessions that Hadron starts get it automatically. To drive Hadron from another Claude:

1. Open **Settings, MCP server**. It shows the URL, the bearer token and a ready-made command.
2. Run that command in a terminal, or add the server yourself:

   ```
   claude mcp add --transport http hadron http://127.0.0.1:47820/mcp --header "Authorization: Bearer <token>"
   ```

   The token is in `~/.hadron/mcp-token` (mode 0600). The actual port is in `~/.hadron/mcp.json`.
3. Ask your Claude to call `app_status` or `workspace_list`.

A client with no `X-Hadron-Session` header acts as `external`. The server only answers on localhost, checks the `Host` header and compares the token in constant time. Every call, from the UI as well, is logged in **Settings, MCP server**.

### The tools

Hadron serves well over a hundred tools, one per action (the count is in `app_status` and at the top of that file), so the UI can do nothing that MCP cannot. The full list, with every argument, is in [`docs/mcp-tools.md`](docs/mcp-tools.md). It is generated from the action registry (`go run ./cmd/toolsdoc`), and a test fails when it drifts. In short:

| Group | What it does |
|---|---|
| `workspace_*`, `column_*`, `card_*` | The board: workspaces, columns, cards, moves, nicknames |
| `session_*`, `planner_*` | Start, stop, restart, type into and read the screen of sessions and the planner |
| `chat_*`, `card_report`, `widget_*` | Chat, session reports, HTML widgets |
| `question_*`, `gate_*`, `planner_policy_*` | Questions with options and notes, merge and deploy gates, what the planner may answer |
| `field_*`, `custom_field_*`, `card_field_set`, `filter_*`, `view_*` | Fields, CEL filters, saved views |
| `rule_*` | Rules: triggers, conditions, actions, dry run, log |
| `dash_*` | The dashboard layout and its queries |
| `integration_*`, `script_*`, `link_*`, `remote_session_*` | Integrations, scripts, linked items, cloud agent sessions |
| `prompt_*`, `launch_profile_*`, `worktree_*` | The planner and session prompts, how sessions start, and the git worktrees cards run in |
| `config_history`, `config_diff`, `config_revert` | Who changed the config, what changed, and undo (Settings, History) |

## Worktrees

A launch profile with **worktree** on starts each card in its own git worktree, `<root>/.hadron-worktrees/<nick>` on branch `hadron/<nick>`, when the workspace root is a git repository (otherwise the session starts in the root and the response says so). Deleting a card leaves its worktree on disk so no work is lost; `worktree_list` shows them and `worktree_remove` deletes one, refusing while it has uncommitted changes unless you pass `force`.

## Integrations

An integration links cards to items in another tool (a Jira issue, a Linear issue, a GitHub pull request) and puts that item's fields on the card: `jira.status`, `github.state`. Those fields work in filters, rules and the dashboard, and the editable ones can be changed from the card, which writes them back.

An integration is a **manifest** plus a **JavaScript file** that runs in a sandbox (goja, one fresh runtime per call):

- **It can only do what its manifest permits.** `permissions.network` lists URL patterns, `secrets` the secrets it may use, `exec` the binaries it may run, `api` the Hadron methods it may call. Anything else throws a `PermissionError` that names the missing permission.
- **Secrets stay secret.** The code gets an opaque handle that only works as `auth` or a header value. The value is stored in the macOS login keychain under the service "Hadron secrets", and Hadron removes it from every output, log and error.
- **It is bounded.** 10 seconds of script time per call (waiting on a server does not count), 64 MB of memory, no `require`, no timers. Every input and output is checked against a JSON Schema.
- **It is tested before it runs.** The manifest records tests (an op, an input, the HTTP, command or MCP answers to replay, and the expected output). Approval requires them to pass, and an edit that keeps the same permissions re-tests and stays enabled.
- **Starters:** `jira` (REST), `github` (the `gh` CLI), `linear` (through the Linear MCP server in your Claude config) and `empty`.

To add one: **Settings, Integrations**, pick a starter, enter its settings and secret, review the permissions it asks for, and approve it. Or ask the planner to build one; it writes the files with `integration_scaffold` and `integration_write_file`, runs `integration_test` and leaves it pending for you. The **planner policy** decides whether the planner may approve it itself: under `decisions_only` and `ask_for_prd` it cannot approve new network access, MCP servers or binaries.

Then open a card, **Links**, and link an item. Hadron polls linked items every `poll_seconds` and runs the rules that watch any field that changed. A rule can also call a script with `run_action`.

Linear needs an MCP server in your Claude config that Hadron can reach without an OAuth sign-in (a token header, or a local command). Servers that only sign in through the browser are not supported yet.

## Widgets

The planner and your sessions can post small HTML widgets in a chat (`widget_post`), and you can pin one to the dashboard. A widget renders in a sandboxed frame: no network, no cookies, no access to Hadron, a strict content security policy, and your theme as CSS variables. Scripts inside a widget are off. **Settings, Widgets** turns them on (`widgets.scripts`); only you can.

## Build from source

Requirements: macOS 14+, Go 1.26, Wails v2.12, Node 22+.

```
make test                    # go vet, go test -race, frontend typecheck and tests
make build                   # build/bin/Hadron.app, ad hoc signed
make build VERSION=v0.0.1-test
make dev                     # wails dev (not from an iCloud-synced folder)
go run ./cmd/toolsdoc        # regenerate docs/mcp-tools.md after adding an action
```

`HADRON_HOME` overrides `~/.hadron`; tests always set it to a temp folder. `HADRON_SECRETS=file` keeps integration secrets in a 0600 file under it instead of the keychain (tests use this). `HADRON_MCP_PORT` overrides the MCP port.

## Release

`make release V=v0.1.0` tests, builds, signs, tags and publishes a zip plus `SHA256SUMS` to
`GabiHert/hadron` on GitHub (`release.conf`, publishing as the `GabiHert` gh account). See the release-updater skill.

## Credits

See `NOTICE.md`.
