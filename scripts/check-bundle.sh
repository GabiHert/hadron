#!/usr/bin/env bash
# Bundle check: the built Hadron.app has its executable, the right bundle id and a valid signature.
set -uo pipefail
cd "$(dirname "$0")/.."
APP="${1:-build/bin/Hadron.app}"
C="$APP/Contents"

fail() {
  echo "check-bundle: FAIL $*"
  exit 1
}

[ -x "$C/MacOS/Hadron" ] || fail "missing Contents/MacOS/Hadron"
codesign --verify --deep --strict "$APP" 2>/dev/null || fail "codesign --verify --deep --strict"
id="$(plutil -extract CFBundleIdentifier raw "$C/Info.plist" 2>/dev/null)"
[ "$id" = "tech.bhlabs.hadron" ] || fail "CFBundleIdentifier is '$id'"

info="$(codesign -dv --verbose=2 "$APP" 2>&1)"
if grep -qx 'Signature=adhoc' <<<"$info"; then
  echo "check-bundle: OK (signed ad hoc)"
elif grep -q '^Authority=Developer ID Application: ' <<<"$info" && grep -q '^TeamIdentifier=[A-Z0-9]' <<<"$info"; then
  echo "check-bundle: OK (signed with Developer ID, team $(sed -n 's/^TeamIdentifier=//p' <<<"$info"))"
else
  fail "Hadron.app is signed neither ad hoc nor with a Developer ID"
fi
