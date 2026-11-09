# Integrations

An integration is a manifest plus a JavaScript file that runs in a sandbox and can only do what its manifest permits (network patterns, secrets, binaries, Hadron API calls). Starters: `jira`, `linear`, `github`, `empty`.

Build one: `integration_scaffold {name, template}` then `integration_write_file` for `manifest.json` and `index.js`, `integration_validate`, `integration_test` (recorded tests with fixtures), and leave it for approval (`integration_approve`; the user always may, the planner depends on `planner_policy`). Only the user stores secrets (`integration_secret_set`).

Once enabled, its fields appear as `<integration>.<field>`; `link_add` ties cards to items; `poll_seconds` keeps snapshots fresh; rules can call a script with `run_action`.
