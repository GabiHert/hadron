# Changelog

All notable changes to Hadron are listed here, newest first. Every change a user can notice adds a
line under **Unreleased** in the same commit; `make release` refuses to release with nothing there,
moves the section under the new version, and uses it as the release notes. The app embeds this file
for its "What's new" window.

## [Unreleased]

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
