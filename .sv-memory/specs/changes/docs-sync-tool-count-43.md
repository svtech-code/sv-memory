# Sync MCP tool count 43/36 in docs (EN/ES) + document full skill workflow

- **ID:** `01b1c9361cc04ba5`
- **Slug:** `docs-sync-tool-count-43`
- **Status:** `applied`
- **Where:** `README.md, README_ES.md, documentation/AGENT-SETUP.md, documentation/AGENT-SETUP_ES.md, documentation/spect.md, documentation/spect_ES.md`
- **Capability:** `docs-sync-tool-count-43`
- **Created:** 2026-09-12T19:22:29-03:00

## Proposal

Fix pre-existing doc drift: 42 tools / 35 core → 43 tools / 36 core (sv_mem_fetch_reference added in v0.21.0 without updating docs). Also add clarifying note that installed skills carry the full workflow.

## Goal

Docs reflect the actual tool count (43 total, 36 core) and accurately describe the installed skill content.

## Design

Replace all instances of "42" (tool count) with "43" and "35 core" with "36 core" across the 6 docs. Fix broken TOC anchors. Add one-line note per skill section clarifying the skill carries the full workflow.

## Tasks

- [x] README.md: fix TOC anchor + count 42→43
- [x] README_ES.md: fix TOC anchor + count 42→43
- [x] AGENT-SETUP.md: 42→43, 35→36 core, add full workflow note
- [x] AGENT-SETUP_ES.md: same + ES
- [x] spect.md: 42→43 in all mentions
- [x] spect_ES.md: same
- [x] Run gate CI