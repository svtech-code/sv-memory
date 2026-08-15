#!/bin/bash
# sv-memory PreToolUse hook — strict mode
# Fires before Read/Glob/Grep.
# On the FIRST file-read tool call of the session, it emits a strong redirect
# to the sv-memory graph. Subsequent calls get a soft nudge.
# The session flag is reset automatically when the temp is cleared or on reboot.
# NOTE: On Claude Code this script NEVER blocks (always exit 0) — it only adds
# context to the tool call. Strict blocking is implemented only on platforms
# whose PreToolUse protocol supports it (Antigravity CLI).
# Optional silent context injection (opt-in): when .sv-memory/context-injection-enabled
# exists (created by `sv-memory hooks install --context-injection`), the FIRST Read of
# each file injects a compact graph+memory context pack as additional context.

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

# Portable hash of stdin: md5sum (GNU/Linux), md5 -q (BSD/macOS), shasum.
sv_mem_hash() {
  { md5sum 2>/dev/null || md5 -q 2>/dev/null || shasum -a 256 2>/dev/null; } | cut -d' ' -f1
}

TOOL_NAME="${CLAUDE_TOOL_CALL_NAME:-}"

case "$TOOL_NAME" in
  Read|Glob|Grep)
    # Compute a stable session flag keyed to the project directory.
    SESSION_KEY=$(echo "$PWD" | sv_mem_hash)
    if [ -z "$SESSION_KEY" ]; then
      SESSION_KEY="$PWD"
    fi
    FLAG_FILE="/tmp/.sv-memory-strict-${SESSION_KEY}"

    if [ ! -f "$FLAG_FILE" ]; then
      touch "$FLAG_FILE"
      echo "🔍 sv-memory (strict): This is your first file read this session."
      echo "Before reading source files directly, please query the dependency graph using sv_graph_query or check past decisions via sv_mem_search."
      echo "sv-memory combines structural code graphs with persistent decision memory — using it first saves tokens and provides richer context."
      FIRST_READ=1
    fi

    if [ -z "$FIRST_READ" ]; then
      echo "💡 sv-memory: Consider using sv_graph_query or sv_mem_search before file reads for token-efficient context."
    fi
    ;;
esac

# --- Optional silent context injection (opt-in) ---
if [ -f ".sv-memory/context-injection-enabled" ]; then
  case "$TOOL_NAME" in
    Read)
      TOOL_INPUT="$(cat 2>/dev/null || true)"
      FILE_PATH="$(printf '%s' "$TOOL_INPUT" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
      if [ -n "$FILE_PATH" ] && [ -f "$FILE_PATH" ]; then
        CACHE_KEY="$(printf '%s|%s' "$PWD" "$FILE_PATH" | sv_mem_hash)"
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
