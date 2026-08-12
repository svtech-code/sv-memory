#!/bin/bash
# sv-memory SessionStart hook — Claude Code
# Fires when a session starts. Nudges the agent to begin the sv-memory session
# lifecycle so memories are grouped and the Auto-Boot bundle is loaded.
# Always exits 0 (fail-open).
echo "💡 sv-memory: Call sv_mem_session_start to load the Auto-Boot Context Bundle and group this session's memories." >&2
exit 0
