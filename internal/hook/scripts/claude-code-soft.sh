#!/bin/bash
# sv-memory PreToolUse hook — soft mode
# Fires before Read/Glob/Grep and nudges the agent toward sv-memory tools.
# Does NOT block the tool call — just adds context.
# Optional silent context injection (opt-in): when .sv-memory/context-injection-enabled
# exists (created by `sv-memory hooks install --context-injection`), the FIRST Read of
# each file injects a compact graph+memory context pack as additional context.
# Always exits 0 (fail-open): a missing binary, missing .sv-memory, or slow run
# never breaks the tool call.

# Portable timeout: GNU timeout on Linux, gtimeout on macOS, else background+kill.
run_with_timeout() {
  if command -v timeout >/dev/null 2>&1; then
    timeout "$1" "${@:2}"
  elif command -v gtimeout >/dev/null 2>&1; then
    gtimeout "$1" "${@:2}"
  else
    "${@:2}" &
    local pid=$!
    ( sleep "$1"; kill "$pid" 2>/dev/null ) &
    wait "$pid" 2>/dev/null
  fi
}

TOOL_NAME="${CLAUDE_TOOL_CALL_NAME:-}"

case "$TOOL_NAME" in
  Read|Glob|Grep)
    echo "💡 sv-memory: Before reading files, consider using sv_mem_search (for past decisions/bugs) or sv_graph_query (for dependency context). This saves tokens and gives better architectural awareness."
    ;;
esac

# --- Optional silent context injection (opt-in) ---
if [ -f ".sv-memory/context-injection-enabled" ]; then
  case "$TOOL_NAME" in
    Read)
      TOOL_INPUT="$(cat 2>/dev/null || true)"
      FILE_PATH="$(printf '%s' "$TOOL_INPUT" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
      if [ -n "$FILE_PATH" ] && [ -f "$FILE_PATH" ]; then
        if command -v md5 >/dev/null 2>&1; then
          CACHE_KEY="$(printf '%s|%s' "$PWD" "$FILE_PATH" | md5 -qs 2>/dev/null || printf '%s|%s' "$PWD" "$FILE_PATH" | md5sum 2>/dev/null | cut -d' ' -f1)"
        else
          CACHE_KEY="$(printf '%s|%s' "$PWD" "$FILE_PATH" | shasum -a 256 2>/dev/null | cut -d' ' -f1)"
        fi
        CACHE_FILE="/tmp/.sv-memory-ctx-${CACHE_KEY}"
        if [ ! -f "$CACHE_FILE" ]; then
          CONTEXT_OUTPUT="$(run_with_timeout 2 sv-memory context "$FILE_PATH" --max-memories 3 2>/dev/null || true)"
          if [ -n "$CONTEXT_OUTPUT" ]; then
            printf '%s' "$CONTEXT_OUTPUT" > "$CACHE_FILE"
            echo ""
            echo "🔍 sv-memory context for $(basename "$FILE_PATH"):"
            echo "$CONTEXT_OUTPUT"
          fi
        fi
      fi
      ;;
  esac
fi

exit 0
