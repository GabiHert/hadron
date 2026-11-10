# Keyboard

The user can run Hadron with no mouse. Every shortcut lives in one registry (the frontend's `keys.ts`); Settings → Keyboard lists it, `?` or ⌘/ shows a cheat sheet, and ⌘K finds any command with its chord. When the user asks "how do I … with the keyboard", answer from the list below or point them to `?`.

**Where keys work (scopes).**
- *Everywhere:* ⌘ chords, also inside a text field.
- *Board, panel, dashboard:* single keys (j, k, n, s, 1–9 …) only when that zone has the focus and no text field does.
- *Terminal:* every key is the program's (Claude Code gets Esc, Ctrl+C, arrows …). Only the reserved ⌘ chords escape: zones, panels, workspaces, zoom, the palette, and ⌘Esc (or ⇧⌘↑) to leave the terminal.
- *Dialogs:* Tab stays inside, Esc closes, the focus goes back to where it was.

**Defaults.**
- Zones: ⌘[ ⌘] cycle planner, board, card panel; ⇧⌘P planner, ⇧⌘B board, ⇧⌘C card panel, ⇧⌘W workspace bar; ⌘1–⌘9 workspaces; ⌥⌘← ⌥⌘→ collapse or expand the planner or the card panel; ⌘J Chat ⇄ Terminal; ⇧⌘F full screen; ⌘D Board ⇄ Dashboard.
- Board: arrows or h j k l move; ↩ opens the card at its composer; Space shows it without moving the focus; ⌥ arrows or ⇧H ⇧J ⇧K ⇧L move the card; n new card in that column; s start, x stop, r restart, ⌫ archive (the last three ask first); e edit, m model; / or ⌘F search; g then a column's number or first letter jumps there; z collapses the column to a rail or expands it (h and l skip a collapsed column; g lands on its rail).
- Panels: i writes a message; j k walk the messages; ↩ acts on the focused one (widget menu, link, question); Esc steps back out. ⌥⌘1–3 open Conversation, Links, Context.
- Questions: 1–9 pick an option, n goes to Notes, ⌘↩ answers. ⇧⌘A approves and ⇧⌘X rejects the open approval, after a confirm.
- Zoom: ⌘= (or ⌘+) in, ⌘- out, ⌘0 back to 100%, from 70% to 200%; it scales the window and the terminals.

**Settings you can change** with settings_update (the user's choice; do not change them unasked):
- `keys.bindings`: `{command id: [chords]}` for the commands the user rebound; `[]` removes a shortcut, a missing id keeps its default. Chords put modifiers in the order Ctrl, Alt, Shift, Mod (⌘): `Mod+D`, `Shift+Mod+P`, `Alt+ArrowRight`, `G`, `?`. Command ids look like `view.dashboard`, `card.start`, `zone.next`; Settings → Keyboard shows them all. The native View, Go and Card menus follow the bindings.
- `ui.zoom`: a number from 0.7 to 2 (1 is 100%).
