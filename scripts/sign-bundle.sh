#!/usr/bin/env bash
# Signs the built Hadron.app. Adapted from Deputy's sign-bundle.sh (Hadron has no bundled dylibs).
# With SIGN_IDENTITY set (a "Developer ID Application: ..." name or its SHA-1) it signs with that
# identity, the hardened runtime, a secure timestamp and build/darwin/entitlements.plist, which
# notarization needs. Otherwise it signs ad hoc.
set -euo pipefail
cd "$(dirname "$0")/.."
APP="${1:-build/bin/Hadron.app}"
if [ -n "${SIGN_IDENTITY:-}" ]; then
  echo "sign-bundle: signing with $SIGN_IDENTITY (hardened runtime)"
  codesign -f -s "$SIGN_IDENTITY" --options runtime --timestamp --entitlements build/darwin/entitlements.plist "$APP"
else
  echo "sign-bundle: signing ad hoc"
  codesign -f -s - "$APP"
fi
