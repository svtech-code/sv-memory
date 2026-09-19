#!/bin/bash
# sv-memory PreToolUse hook -- Antigravity CLI (agy) strict mode
#
# Blocks the first file/source read of each conversation, returning a JSON
# `deny` decision whose `reason` nudges the model to query the dependency graph
# (sv_graph_explore / sv_graph_query) and past decisions (sv_mem_search) before
# reading source directly. Subsequent reads in the same conversation are
# allowed. The graph-first redirect re-arms for every new conversation.
#
# agy contract: the hook must print a JSON object on stdout
# ({"decision":"allow|deny|ask|force_ask","reason":"..."}). It is fail-open:
# when sv-memory is unavailable, the project is uninitialized, or
# SV_MEMORY_STRICT_DISABLE is set, the read is allowed instead of deadlocking.

PAYLOAD=$(cat)

command -v python3 >/dev/null 2>&1 || { echo '{"decision":"allow"}'; exit 0; }

read -r TOOL_NAME CONV_ID WS < <(printf '%s' "$PAYLOAD" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    d = {}
paths = d.get("workspacePaths") or [""]
print(d.get("toolCall", {}).get("name", ""), d.get("conversationId", ""), paths[0] if paths else "")
' 2>/dev/null)

# Explicit opt-out: never block.
if [ -n "${SV_MEMORY_STRICT_DISABLE:-}" ]; then
  echo '{"decision":"allow"}'
  exit 0
fi

# Fail-open when sv-memory is not usable for this project. agy runs hooks with
# the hooks.json directory as cwd, so the project root comes from the payload.
if ! command -v sv-memory >/dev/null 2>&1; then
  echo '{"decision":"allow"}'
  exit 0
fi
if [ -z "$WS" ] || [ ! -d "$WS/.sv-memory" ]; then
  echo '{"decision":"allow"}'
  exit 0
fi

case "$TOOL_NAME" in
  view_file|grep_search|list_dir) ;;
  *)
    echo '{"decision":"allow"}'
    exit 0
    ;;
esac

# Conversation-scoped flag: the redirect fires once per conversation.
KEY=$(printf '%s' "${CONV_ID:-$WS}" | { md5sum 2>/dev/null || md5 -q 2>/dev/null || shasum -a 256 2>/dev/null; } | cut -d' ' -f1)
[ -n "$KEY" ] || KEY=$(printf '%s' "${CONV_ID:-$WS}" | tr -c 'A-Za-z0-9' '_')
FLAG_FILE="/tmp/.sv-memory-agy-strict-${KEY}"

if [ ! -f "$FLAG_FILE" ]; then
  : > "$FLAG_FILE" 2>/dev/null
  REASON="sv-memory (strict): this is your first source read in this conversation. Before reading source files directly, query the dependency graph (sv_graph_explore / sv_graph_query) and check past decisions with sv_mem_search. If sv-memory is not responding, re-run this read — it will be allowed now."
  python3 -c 'import json,sys; print(json.dumps({"decision":"deny","reason":sys.argv[1]}))' "$REASON"
  exit 0
fi

echo '{"decision":"allow"}'
exit 0
