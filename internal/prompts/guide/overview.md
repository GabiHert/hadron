# Hadron overview

Hadron is a macOS app. A **workspace** has a **board** of columns; every **card** is one Claude Code session that Hadron starts and owns; one **planner** session per workspace is the only one the user talks to.

- The user talks to the planner (planner chat) or about a card (card chat). Hadron types those lines into the planner's terminal, tagged `[planner] ...` or `[KEY nick] ...`.
- Sessions report with `card_report`, ask with `question_ask`, and wait on the user with `gate_set`. A question or gate moves the card to the **Needs you** column until the user answers.
- Every button in the app is also an MCP tool. `app_status` shows the version and tool count; `hadron_guide` explains features.
- Columns have **roles** (needs-you, active, blocked, done, backlog). Rules and moves use roles, never names.
- The user can open any session's Terminal tab and type directly. That bypasses the planner.

## Session folders and adoption

- A session runs in the workspace root unless its card has a **folder** (`cwd`): `card_create` and `card_update` take an absolute path to an existing folder (`~` is expanded; empty means the root). A running session keeps its folder until it next starts. A launch profile with a worktree makes it from the card folder's repository.
- The planner's folder is the workspace's `planner_cwd` (`workspace_update`), used by `planner_start`, `planner_restart` and restores.
- `session_adoptable_list` lists the Claude Code conversations that could be brought in: the root's by default, another folder's with `cwd`, or every folder's with `all: true`. Each result's `cwd` is the folder it was run in, read from its transcript.
- `session_adopt` and `planner_adopt` take that `cwd`: the transcript is looked up there and the card, or the planner, runs there from then on. Claude can resume a conversation only in the folder it was run in.

Read next: `chat`, `questions`, `reports`, `widgets`.
