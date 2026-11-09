# Debugging

- `logs_tail {lines, level?, contains?}` reads Hadron's own log (JSON lines). Without debug logging it holds errors and recovered crashes only; `debug_logging_set {enabled: true}` records actions, session starts and exits, MCP requests, chat delivery, rule firings, script runs and update steps. Secrets, tokens and message text are never logged.
- `diagnostics_get` returns the version, macOS version, settings without secrets and the last 300 log lines: attach it to a bug report.
- `mcp_calls` lists recent tool calls with who made them; `rule_log` shows what rules did; `integration_logs` shows script runs; `config_history` shows config changes and `config_revert` undoes one.
- `session_read_screen {card}` shows what a session's terminal shows.
