#!/bin/bash
# sv-memory PreToolUse hook — soft mode
# Fires before Read/Glob/Grep and nudges the agent toward sv-memory tools.
# Does NOT block the tool call — just adds context.

TOOL_NAME="${CLAUDE_TOOL_CALL_NAME:-}"

case "$TOOL_NAME" in
  Read|Glob|Grep)
    echo "💡 sv-memory: Before reading files, consider using sv_mem_search (for past decisions/bugs) or sv_graph_query (for dependency context). This saves tokens and gives better architectural awareness."
    ;;
esac

exit 0
