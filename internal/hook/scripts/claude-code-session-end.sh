#!/bin/bash
# sv-memory SessionEnd hook — Claude Code
# Fires when a session ends. Reminds the agent to close the sv-memory session
# so context recovery works in the next session.
# Always exits 0 (fail-open).
echo "💡 sv-memory: Call sv_mem_session_summary (if not already done) and then sv_mem_session_end to close this session." >&2
exit 0
