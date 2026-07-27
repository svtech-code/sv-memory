#!/bin/bash
# sv-memory PreToolUse hook — Codex platform
# Intentionally a no-op: Codex Desktop rejects additionalContext on PreToolUse,
# so emitting a nudge there would break Bash tool calls.
# AGENTS.md is the always-on mechanism for this platform.
exit 0
