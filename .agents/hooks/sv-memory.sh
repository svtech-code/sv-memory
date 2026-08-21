#!/bin/bash
# sv-memory PreToolUse hook -- Antigravity CLI (agy) soft mode
# agy PreToolUse stdout is the decision response, not additionalContext.
# The always-on nudge mechanism is AGENTS.md for this platform.
echo '{"decision":"allow"}'
exit 0
