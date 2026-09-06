#!/bin/bash
# sv-memory SessionStart hook — Claude Code
# Deterministically starts the sv-memory session (when none is active) and
# injects the Auto-Boot Context Bundle as additionalContext, so ANY model
# receives prior context without needing to call sv_mem_session_start. When a
# session is already active it injects a short note instead (no double-start).
# Fail-open: no output is emitted when sv-memory is unavailable or the project
# is uninitialized. Always exits 0.
SV=sv-memory
command -v "$SV" >/dev/null 2>&1 || exit 0

ACTIVE=$("$SV" session active 2>/dev/null)
if [ -z "$ACTIVE" ] || [ "$ACTIVE" = "none" ]; then
  OUT=$("$SV" session start 2>/dev/null)
  if [ -n "$OUT" ]; then
    CONTEXT="Session is auto-managed by the sv-memory hook — do not call sv_mem_session_start. Use sv_mem_session_end when done.

$OUT"
  else
    CONTEXT=""
  fi
else
  CONTEXT="Session $ACTIVE is already active (auto-managed by the sv-memory hook). Do not call sv_mem_session_start."
fi

if [ -n "$CONTEXT" ]; then
  python3 -c 'import json,sys; print(json.dumps({"additionalContext": sys.argv[1]}))' "$CONTEXT"
fi
exit 0