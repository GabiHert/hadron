# Changelog

All notable changes to Hadron are listed here, newest first. Every change a user can notice adds a
line under **Unreleased** in the same commit; `make release` refuses to release with nothing there,
moves the section under the new version, and uses it as the release notes. The app embeds this file
for its "What's new" window.

## [Unreleased]

## [0.2.26] - 2026-11-10

### Added
- Colour themes: Settings → Appearance has six new themes (Nucleus, Deep Space, Aurora, Ember, Synthwave and High Contrast) beside Classic light and dark. Follow macOS with one theme for light and one for dark, or always use one. Widgets and terminals follow the theme.
- Make your own theme: duplicate any theme, change its colours with a live preview (or try it on the whole app), then save, rename, export or delete it.
- Import a VS Code colour theme (.json, comments allowed): its editor, button and terminal colours carry over and Hadron fills in the tag colours.
- ⌘K lists "Switch theme" for every theme, and the planner can list, switch, import and delete themes.

## [0.2.25] - 2026-11-10

### Fixed
- An answered question now reaches the card that asked it: the answer is typed into its session as "[· answer] Q-NN: …", whether you answered in the app or the planner answered or relayed it. Before, an idle card never got the answer and waited until someone typed it in by hand. If the card's session is not running, its chat says the answer was not delivered.

## [0.2.24] - 2026-11-10

### Added
- Sounds: a short rising chime when a card moves into Needs you (a question, a gate, or you moved it there) and a soft low note when it moves out, for every workspace and also while the window is hidden. Several cards at once play one sound. Settings → Sounds switches them off, sets the volume, plays each one, and mutes the current workspace. The planner can change them too.

## [0.2.23] - 2026-11-10

### Added
- Card tags have colours. Each value of a field (such as a Jira status), each stage and each model gets a steady colour of its own, and you can pick another from the card panel's tags or in Settings → Tags. Colour a key prefix (such as ACME) to tell projects apart, or switch automatic colours off for a field to keep it grey. The planner can set colours too. Colours follow the theme, so they read well in light and dark.
- The card's stage now shows as a tag on the card face.

### Fixed
- ↩ on a button in the card or planner panel (such as Resume) presses that button instead of jumping to the message box.

## [0.2.22] - 2026-11-10

### Changed
- New typography: Inter Tight for the interface and headings and Unbounded for the Hadron wordmark replace the serif titles and IBM Plex Sans. The terminal and code keep IBM Plex Mono.

## [0.2.21] - 2026-11-10

### Added
- Integrations can fill the board with cards: switch on "Create cards from new items" for an integration in Settings → Integrations, pick the column (Backlog by default) and how many cards a run may add. Each new item becomes a card linked to it, without starting a session, and an item never comes back as a new card, even after you archive its card.
- The Jira starter can create cards for the open sprint's issues (without Cancelled ones); change the search in its settings.
- An integration's code can create a card with host.card.create, once its manifest asks for the card.create permission.

## [0.2.20] - 2026-11-10

### Added
- Routines: in Settings → Routines, have Hadron send a prompt to the planner or a card on a schedule, such as weekdays at 09:00, every day at a time, every N minutes or hours, or a cron spec. A stopped session is started and gets the prompt once Claude is ready. Pause, run now, edit or delete each one, and see its next run and how the last one went. A run missed while Hadron was closed runs once when it opens, unless you turn that off. The planner can manage routines too.

## [0.2.19] - 2026-11-10

### Added
- Collapse a board column to a thin rail with the button in its header (or z on the board). The rail shows the column's name and card count, marks the cards that arrived since you collapsed it, and turns red when a card in it waits on you. Click the rail, or press z or ↩ on it, to expand it again; a collapsed column stays collapsed after a restart. You can drop a card on a rail, and ⌘K has "Expand every collapsed column".

## [0.2.18] - 2026-11-09

### Added
- Settings → Keyboard lists every keyboard shortcut, grouped by where it works, with a search box. Choose Change and press new keys to rebind one; Hadron warns when the keys are taken (and can move them over), refuses macOS's own shortcuts, and resets one or all to the defaults. The planner can change them too, through the keys.bindings setting.
- Hadron works with no mouse. Move between the planner, the board and the card panel with ⌘[ and ⌘] (or ⇧⌘P, ⇧⌘B, ⇧⌘C), switch workspaces with ⌘1–⌘9, collapse panels with ⌥⌘← and ⌥⌘→, switch Chat ⇄ Terminal with ⌘J, full screen with ⇧⌘F and Board ⇄ Dashboard with ⌘D. A focus ring shows where you are.
- On the board, arrows or h j k l move between cards, ↩ opens a card at its message box, ⌥ arrows move the card, n adds a card in that column, s starts, x stops, r restarts and ⌫ archives (asking first), e edits, m changes the model, / searches, and g then a column's number or letter jumps to it.
- In a chat, i writes a message, j and k move between messages, ↩ opens a widget's menu or a question, and Esc steps back. On a question, 1–9 pick an option, n jumps to Notes and ⌘↩ answers; ⇧⌘A and ⇧⌘X approve or reject an approval after a confirm.
- A focused terminal keeps every key for Claude Code except Hadron's ⌘ shortcuts; ⌘Esc leaves the terminal.
- Zoom with ⌘= or ⌘+, ⌘- and ⌘0 (70% to 200%), terminals included. The zoom is kept, and is also in Settings → Appearance and the View menu.
- Press ? (or ⌘/) for a sheet of every shortcut. Tooltips and the new View, Go and Card menus show the shortcuts too.

### Changed
- The command palette (⌘K) lists every command with its shortcut, Go to card, Switch workspace and each Settings section, with your recent picks first.
- Dialogs keep Tab inside them, close with Esc, and put the focus back where it was. Menus close with Esc. Settings sections can be walked with ↑ and ↓.

## [0.2.17] - 2026-11-09

### Added
- A widget's "⋯" menu (it shows when you hover the widget or Tab to it) opens the widget full screen, copies its HTML, or adds it to the dashboard. Esc closes the menu and the full-screen view.
- Widgets without scripts now size themselves: a diagram fits its drawing, and other widgets get a fitting height with a "Show more" link.
- The built-in planner and session instructions now encourage widgets to explain things (comparison tables, before/after, diagrams, timelines, charts, plan checklists, status boards), and the widgets guide has new examples to copy.

### Changed
- Chat widgets no longer show a "Pin to dashboard" button: adding one to the dashboard is in the widget's menu, and the planner does it only when you ask.
- Chat widgets look like part of the conversation: a small caption instead of a title bar (no bar when untitled), bubble width, and the full chat width for diagrams.

## [0.2.16] - 2026-11-09

### Fixed
- Widgets in a long chat showed as an empty line: the scrolling chat log shrank them to their border. Messages and widgets in the log now keep their full height.

## [0.2.15] - 2026-11-09

### Added
- In the card chat and the planner chat, typing `/` at the start opens the session's slash commands: Claude Code's own, the project's and yours in `.claude/commands`, and skills, with descriptions. Arrows, Enter or Tab pick; Esc closes.
- A chat message that starts with `/` (such as `/compact`) goes straight to that session's terminal, not to the planner, and shows in the chat as a "you → terminal" line. If the session is not running, the chat says so and offers Start. Start with `//` to send a literal slash message to the planner.
- Typing `@` in a chat opens the workspace's cards, `@planner`, and the files and folders of the card's folder (or the planner's), ranked as you type. Picking a file inserts `@path`, which Claude Code reads.

## [0.2.14] - 2026-11-09

### Added
- Chat messages, reports, questions and text blocks on the dashboard are shown as Markdown: tables (scrolling sideways when wide), lists, headings, quotes, task lists, code with a Copy button, and links that open in your browser.
- Attach images and PDFs to a chat message: paste, drop or use the clip button. Each shows as a chip before you send, the message carries its path for the session, and the conversation shows a thumbnail.
- In the terminal, paste an image (Cmd+V) or drop files to type their paths, the way Claude Code attaches images, or use the new Attach button. Ctrl+V still goes to Claude Code.
- An "Option as Meta" switch above the terminal, for Claude Code's Option shortcuts.

### Fixed
- Keys typed fast in the terminal could reach Claude Code out of order, garbling commands such as `/model`. Each keystroke now waits for the one before it.
- Shift+Enter in the terminal adds a new line in Claude Code instead of sending the message.
- Ctrl+K, Ctrl+N and Esc in the terminal always go to Claude Code, never to Hadron's shortcuts. Cmd+K and Cmd+N still work there.

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
