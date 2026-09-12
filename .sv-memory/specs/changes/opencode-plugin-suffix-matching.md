# OpenCode plugin: suffix matching for spec tool detection

- **ID:** `ba53d187d2da4c6e`
- **Slug:** `opencode-plugin-suffix-matching`
- **Status:** `applied`
- **Where:** `internal/hook/scripts/opencode-plugin-strict.ts`
- **Capability:** `opencode-plugin-suffix-matching`
- **Created:** 2026-09-12T19:13:47-03:00

## Proposal

The corrective spec nudge in opencode-plugin-strict.ts hardcoded MCP client prefixes (sv-memory_sv_spec_list) to detect spec tool usage. If the prefix changes, the nudge misfires. Change to suffix matching (endsWith) so detection works regardless of client prefix.

## Goal

Make the OpenCode plugin robust against MCP client prefix changes, maintaining the model-agnostic corrective nudge for any prefix convention.

## Design

Replace `input.tool === "sv-memory_sv_spec_list"` with `t.endsWith("sv_spec_list")` for all three spec tools. Add a guard test that asserts endsWith patterns exist and hardcoded prefixes do not.

## Tasks

- [x] Change hardcoded prefix matches to suffix matches (endsWith) in tool.execute.after
- [x] Sync installed copy (.opencode/plugin/sv-memory.ts) with template
- [x] Add TestOpenCodePluginUsesSuffixMatching guard test
- [x] Run gate CI