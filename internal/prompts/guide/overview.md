# Hadron overview

Hadron is a macOS app. A **workspace** has a **board** of columns; every **card** is one Claude Code session that Hadron starts and owns; one **planner** session per workspace is the only one the user talks to.

- The user talks to the planner (planner chat) or about a card (card chat). Hadron types those lines into the planner's terminal, tagged `[planner] ...` or `[KEY nick] ...`.
- Sessions report with `card_report`, ask with `question_ask`, and wait on the user with `gate_set`. A question or gate moves the card to the **Needs you** column until the user answers.
- Every button in the app is also an MCP tool. `app_status` shows the version and tool count; `hadron_guide` explains features.
- Columns have **roles** (needs-you, active, blocked, done, backlog). Rules and moves use roles, never names.
- The user can open any session's Terminal tab and type directly. That bypasses the planner.

Read next: `chat`, `questions`, `reports`, `widgets`.
