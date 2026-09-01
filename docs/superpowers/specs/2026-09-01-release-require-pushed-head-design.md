# Release Must Use Pushed Main Design

## Problem

`release.sh` currently verifies only that `origin/main` is an ancestor of local
`HEAD`. A clean local `main` may therefore contain commits that have not been
pushed. The script can tag that local commit and push the tag, causing GitHub
Actions to release code that is not on the remote `main` branch.

## Approaches Considered

1. Require `HEAD` to equal `origin/main`. This directly expresses that the
   release commit must already be the remote main commit and rejects both ahead
   and behind states.
2. Count commits ahead and fail when the count is nonzero. This addresses the
   immediate bug but retains a separate ancestry check for the behind case.
3. Check that `HEAD` exists anywhere on the remote. This would allow releases
   from other remote branches, conflicting with the script's explicit `main`
   requirement.

Use exact equality because it is the simplest and strongest release invariant.

## Behavior

After fetching tags and remote refs from `origin`, compare the object IDs for
`HEAD` and `origin/main`. If they differ, stop before creating a tag and report
that local `main` must exactly match `origin/main`, instructing the user to push
or pull as appropriate.

The existing clean-tree, branch-name, tag calculation, confirmation, and tag
push behavior remain unchanged.

## Testing

Add shell-level regression coverage that creates isolated local and bare remote
repositories. The test must show that an unpushed commit on local `main` causes
the release script to fail before tagging, while a local `main` equal to
`origin/main` can complete a dry run. Existing verification remains the final
regression check.
