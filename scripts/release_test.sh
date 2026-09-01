#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

remote="$tmpdir/origin.git"
work="$tmpdir/work"

git init --bare --quiet "$remote"
git init --quiet --initial-branch=main "$work"
cp "$repo_root/release.sh" "$work/release.sh"

git -C "$work" config user.name "Release Test"
git -C "$work" config user.email "release-test@example.invalid"
git -C "$work" add release.sh
git -C "$work" commit --quiet -m "initial"
git -C "$work" remote add origin "$remote"
git -C "$work" push --quiet --set-upstream origin main

synced_output=$(cd "$work" && sh ./release.sh --dry-run)
grep -qF "release: v0.0.0 -> v0.1.0" <<<"$synced_output"

printf 'unpushed\n' >"$work/local-only.txt"
git -C "$work" add local-only.txt
git -C "$work" commit --quiet -m "local only"

if ahead_output=$(cd "$work" && sh ./release.sh --dry-run 2>&1); then
  echo "release test: unpushed local commit was accepted" >&2
  exit 1
fi
grep -qF "local main does not match origin/main; push or pull first" <<<"$ahead_output"

echo "release test OK"
