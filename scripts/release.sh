#!/usr/bin/env bash
# Builds and publishes a release from this Mac. No CI is involved. Settings come from release.conf.
# Usage: make release              next patch version after the newest tag (v0.2.1 → v0.2.2)
#        make release V=v0.3.0     a version you choose
#
# Steps, stopping at the first failure:
#   1. Checks: on $BRANCH, nothing uncommitted, not behind origin, the tag and release are new,
#      gh is signed in as $GH_ACCOUNT.
#   2. Changelog: CHANGELOG.md must have entries under [Unreleased] (or a section for the version).
#      The release is refused without them.
#   3. Tests: $TEST_CMD.
#   4. Changelog promoted: [Unreleased] moves under "## [X.Y.Z] - date", so the build embeds it.
#   5. Build: scripts/build-release.sh (zip + SHA256SUMS; signing from the environment).
#   6. Commit the changelog, tag, push the branch and the tag, then create the GitHub Release with
#      both files, using the changelog section as the release notes.
# A failure before step 6 leaves no tag, push or release behind, and CHANGELOG.md is put back.
# From the release-updater skill (~/.claude/skills/release-updater).
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=/dev/null
source ./release.conf

log() { echo "release: $*"; }
die() {
  echo "release: $*" >&2
  exit 1
}

export GH_TOKEN
GH_TOKEN="$(gh auth token -u "$GH_ACCOUNT" 2>/dev/null)" || die "gh is not signed in as $GH_ACCOUNT. Run: gh auth login (as $GH_ACCOUNT)"

git fetch -q --tags origin "$BRANCH"
latest="$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)"
tag="${1:-}"
if [ -z "$tag" ]; then
  [ -n "$latest" ] || die "there is no vX.Y.Z tag yet; run: make release V=v0.1.0"
  IFS=. read -r major minor patch <<<"${latest#v}"
  tag="v$major.$minor.$((patch + 1))"
fi
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "'$tag' is not a vX.Y.Z version"

log "releasing $tag (newest tag: ${latest:-none})"
[ "$(git branch --show-current)" = "$BRANCH" ] || die "switch to $BRANCH first"
[ -z "$(git status --porcelain)" ] || die "commit or stash your changes first:
$(git status --short)"
git merge-base --is-ancestor "origin/$BRANCH" HEAD || die "$BRANCH is behind origin/$BRANCH; pull first"
! git rev-parse -q --verify "refs/tags/$tag" >/dev/null || die "tag $tag already exists"
! gh release view "$tag" --repo "$REPO" >/dev/null 2>&1 || die "release $tag already exists on GitHub"

bash scripts/changelog.sh check "$tag" || die "CHANGELOG.md is not ready for $tag (see above)"

if [ -n "${TEST_CMD:-}" ]; then
  log "testing"
  eval "$TEST_CMD"
fi

# Move [Unreleased] under the new version before the build, so the app embeds its own notes. Put
# the file back if anything fails before the commit.
cl_backup="$(mktemp)"
cp CHANGELOG.md "$cl_backup"
restore_changelog() {
  if [ -n "${cl_backup:-}" ]; then
    cp "$cl_backup" CHANGELOG.md
    rm -f "$cl_backup"
  fi
}
trap restore_changelog EXIT
bash scripts/changelog.sh promote "$tag" "$(date +%Y-%m-%d)"
notes_file="$(mktemp)"
bash scripts/changelog.sh notes "$tag" >"$notes_file"
[ -s "$notes_file" ] || die "CHANGELOG.md has no notes for $tag after promoting [Unreleased]"

bash scripts/build-release.sh "$tag"

log "committing the changelog, tagging and pushing $tag"
if ! git diff --quiet -- CHANGELOG.md; then
  git add CHANGELOG.md
  git commit -q -m "docs: changelog for $tag"
fi
rm -f "$cl_backup"
cl_backup=""
git tag -a "$tag" -m "$APP_NAME $tag"
git push -q origin "$BRANCH"
git push -q origin "$tag"

notes="Built and signed on the developer's Mac with make release."
if ! codesign -dvv "$APP_PATH" 2>&1 | grep -q '^TeamIdentifier=[A-Z0-9]'; then
  notes="$notes Signed ad hoc and not notarized."
fi
prerelease=()
case "$tag" in *-*) prerelease=(--prerelease) ;; esac
{
  cat "$notes_file"
  printf '\n---\n%s\n' "$notes"
} >"$notes_file.full"
gh release create "$tag" build/release/* --repo "$REPO" --title "$APP_NAME $tag" --verify-tag \
  --notes-file "$notes_file.full" ${prerelease[@]+"${prerelease[@]}"}
rm -f "$notes_file" "$notes_file.full"
log "published https://github.com/$REPO/releases/tag/$tag"
