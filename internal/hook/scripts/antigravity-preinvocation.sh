#!/bin/bash
# sv-memory PreInvocation hook -- Antigravity CLI (agy)
#
# Deterministically auto-starts the sv-memory session and injects the Auto-Boot
# Context Bundle as an ephemeral message on the first invocation of a
# conversation, so ANY model receives prior context without needing to call
# sv_mem_session_start. On later invocations it injects a compact adaptive
# reminder (only when active spec changes exist) instead of repeating the full
# bundle, so the agent keeps using graph/memory/spec tools after idle periods.
#
# Contract (agy PreInvocation): reads the payload JSON on stdin and prints a
# single JSON object on stdout with `injectSteps`. Fail-open: when sv-memory is
# unavailable or the project is uninitialized it prints `{}` and exits 0. The
# hook never blocks the agent.

SV=sv-memory

emit_empty() {
  echo '{}'
  exit 0
}

PAYLOAD=$(cat)

command -v "$SV" >/dev/null 2>&1 || emit_empty
command -v python3 >/dev/null 2>&1 || emit_empty

# Extract camelCase fields (agy protojson encoding).
read -r INVOCATION_NUM CONV_ID WS < <(printf '%s' "$PAYLOAD" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    d = {}
paths = d.get("workspacePaths") or [""]
print(d.get("invocationNum", 0), d.get("conversationId", ""), paths[0] if paths else "")
' 2>/dev/null)

# Fail-open: no workspace or not an sv-memory project.
[ -n "$WS" ] || emit_empty
[ -d "$WS/.sv-memory" ] || emit_empty

# Run sv-memory from the workspace root (agy runs hooks with the hooks.json
# directory as cwd, so we must cd to the project before project-scoped commands).
cd "$WS" 2>/dev/null || emit_empty

# Conversation-scoped state file so the full bundle is injected once per
# conversation while later invocations only get a compact reminder.
KEY=$(printf '%s' "${CONV_ID:-$WS}" | { md5sum 2>/dev/null || md5 -q 2>/dev/null || shasum -a 256 2>/dev/null; } | cut -d' ' -f1)
[ -n "$KEY" ] || KEY=$(printf '%s' "${CONV_ID:-$WS}" | tr -c 'A-Za-z0-9' '_')
STATE="/tmp/.sv-memory-agy-boot-${KEY}"

CONTEXT=""
if [ ! -f "$STATE" ]; then
  : > "$STATE" 2>/dev/null
  ACTIVE=$("$SV" session active 2>/dev/null)
  if [ -z "$ACTIVE" ] || [ "$ACTIVE" = "none" ]; then
    OUT=$("$SV" session start 2>/dev/null)
    if [ -n "$OUT" ]; then
      CONTEXT="Session is auto-managed by the sv-memory PreInvocation hook — do not call sv_mem_session_start. Use sv_mem_session_end when done.

$OUT"
    fi
  else
    CONTEXT="Session $ACTIVE is already active (auto-managed by the sv-memory hook). Do not call sv_mem_session_start."
  fi
else
  # Later invocation: only nudge periodically to keep the hook cheap and quiet.
  NUDGE_EVERY=${SV_MEMORY_AGY_NUDGE_EVERY:-8}
  case "$NUDGE_EVERY" in ''|*[!0-9]*) NUDGE_EVERY=8 ;; esac
  [ "$NUDGE_EVERY" -gt 0 ] 2>/dev/null || NUDGE_EVERY=8
  if [ $((INVOCATION_NUM % NUDGE_EVERY)) -eq 0 ]; then
    if "$SV" specs list 2>/dev/null | grep -qE '^(draft|proposed|validated)[[:space:]]'; then
      CONTEXT="sv-memory: there are active spec changes. Before changing behavior, contracts, APIs or architecture, follow the Spec-Driven Decision Cycle (sv_spec_list → sv_propose_spec → sv_update_spec → sv_validate_decision → sv_commit_spec). Before reading or editing source, query sv_graph_explore / sv_graph_explore and sv_mem_search."
    fi
  fi
fi

if [ -n "$CONTEXT" ]; then
  python3 -c 'import json,sys; print(json.dumps({"injectSteps":[{"ephemeralMessage":sys.argv[1]}]}))' "$CONTEXT"
else
  echo '{}'
fi
exit 0
