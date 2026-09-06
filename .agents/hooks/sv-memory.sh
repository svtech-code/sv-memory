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

# Portable hash of stdin: md5sum (GNU/Linux), md5 -q (BSD/macOS), shasum.
sv_mem_hash() {
  { md5sum 2>/dev/null || md5 -q 2>/dev/null || shasum -a 256 2>/dev/null; } | cut -d' ' -f1
}

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

SESSION_KEY=$(echo "$PWD" | sv_mem_hash)
if [ -z "$SESSION_KEY" ]; then
  SESSION_KEY="$PWD"
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
  write_file|edit_file|replace_in_file)
    # Write nudge: once per session, remind to propose before modifying behavior.
    WRITE_FLAG="/tmp/.sv-memory-agy-write-${SESSION_KEY}"
    if [ ! -f "$WRITE_FLAG" ]; then
      touch "$WRITE_FLAG"
      cat <<'WRITENUDGE' >&2
sv-memory: First file write this session. If this modifies behavior,
contracts, APIs, or architecture, run sv_propose_spec first (MANDATORY
per AGENTS.md Spec-Driven Decision Cycle).
WRITENUDGE
    fi
    echo '{"decision":"allow"}'
    exit 0
    ;;
esac

echo '{"decision":"allow"}'
exit 0
