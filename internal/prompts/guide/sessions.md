# Sessions space

The Sessions space is Hadron's own terminal app: free sessions that belong to no card. The user opens it with the **Sessions** tab next to Board and Dashboard (⇧⌘S), or from anywhere with ⌘T (a new terminal) and ⌥⌘T (a new claude). The **Sessions** menu in the menu bar does the same.

**What a free session is.**
- *kind shell* (the default): the user's login shell (`$SHELL -l`) with their full environment, in a folder. *kind claude*: plain `claude` with none of Hadron's prompt or tools.
- Global: free sessions do not belong to a workspace and keep running when the user switches workspaces or views. A workspace is only an optional tag (`workspace_id`).
- They come back after Hadron restarts, crashes or updates: a shell starts again in the folder it was last in, and a claude resumes its conversation. A shell that exits cleanly (`exit`) closes its tab, as in a terminal app. Anything else that ends stays, ended, until it is started again or closed.
- Without a folder, a new one starts where the last one did, else in the home folder. From the space, ⌘T opens in the current terminal's folder.

**Tabs and splits** are the user's layout: tabs hold panes split right (⌘D) or down (⇧⌘D). The space has normal terminal keys: ⌘K clears, ⌘F finds in the scrollback, ⌘C and ⌘V copy and paste, links are clickable, and the tab's title follows the program running in it (vim, htop) or the title it sets. The full list is under "Sessions" in Settings → Keyboard.

**Tools.** They are for the user and the planner; card sessions cannot use them, because these are the user's own shells.
- `free_session_start {kind?, cwd?, name?, workspace_id?}` and `free_session_list {workspace_id?}` (each with `running`, `liveness`, `program` in the foreground and the current `cwd`).
- `free_session_send {session, text?, enter?, keys?}`, `free_session_read_screen {session, lines?, scrollback?}`. `session` is the id, or the name when only one session has it.
- `free_session_update {session, name?, workspace_id?}` renames or retags; `free_session_stop {session, close?}` hangs up (close also removes it); `free_session_restart {session}`.
- `free_session_to_card {session, nick, workspace_id?, key?, column_id?}` makes a card of a free session **without stopping it**: the card takes over the running process, screen and output. A claude keeps its conversation; it gets Hadron's card prompt and tools at the card's next restart (`session_restart` resumes it). A card made of a shell runs its launch profile (claude) when restarted.
- `ui_main_view {view: "sessions"}` shows the space.

Never type into a user's shell unasked: a free session is the user's own terminal.
