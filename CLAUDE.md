# Working on Hadron

Hadron is a macOS Wails v2 app: a Go backend whose action registry is also the MCP tool list, and a
React and TypeScript frontend. `docs/spec.md` is the design, `docs/work-items.md` the build plan.

## Rules for every change

- **Changelog.** A change a user can notice adds a bullet under `## [Unreleased]` in `CHANGELOG.md`,
  in the same commit, under `### Added`, `### Changed` or `### Fixed`. Write what the user sees, in
  plain words, one line each. `make release` refuses to release when `[Unreleased]` is empty, moves
  it under the new version, and uses it as the release notes. The app embeds the file for its
  "What's new" window. Internal refactors and test-only changes need no entry.
- **One source for the UI and MCP.** Every action registered in `internal/actions` is an MCP tool and
  needs a wrapper in `frontend/src/api.ts`. After adding or changing an action, run
  `go test ./internal/actions -run 'TestActionManifestMatchesGolden|TestMCPToolsDocIsCurrent' -update`
  to regenerate `internal/actions/testdata/actions.json` and `docs/mcp-tools.md`.
- **Tests.** `make test` (Go with `-race`, then the frontend) must pass, and `make build` for anything
  that touches the app shell. Tests use a temporary `HADRON_HOME`, and never run the real `claude`:
  the session tests use a fake one.
- **Public repository.** No real project, company or people names in code, tests, fixtures or docs.
  Use neutral examples such as Acme, Beta, ACME-508 and billing-fix.
- **Commits.** One commit per work item, `type(area): summary`. Releases are cut by the maintainer
  with `make release`; do not tag or push as part of a change.
