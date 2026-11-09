# Changelog

All notable changes to Hadron are listed here, newest first. Every change a user can notice adds a
line under **Unreleased** in the same commit; `make release` refuses to release with nothing there,
moves the section under the new version, and uses it as the release notes. The app embeds this file
for its "What's new" window.

## [Unreleased]

## [0.2.13] - 2026-11-09

### Fixed

- After Hadron crashed, a Claude Code process it had started could still be running, and restoring that session started a second process on the same conversation. At startup Hadron now stops such leftover processes (only ones whose command line is that session's) before it restores sessions.

## [0.2.12] - 2026-11-09

### Fixed
- Opening Hadron a second time, or building it while it runs, no longer marks the running app's sessions as stopped. Only the first instance opens the database and reconciles sessions, and a session whose process is still alive is never marked exited.
- Integrations that run a command-line tool (such as gh from Homebrew) failed inside the installed app with "executable file not found in $PATH": the tool is now found on your login shell's PATH.

## [0.2.11] - 2026-11-09

### Added

- Every action can be done over MCP, including the user-only ones. The new setting `mcp.user_actions` (Settings, MCP server) chooses who may call them: `external` (default) lets your own MCP clients, `all` also the planner and card sessions, `off` only the app. Each such call is written to the config history with the actor. New action `planner_keys` presses keys in the planner's terminal.

## [0.2.10] - 2026-11-09

### Added
- Cards and the planner can run in their own folder, so you can adopt a conversation from any folder: pick a folder or "All folders" when you bring in a session, and set a card's folder when you create it or from its menu.

## [0.2.9] - 2026-11-09

### Changed

- A script-defined dashboard query can declare `timeout_s` (1 to 180 seconds, default 10) under `ops.queries.<name>` in its manifest, so a query that pages through a large API has time to finish; a run that times out keeps the last good data and shows the error.

### Fixed
- The planner panel title was cut to "P..." when the panel was narrow. "Planner" now always fits: the Chat and Terminal switch drops its icons, and the buttons wrap to a second row when there is still no room.

## [0.2.8] - 2026-11-09

### Added
- A calendar layout for heatmap blocks: a contributions-style grid with one square per day, weeks as columns, month labels, a five-step colour scale and a Less/More legend. It is chosen automatically when every group is a date (`YYYY-MM-DD`), or set with `layout: "calendar"`, with `weeks` (default 26) and `unit` options and a total line such as "42 deploys in the last 26 weeks".
- Integrations can define their own dashboard queries (`ops.queries`), such as a repository's successful workflow runs per day. The data is fetched in the background and cached, so the dashboard never waits for the network; it shows how old the data is, and the error of the last refresh next to the last good data.

## [0.2.7] - 2026-11-09

### Changed
- ⌘W and the window's close button now hide Hadron instead of quitting it, so your sessions keep running. Click Hadron in the Dock to bring it back. ⌘Q still quits.

## [0.2.6] - 2026-11-09

### Changed
- Clicking a card (on the board, in the dashboard, or from the command palette) opens the card panel again if you had collapsed it. Background refreshes still leave it collapsed.

## [0.2.5] - 2026-11-09

### Added
- Running sessions come back after Hadron restarts, updates or crashes: the planner first, then the cards, one second apart, each resuming its conversation. A session you stopped yourself, or ended with /exit, stays stopped. Turn it off in Settings, General. Settings, Updates tells you how many sessions will resume before you install.

## [0.2.4] - 2026-11-09

### Fixed
- Starting or restarting a session no longer fails with "No conversation found with session ID" when the previous run never got as far as a first message. Hadron resumes a conversation only when it exists, and otherwise starts it again under the same id. Bringing in a conversation that has no transcript now says where it looked.

## [0.2.3] - 2026-11-09

### Fixed
- The collapse button of the card panel (the right-hand chat panel) did nothing while a card was open. It now collapses and stays collapsed.

## [0.2.2] - 2026-11-09

### Added
- Settings, Planner (per workspace): choose the planner's launch profile (with the exact command and arguments it will run shown), its policy (with a one-line explanation of each and "inherit the global policy"), its model, whether it starts when Hadron opens, and adopt a conversation, start, restart or stop it. "Bring in a session" and "Adopt planner conversation" offer a launch profile too.
- `planner_stop` stops the planner's session.

### Fixed
- Importing a large planner folder could freeze the whole app, including every MCP call. The import now reads the folder without holding any database lock, writes in one short transaction, stops at a time budget (`timeout_seconds`, default 120), and logs its progress when debug logging is on. A slow or stuck write no longer blocks reads: the database uses a small connection pool, and the call log never delays an answer.

## [0.2.1] - 2026-11-09

### Fixed
- Adopted sessions and the planner can now start with a launch profile, so arguments such as `--dangerously-skip-permissions`, the model and the environment apply when they resume. `session_adopt` and `planner_adopt` take `launch_profile`, and `planner_set_profile` sets the planner's. The model setting is no longer passed twice when a profile's arguments also name one.

## [0.2.0] - 2026-11-09

### Added
- Bring in a Claude Code conversation you started elsewhere: New session, "Bring in a session", and "Adopt planner conversation" in the planner menu. Hadron resumes it with its prompt and tools attached and can type a short handoff message.
- Import a planner run folder into cards, messages, questions and gates, with a dry run first (Settings, Import).
- Debug logging and diagnostics (Settings, Diagnostics): switch on detailed logs, tail them, and copy a diagnostics report. Secrets and message text are never logged.
- A feedback inbox: the Feedback button, the planner's "feedback: ..." shortcut, and Settings, Feedback with "Open as GitHub issue".
- A built-in Hadron harness. Every session now starts from instructions that teach it the board, chat, questions, reports and widgets, with your workspace prompt, the launch profile's prompt and the card's prompt added on top (Settings, Harness shows them composed). Sessions can call `hadron_guide` to read how any part of Hadron works.
- A model per card: pick Opus, Sonnet, Haiku or any model id on a card, on a launch profile, or as the workspace default, and set the planner's. A running session is switched with `/model`. Settings, Models edits the list.
- Planner policy per workspace, with the global policy as the fallback, and a new `user_only` policy: the planner answers and clears nothing itself and only relays what you said in chat.
- The planner can relay your answer from chat (`question_answer` with `on_behalf_of`), and `gate_withdraw` takes back a gate that no longer applies.
- The `no_report_for` rule trigger fires when a running session goes quiet, with the last report in the message.
- Questions can say what the session does meanwhile, and approvals say whether they can be undone (yes, no or partly), how, and show the command with its language.
- Two workspaces can use the same root folder; their card worktrees no longer collide.
- This changelog, and a "What's new" window: after an update, from the Help menu, and from Settings, Updates. The update-available pill in the header shows the highlights.

### Changed
- The header no longer shows the Hadron name, mark or version. The version is in the Settings title and in Settings, Updates.
- Notices Hadron types into the planner now carry a middle dot that nothing a user, session or rule types can contain: `[KEY nick · report]`, `[KEY nick · answer]`, `[rule · name]`. A line without the dot is you typing.
- Every Claude session is launched with a conversation id Hadron stores, so a restart always resumes the same conversation.
- A card session can read only its own screen and status; the planner drives card sessions but not itself.
- A card session can set its own stage, custom fields and linked-item fields, and nothing else on the card.
- Moving a waiting card by hand keeps its return column true.
- Settings, Prompts is now Settings, Harness: the workspace prompt defaults to empty, and the built-in harness sits underneath it.

## [0.1.0] - 2026-11-08

### Added
- The first release: workspaces with a board of columns, where every card is a Claude Code session and one planner per workspace is the session you talk to.
- Chat with the planner and per card, questions with options and notes, approvals, gates, reports, and widgets in a sandboxed frame.
- Fields, filters and rules with a dry run; integrations written as sandboxed scripts; a dashboard of blocks.
- An MCP server with one tool per action, so any Claude can drive Hadron.
- Config history with revert, git worktrees per card, and self-update from GitHub Releases.
