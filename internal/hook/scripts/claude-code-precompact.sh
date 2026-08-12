#!/bin/bash
# sv-memory PreCompact hook — Claude Code
# Fires immediately before context compaction. This is the last chance to
# persist session state before the window is lost — nudge the agent to save.
# Always exits 0 (fail-open).
echo "💡 sv-memory: Context is about to be compacted. Call sv_mem_session_summary (goal, discoveries, accomplished, next steps) BEFORE the compacted summary arrives, then sv_mem_context afterwards." >&2
exit 0
