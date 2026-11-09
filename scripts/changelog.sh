#!/usr/bin/env bash
# CHANGELOG.md helper for scripts/release.sh. The file follows Keep a Changelog: a
# "## [Unreleased]" section and one "## [X.Y.Z] - date" section per release, newest first.
#
#   scripts/changelog.sh check <vX.Y.Z> [file]          exit 0 when the version can be released:
#                                                       [Unreleased] has bullets, or the version
#                                                       already has a section with bullets
#   scripts/changelog.sh promote <vX.Y.Z> <date> [file] move [Unreleased] under "## [X.Y.Z] - date"
#                                                       and leave an empty [Unreleased]
#   scripts/changelog.sh notes <vX.Y.Z> [file]          print that version's section body
set -euo pipefail

cmd="${1:-}"
tag="${2:-}"
[ -n "$cmd" ] && [ -n "$tag" ] || {
  echo "usage: changelog.sh check|promote|notes <vX.Y.Z> ..." >&2
  exit 2
}
ver="${tag#v}"

# bullets_of <heading> prints the bullets under "## [heading]" (up to the next "## ").
bullets_of() {
  awk -v want="[$1]" '
    /^## / { on = (index($0, "## " want) == 1); next }
    on && /^[[:space:]]*[-*][[:space:]]+/ { print }
  ' "$file"
}

case "$cmd" in
check)
  file="${3:-CHANGELOG.md}"
  [ -f "$file" ] || { echo "no $file" >&2; exit 1; }
  if [ -n "$(bullets_of Unreleased)" ] || [ -n "$(bullets_of "$ver")" ]; then
    exit 0
  fi
  echo "$file has no entries under [Unreleased] and no [$ver] section." >&2
  echo "Write what changed for users, as '- ...' bullets under ### Added, ### Changed or ### Fixed (see CLAUDE.md), then release again." >&2
  exit 1
  ;;
promote)
  date="${3:-}"
  file="${4:-CHANGELOG.md}"
  [ -n "$date" ] || { echo "usage: changelog.sh promote <vX.Y.Z> <date> [file]" >&2; exit 2; }
  [ -f "$file" ] || { echo "no $file" >&2; exit 1; }
  if [ -n "$(bullets_of "$ver")" ] && [ -z "$(bullets_of Unreleased)" ]; then
    exit 0 # already promoted by hand
  fi
  tmp="$(mktemp)"
  awk -v ver="$ver" -v date="$date" '
    /^## \[Unreleased\]/ { print; print ""; print "## [" ver "] - " date; done = 1; next }
    { print }
    END { if (!done) exit 3 }
  ' "$file" >"$tmp" || { rm -f "$tmp"; echo "$file has no [Unreleased] heading" >&2; exit 1; }
  cat "$tmp" >"$file" # keep the file's own mode
  rm -f "$tmp"
  ;;
notes)
  file="${3:-CHANGELOG.md}"
  awk -v want="[$ver]" '
    /^## / { on = (index($0, "## " want) == 1); next }
    on { print }
  ' "$file" | sed -e :a -e '/^[[:space:]]*$/{$d;N;ba' -e '}' | sed '1{/^[[:space:]]*$/d;}'
  ;;
*)
  echo "unknown command $cmd" >&2
  exit 2
  ;;
esac
