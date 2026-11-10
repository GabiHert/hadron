# Rules and filters

**Filters** are CEL expressions over fields: `card.assignee == 'You' && jira.status == 'Finished'`. `filter_eval {expr}` returns the matching card ids. Saved filters are views (`view_save`, `view_apply`).

**Rules**: `rule_create {name, trigger, condition?, actions}`. Triggers: `field_changed {field}`, `column_entered {column}`, `card_created`, `report {kind}`, `question_opened`, `question_answered`, `idle_for {minutes}`, `no_report_for {minutes}` (a running session has not reported for that long; text may use `{{quiet.minutes}}`, `{{report.last}}`, `{{report.last_kind}}`, `{{report.last_ago}}`), `cron {spec}`. Actions: `move {role|column_id}`, `set_field {path, value}`, `notify {text}`, `tell_planner {text}`, `run_action {script, action}`. At most 10 rule actions per card per event. Use `rule_dry_run` before enabling, `rule_log` to see what fired.

**Routines** send a prompt on a schedule, to the planner or one card's session; use them instead of a cron rule when the point is to make a session do something. `routine_create {name, cron | every_minutes, target: planner|card, card?, prompt, catch_up?}`: cron is five fields in the Mac's local time (`0 9 * * 1-5`), every_minutes is 5 to 10080, and nothing runs more often than every 5 minutes. A stopped session is started and gets the prompt once Claude is ready. A run missed while Hadron was closed runs once when it opens, or is skipped with `catch_up: false`. `routine_update {routine_id, enabled: false}` pauses one; `routine_run` runs it now; `routine_list` shows the next run and the last result. The user manages them in Settings → Routines. Create one only when the user asks for it.

Two built-in rules move a card to Needs you when a question or gate opens and back when the last one closes.
