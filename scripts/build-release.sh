#!/usr/bin/env bash
# Builds a release into build/release: <App>-<tag>-darwin-<arch>.zip and SHA256SUMS. It does not
# publish; scripts/release.sh (make release) calls it. Settings come from release.conf.
# Usage: scripts/build-release.sh v1.2.3
#
# Signing, from the environment:
#   SIGN_IDENTITY   "Developer ID Application: Name (TEAMID)". Unset: ad hoc.
#   NOTARY_KEY_PATH, NOTARY_KEY_ID, NOTARY_ISSUER
#                   An App Store Connect API key (.p8) for notarytool. With these and a Developer
#                   ID, the app is notarized and the ticket stapled.
# From the release-updater skill (~/.claude/skills/release-updater).
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=/dev/null
source ./release.conf
tag="${1:?usage: scripts/build-release.sh vX.Y.Z}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "build-release: '$tag' is not a vX.Y.Z tag" >&2; exit 1; }
VERSION="$tag"
version="${tag#v}"
OUT=build/release
zip_name="$APP_NAME-$tag-darwin-${ARCH:-arm64}.zip"

log() { echo "build-release: $*"; }
die() { echo "build-release: $*" >&2; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT"

log "building $tag"
VERSION="$VERSION" eval "$BUILD_CMD"
[ -x "$APP_PATH/Contents/MacOS/$APP_NAME" ] || die "$APP_PATH/Contents/MacOS/$APP_NAME is missing after the build"
id="$(plutil -extract CFBundleIdentifier raw "$APP_PATH/Contents/Info.plist")"
[ "$id" = "$BUNDLE_ID" ] || die "CFBundleIdentifier is '$id', not '$BUNDLE_ID'"

# The bundle's version is the tag, so Finder and macOS show the release.
plutil -replace CFBundleShortVersionString -string "$version" "$APP_PATH/Contents/Info.plist"
plutil -replace CFBundleVersion -string "$version" "$APP_PATH/Contents/Info.plist"

# Sign inside-out: every nested Mach-O first, then the bundle.
ident="${SIGN_IDENTITY:--}"
opts=()
if [ "$ident" != "-" ]; then
  opts=(--options runtime --timestamp)
  log "signing with $ident (hardened runtime)"
else
  log "SIGN_IDENTITY is not set: signing ad hoc; the release will not be notarized"
fi
while IFS= read -r -d '' f; do
  [ "$f" = "$APP_PATH/Contents/MacOS/$APP_NAME" ] && continue
  if file -b "$f" | grep -q 'Mach-O'; then
    codesign -f -s "$ident" ${opts[@]+"${opts[@]}"} "$f"
  fi
done < <(find "$APP_PATH/Contents" -type f \( -path '*/Frameworks/*' -o -path '*/MacOS/*' -o -path '*/Helpers/*' \) -print0)
ent=()
if [ "$ident" != "-" ] && [ -n "${ENTITLEMENTS:-}" ]; then ent=(--entitlements "$ENTITLEMENTS"); fi
codesign -f -s "$ident" ${opts[@]+"${opts[@]}"} ${ent[@]+"${ent[@]}"} "$APP_PATH"
codesign --verify --deep --strict "$APP_PATH" || die "codesign --verify failed"

# ditto keeps symlinks, permissions and the signature; --keepParent puts <App>.app at the top of
# the zip, which is where the updater looks.
ditto -c -k --keepParent "$APP_PATH" "$OUT/$zip_name"

if [ "$ident" != "-" ] && [ -n "${NOTARY_KEY_PATH:-}" ]; then
  log "notarizing (this takes a few minutes)"
  xcrun notarytool submit "$OUT/$zip_name" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" \
    --issuer "$NOTARY_ISSUER" --wait --timeout 30m
  xcrun stapler staple "$APP_PATH"
  xcrun stapler validate "$APP_PATH"
  spctl --assess --type execute --verbose "$APP_PATH"
  # Zip again so the download carries the stapled ticket and opens offline.
  rm -f "$OUT/$zip_name"
  ditto -c -k --keepParent "$APP_PATH" "$OUT/$zip_name"
elif [ "$ident" != "-" ]; then
  log "NOTARY_KEY_PATH is not set: signed with a Developer ID but not notarized; Gatekeeper will refuse it"
fi

(cd "$OUT" && shasum -a 256 "$zip_name" > SHA256SUMS)
log "done:"
ls -l "$OUT"
cat "$OUT/SHA256SUMS"
