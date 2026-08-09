#!/bin/bash
# sv-memory PreToolUse hook -- Antigravity CLI (agy) strict mode
# Blocks the first view_file/grep_search/list_dir call per session,
# emitting a nudge to the model via stderr (exit code 2).
# Subsequent calls are allowed (session tracked via temp flag file).
#
# Fail-open behavior: the block is skipped when sv-memory appears to be
# unavailable (no .sv-memory directory, binary missing from PATH, or
# SV_MEMORY_STRICT_DISABLE set), so a missing/unconfigured sv-memory never
# deadlocks the agent on its first file read. The hooks themselves never
# call the sv-memory server; they only inspect local files and env vars.

PAYLOAD=$(cat)
TOOL_NAME=$(echo "$PAYLOAD" | python3 -c "import sys,json; print(json.load(sys.stdin).get('toolCall',{}).get('name',''))" 2>/dev/null)

# Explicit opt-out: never block.
if [ -n "${SV_MEMORY_STRICT_DISABLE:-}" ]; then
  echo '{"decision":"allow"}'
  exit 0
fi

# Fail-open: sv-memory not initialized for this project.
if [ ! -d "$PWD/.sv-memory" ]; then
  echo '{"decision":"allow"}'
  exit 0
fi

# Fail-open: sv-memory binary not installed, so the nudged tools cannot run.
if ! command -v sv-memory >/dev/null 2>&1; then
  echo '{"decision":"allow"}'
  exit 0
fi

if command -v md5 >/dev/null 2>&1; then
  SESSION_KEY=$(echo "$PWD" | md5 -qs 2>/dev/null || echo "$PWD" | md5sum 2>/dev/null | cut -d' ' -f1)
else
  SESSION_KEY=$(echo "$PWD" | shasum -a 256 2>/dev/null | cut -d' ' -f1 || echo "$PWD")
fi
FLAG_FILE="/tmp/.sv-memory-agy-strict-${SESSION_KEY}"

case "$TOOL_NAME" in
  view_file|grep_search|list_dir)
    if [ ! -f "$FLAG_FILE" ]; then
      touch "$FLAG_FILE"
      cat <<'BLOCKMSG' >&2
sv-memory (strict): This is your first file/source read this session.

Before reading source files directly, query the dependency graph using
sv_graph_query or check past decisions via sv_mem_search. sv-memory combines
structural code graphs with persistent decision memory.

If sv-memory is not responding, re-run this read (it is allowed now).
BLOCKMSG
      exit 2
    fi
    echo '{"decision":"allow"}'
    exit 0
    ;;
esac

echo '{"decision":"allow"}'
exit 0
