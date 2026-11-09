# Rules and filters

**Filters** are CEL expressions over fields: `card.assignee == 'You' && jira.status == 'Finished'`. `filter_eval {expr}` returns the matching card ids. Saved filters are views (`view_save`, `view_apply`).

**Rules**: `rule_create {name, trigger, condition?, actions}`. Triggers: `field_changed {field}`, `column_entered {column}`, `card_created`, `report {kind}`, `question_opened`, `question_answered`, `idle_for {minutes}`, `cron {spec}`. Actions: `move {role|column_id}`, `set_field {path, value}`, `notify {text}`, `tell_planner {text}`, `run_action {script, action}`. At most 10 rule actions per card per event. Use `rule_dry_run` before enabling, `rule_log` to see what fired.

Two built-in rules move a card to Needs you when a question or gate opens and back when the last one closes.
