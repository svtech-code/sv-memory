#!/bin/bash
# sv-memory PreToolUse hook — strict mode
# Fires before Read/Glob/Grep.
# On the FIRST file-read tool call of the session, it emits a strong redirect
# to the sv-memory graph. Subsequent calls get a soft nudge.
# The session flag is reset automatically when the temp is cleared or on reboot.
# NOTE: On Claude Code this script NEVER blocks (always exit 0) — it only adds
# context to the tool call. Strict blocking is implemented only on platforms
# whose PreToolUse protocol supports it (Antigravity CLI).

TOOL_NAME="${CLAUDE_TOOL_CALL_NAME:-}"

case "$TOOL_NAME" in
  Read|Glob|Grep)
    # Compute a stable session flag keyed to the project directory.
    if command -v md5 >/dev/null 2>&1; then
      SESSION_KEY=$(echo "$PWD" | md5 -qs 2>/dev/null || echo "$PWD" | md5sum 2>/dev/null | cut -d' ' -f1)
    else
      SESSION_KEY=$(echo "$PWD" | shasum -a 256 2>/dev/null | cut -d' ' -f1 || echo "$PWD")
    fi
    FLAG_FILE="/tmp/.sv-memory-strict-${SESSION_KEY}"

    if [ ! -f "$FLAG_FILE" ]; then
      touch "$FLAG_FILE"
      echo "🔍 sv-memory (strict): This is your first file read this session."
      echo "Before reading source files directly, please query the dependency graph using sv_graph_query or check past decisions via sv_mem_search."
      echo "sv-memory combines structural code graphs with persistent decision memory — using it first saves tokens and provides richer context."
      exit 0
    fi

    echo "💡 sv-memory: Consider using sv_graph_query or sv_mem_search before file reads for token-efficient context."
    ;;
esac

exit 0
