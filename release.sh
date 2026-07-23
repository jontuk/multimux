#!/bin/sh
# multimux release — bump the minor version, tag, and push.
#
# Pushing a v* tag triggers .github/workflows/release.yml, which runs goreleaser
# and publishes the release archives. This script only creates and pushes the tag.
#
#   ./release.sh            # v0.2.0 -> v0.3.0, prompts before pushing
#   ./release.sh --yes      # skip the confirmation prompt
#   ./release.sh --dry-run  # show the next tag and exit
set -eu

BRANCH="main"
ASSUME_YES=0
DRY_RUN=0

for arg in "$@"; do
  case "$arg" in
    -y|--yes) ASSUME_YES=1 ;;
    -n|--dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "release: unknown option: $arg" >&2; exit 2 ;;
  esac
done

err() { echo "release: $*" >&2; exit 1; }

# --- sanity checks -----------------------------------------------------------
git rev-parse --git-dir >/dev/null 2>&1 || err "not a git repository"

current="$(git rev-parse --abbrev-ref HEAD)"
[ "$current" = "$BRANCH" ] || err "on branch '$current', expected '$BRANCH'"

[ -z "$(git status --porcelain)" ] || err "working tree not clean; commit or stash first"

# Make sure local tags and main match the remote before computing the next tag.
echo "release: fetching from origin..."
git fetch --quiet --tags origin

if ! git merge-base --is-ancestor origin/"$BRANCH" HEAD; then
  err "local $BRANCH is behind origin/$BRANCH; pull first"
fi

# --- compute next minor tag --------------------------------------------------
latest="$(git tag -l 'v*' --sort=-v:refname | head -n1)"
[ -n "$latest" ] || latest="v0.0.0"

ver="${latest#v}"
major="${ver%%.*}"
rest="${ver#*.}"
minor="${rest%%.*}"

case "$major" in *[!0-9]*|"") err "cannot parse major from '$latest'" ;; esac
case "$minor" in *[!0-9]*|"") err "cannot parse minor from '$latest'" ;; esac

next="v${major}.$((minor + 1)).0"

if git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
  err "tag $next already exists"
fi

echo "release: $latest -> $next"

if [ "$DRY_RUN" -eq 1 ]; then
  exit 0
fi

# --- confirm, tag, push ------------------------------------------------------
if [ "$ASSUME_YES" -ne 1 ]; then
  printf "release: tag and push %s? [y/N] " "$next"
  read -r reply </dev/tty || reply=""
  case "$reply" in
    y|Y|yes|YES) ;;
    *) echo "release: aborted"; exit 1 ;;
  esac
fi

git tag -a "$next" -m "Release $next"
git push origin "$next"

echo "release: pushed $next"

repo_url="$(git remote get-url origin 2>/dev/null | sed 's#git@github.com:#https://github.com/#; s#\.git$##')"
case "$repo_url" in
  https://github.com/*) echo "release: watch  ${repo_url}/actions" ;;
esac
