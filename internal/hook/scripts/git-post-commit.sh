#!/bin/sh
# sv-memory post-commit hook: capture commit metadata as passive memory
# Fail-open: if sv-memory is missing or uninitialized, exit 0 cleanly.

# 1. Guard: Check if .sv-memory directory exists in repository root
if [ ! -d ".sv-memory" ]; then
    exit 0
fi

# 2. Check if sv-memory binary is in PATH or ~/.local/bin
SV_BIN=$(which sv-memory 2>/dev/null)
if [ -z "$SV_BIN" ]; then
    if [ -f "$HOME/.local/bin/sv-memory" ]; then
        SV_BIN="$HOME/.local/bin/sv-memory"
    else
        exit 0
    fi
fi

# 3. Extract commit information
COMMIT_HASH=$(git log -1 --format=%H 2>/dev/null)
COMMIT_MSG=$(git log -1 --format=%s 2>/dev/null)
COMMIT_AUTHOR=$(git log -1 --format=%an 2>/dev/null)
COMMIT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
CHANGED_FILES=$(git diff-tree --no-commit-id --name-only -r HEAD 2>/dev/null | head -n 10 | tr '\n' ',' | sed 's/,$//')

if [ -z "$COMMIT_HASH" ] || [ -z "$COMMIT_MSG" ]; then
    exit 0
fi

# 4. Trigger sv-memory capture in background/subshell so git commit is never delayed
"$SV_BIN" capture --commit "$COMMIT_HASH" --message "$COMMIT_MSG" --author "$COMMIT_AUTHOR" --branch "$COMMIT_BRANCH" --paths "$CHANGED_FILES" >/dev/null 2>&1 || true

exit 0
