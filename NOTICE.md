# Notices

Hadron adapts code and design from three projects by the same author or under the MIT License:

- **quant** (github.com/brienze1/quant), MIT, Copyright (c) 2026 Luis Brienze. Hadron's PTY session
  handling, login-shell environment capture and terminal ideas come from quant. Hadron never
  imports quant's packages.
- **Deputy** (same author as Hadron), which copies and modifies code from Notted and quant.
  Hadron copies Deputy's build scripts, Wails bootstrap, SQLite store setup, design tokens, fonts
  and icon set.
- **Notted** (same author as Hadron), the source of the Ledger design system that Deputy and
  Hadron share.

## quant license

MIT License

Copyright (c) 2026 Luis Brienze

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Copied files

| Hadron file | Deputy source | Changes |
|---|---|---|
| `Makefile` | `Makefile` | Hadron paths and module; no dylib fixup; `test` runs `-race` |
| `scripts/sign-bundle.sh` | `scripts/sign-bundle.sh` | no bundled dylibs; no local signing identity; `SIGN_IDENTITY` |
| `scripts/check-bundle.sh` | `scripts/check-bundle.sh` | no helper or speech-library checks |
| `build/darwin/Info.plist`, `Info.dev.plist` | same | Hadron bundle id; microphone text removed |
| `internal/app/run.go` | `internal/run/run.go` | window options and single-instance lock only |
| `internal/pty/env.go` | `internal/claude/env.go` (from quant `job_manager.go`) | Hadron marker; only LoginShellEnv and LookPath |
| `internal/pty/decmodes.go` | quant `internal/integration/process/decmodes.go` | streaming scanner kept across chunks; replay preamble reads the live tracker |
| `internal/pty/session.go` (input framing) | quant `internal/integration/process/input.go` | bracketed paste only when the child enabled the mode; submit delay after a paste |
| `internal/store/store.go` | embedded versioned SQL migrations replace the inline schema |
| `frontend/src/styles/tokens.css`, `fonts.css` | `frontend/src/styles/` | prototype token names; fonts replaced |
| `frontend/src/components/ui.tsx` | `frontend/src/components/ui.tsx` | Hadron mark and the icons the board uses |

## Code from the release-updater skill

`internal/update/` is copied unchanged from `~/.claude/skills/release-updater/templates/update/`.
`scripts/release.sh`, `scripts/build-release.sh` and the Updates card
(`frontend/src/components/UpdatesCard.tsx`, restyled) come from the same templates.

## Fonts

Inter Tight, Unbounded and IBM Plex Mono are bundled under the SIL Open Font License 1.1; see
`frontend/src/assets/fonts/OFL-*.txt`. The woff2 files come from the npm packages
`@fontsource-variable/inter-tight` 5.3.0, `@fontsource/unbounded` 5.3.0 and
`@fontsource/ibm-plex-mono` 5.3.0, fetched with `npm pack`. The packages are not dependencies, and
nothing is fetched at runtime.
