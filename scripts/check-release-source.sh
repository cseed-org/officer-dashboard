#!/bin/sh
# The caller fetches origin/main before checking a push's tagged commit.
set -eu
: "${GITHUB_SHA:?GITHUB_SHA must identify the tagged commit}"
if ! git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main; then
  echo '::error::Release tags must point to a commit reachable from origin/main.' >&2
  exit 1
fi
