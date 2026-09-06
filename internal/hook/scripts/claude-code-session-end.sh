#!/bin/bash
# sv-memory SessionEnd hook — Claude Code
# Deterministically closes the sv-memory session when the Claude Code session
# ends. Idempotent: a session already completed is left untouched, and any
# summary saved earlier is preserved. This guarantees no active session leaks
# even when the model forgets to call sv_mem_session_end. Fail-open. Always
# exits 0.
[ -n "$CLAUDE_PROJECT_DIR" ] && cd "$CLAUDE_PROJECT_DIR" 2>/dev/null
SV=sv-memory
command -v "$SV" >/dev/null 2>&1 || exit 0
[ -d "$PWD/.sv-memory" ] || exit 0

"$SV" session end >/dev/null 2>&1
echo "💡 sv-memory: Session closed. If you did not save a summary, call sv_mem_session_summary at the start of the next session." >&2
exit 0