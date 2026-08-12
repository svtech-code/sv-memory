#!/bin/bash
# sv-memory SubagentStop hook — Claude Code
# Fires when a subagent finishes. Subagents work with a fresh context, so
# findings should be persisted back into project memory before they are lost.
# Always exits 0 (fail-open).
echo "💡 sv-memory: If this subagent discovered anything durable (bugfix, decision, standard), persist it with sv_mem_save before continuing." >&2
exit 0
