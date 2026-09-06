#!/bin/bash
# sv-memory UserPromptSubmit hook — Claude Code
# Deterministically persists every user prompt via `sv-memory capture prompt`,
# so user intent is recoverable after compaction regardless of the model.
# Fail-open: never blocks or fails the prompt. Always exits 0.
[ -n "$CLAUDE_PROJECT_DIR" ] && cd "$CLAUDE_PROJECT_DIR" 2>/dev/null
SV=sv-memory
command -v "$SV" >/dev/null 2>&1 || exit 0

if [ -n "${PROMPT:-}" ]; then
  "$SV" capture prompt "$PROMPT" >/dev/null 2>&1
fi
exit 0