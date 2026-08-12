# Changelog

All notable changes follow [Conventional Commits](https://www.conventionalcommits.org/).
Releases are tagged `vX.Y.Z`; the CI pipeline builds and publishes them automatically.

## [Unreleased]

### Added

- **`sv_mem_context_pack` MCP tool + `sv-memory context <path>` CLI:** a compact, fused context pack for a file, package, or symbol — the node's structural role in the dependency graph (type, fan-in/fan-out, community, hub flag) plus the memories linked to that path via `where_path` or `rationale_for` edges (decisions, standards, bugfixes), each rendered as title + `why` truncated to `bundle_why_chars`. One bounded call replaces the `sv_graph_explain` + `sv_mem_search path=` + `sv_mem_get` round-trips (the proprietary graph→memory bridge that also powers the upcoming optional silent context-injection hook). Configurable via `context_pack_max_memories` (default 5, max 20). Tool count grows 28 → 29; docs (EN/ES), protocol, skills, and CHANGELOG synced.
- **`token_budget` on `sv_mem_session_start` and `sv_mem_timeline`:** the two remaining large read paths now accept a per-call token budget (or fall back to the global `max_response_tokens` default), rounding out budget coverage across all bulk-returning tools.
- **Configurable truncation thresholds:** the per-field caps `max_field_chars` (1000), `search_expand_chars` (300), `timeline_why_chars` (200), and `bundle_why_chars` (300) can now be tuned via `~/.sv-memory/config.yaml` or `.sv-memory/config.yaml` without recompiling; the compiled-in values remain the defaults. `sv-memory configure list` reports the new keys.

### Changed

- **`sv_mem_session_start` now respects the token budget:** the Auto-Boot Context Bundle + Graph Hubs response is routed through the shared `Server.respond` truncation, so the largest pre-tool payload of each session is bounded by `max_response_tokens` (or a per-call `token_budget`), instead of being returned unguarded.
- **Auto-Boot Bundle now surfaces postmortems and recent Q&A:** the session-start bundle adds a **Postmortems & Lessons Learned** section (the single most recent `postmortem`, with its rationale) and a **Recent Q&A** section (the single most recent `qa`, title-only to stay compact), both deduplicated against the previous-session section. Protocol/docs (EN/ES), skills, and CHANGELOG updated.
- **Partial indexes for the hot session lookups:** migration v12 adds `idx_sessions_active_started` (`sessions(project_id, started_at DESC) WHERE status='active'`) and `idx_sessions_completed_ended` (`sessions(project_id, ended_at DESC) WHERE status='completed'`), so `GetActiveSession` (on every save) and `GetLastSession` (Auto-Boot) serve both the status predicate and the sort directly.
- **`TopDegreeNodes` filters documents before the degree joins:** the `node_type != 'document'` predicate now lives inside the node scan subquery, shrinking the JOIN matrix for the Auto-Boot Graph Hubs query.

### Fixed

- **Compaction now preserves the synthesized row's full metadata:** the unified memory created when multiple `topic_key` revisions are consolidated now carries over `last_seen_at`, `normalized_hash` (recomputed from the consolidated content so future dedup detection works), `review_after` (falls back to the category decay deadline when unset, so the row stays on the policy-review radar), and `pinned`. It also keeps the latest source row's `created_at` instead of resetting it to `CURRENT_TIMESTAMP`, so chronological queries (timeline, recent activity) are not disrupted. The insert goes through the shared `memoryInsertArgs` helper instead of a hand-rolled 18-column statement.
- **`sv_mem_compact` advances the compaction watermark:** the manual tool now routes through `CompactMemoriesIncremental`, sharing the same `last_compaction_at` watermark as the background worker. After the first full pass, a manual trigger only re-processes topic keys with new activity instead of re-scanning the entire history on the next worker tick.
- **Session lookup no longer blocks the single writer connection:** the auto-association of saves and passive observations (`GetActiveSession`) now reads through the reader pool instead of the writer, removing a serialization point on every `sv_mem_save` / `sv_mem_capture_passive` call.
- **Spec dedup hash drift fixed:** the rolling-window dedup hash is documented as `what + why + learned + where_path` (was `what + why + learned`), matching the actual `computeHash` implementation.

## [v0.8.0] - 2026-08-09

### Changed

- **MCP tool surface reduced (30 → 28):** `sv_mem_unpin` merged into `sv_mem_pin` via `action` (`'pin'` default, `'unpin'`), and `sv_mem_current_project` folded into `sv_mem_stats`, which now reports the active project ID, name, and path. Fewer tools reduce model-selection friction and per-session context overhead. Existing permission allow-lists retain the removed tool names as harmless stale entries; re-run the grant to refresh them.
- **PreToolUse strict hooks are now fail-open:** the Antigravity strict hook only blocks the first file read when sv-memory is detected (`.sv-memory/` present, binary on PATH) and honors the `SV_MEMORY_STRICT_DISABLE=1` opt-out; otherwise it allows the read, so a missing/unconfigured sv-memory never deadlocks the agent. Docs now state that Claude Code strict mode is nudge-only (never blocks), aligning the documented behavior with the scripts.
- **Git sync is resilient to real merge conflicts:** importing a chunk left with git conflict markers (same-ID concurrent edit) or any unparseable JSON now **skips that chunk with a warning** instead of aborting the whole import; the remaining chunks still arrive. When a pulled chunk would overwrite a newer local edit (higher `revision_count`) or one diverged at the same revision, a **last-writer-wins warning** is logged so a lost local edit is surfaced rather than silently dropped. The conflict model (including resolution steps) is documented in the README, spec, and getting-started guides (EN/ES).
- **Sync cleanup:** the unused `SyncToGitChunked` is removed and chunk writing is unified behind a shared `writeChunkFiles` helper (atomic tmp+rename). `sv_mem_session_start` now imports git-pulled chunks (via `maybeSyncFromGit`) before assembling the Auto-Boot bundle, so a fresh session starts from the latest team context without an explicit search.
- **Secrets are redacted end-to-end:** the code-graph pipeline now redacts content-derived text (markdown headings, rationale comments, SQL defaults) before persisting to `graph_nodes`, and the import paths (`ImportJSON`, git chunk import, monolithic `memories.json`) plus session summaries sanitize free-text fields through a shared `sanitizeMemoryFields` helper. The Auto-Boot bundle, session context, and graph Obsidian vault render through `SanitizeText` (defense in depth), and the generated `graph.html` escapes node id/type/path/metadata (fixing a stored-XSS sink). The default `.sv-memoryignore` template now excludes `.env`, keys, PEM, credentials, `.ssh/.aws/.gcp`, and `secrets.yaml`. See the **Secret Hygiene** docs section (README / spec, EN/ES).
- **Dead code removed:** `CountRelations`, `GetSession`, `SearchMemoriesBySession`, `perm.Infos`, `graph.SetExtractor` + `MDSemanticExtractor`, and two write-only struct fields were dropped. Deduplication: a shared `memoryInsertArgs` helper replaces three identical 22-parameter import calls, `SearchMemoriesScoped` reuses `scanMemories`, and a `Server.respond` helper collapses ten token-truncation call sites. Git metadata now goes through a single batched `GetGitMetadata` entry point.
- **Lint fixes for CI:** tests introduced in the security/git-sync work no longer use `if err := X(); err != nil` when an outer `err` is in scope, which golangci-lint's `govet` (enable-all) flagged as shadowing. They now reuse the outer `err` (`if err = X(); err != nil`), matching the repo pattern from 9adc79f/6613b56. The repo rule and a memory standard now require running `golangci-lint run ./...` (v2.12.2) before pushing, since `go vet` alone does not run the shadow analyzer.

## [v0.7.0] - 2026-08-08

### Added

- `sv_mem_update` MCP tool: partially update an existing memory by ID (fields `what`, `why`, `learned`, `where_path`, `impact`, `errors_faced`, `next_steps`). Identity fields are preserved, the revision counter advances, and the change is synced to Git.
- `sv_mem_diagnose` MCP tool: read-only health checks (database, schema tables, FTS5 triggers, project registration, write permissions, chunks) combined with structural graph diagnostics (dangling edges, orphan nodes, self-loops, missing files).
- `sv_mem_review` `action="mark_reviewed"`: reset a memory's policy-review deadline (`review_after`) after it has been validated, closing the review lifecycle loop.
- **Memory→code graph unification:** saving or updating a memory with a `where_path` now auto-links it to its code node through a `rationale_for` edge (memory `document` node + edge). `sv_graph_explain`/`sv_graph_query` surface associated decisions/standards, and the links are re-created automatically after a full graph rebuild.
- **Installer checksum verification:** `install.sh` and `install.ps1` now verify the downloaded binary against the release's `checksums.txt` (SHA-256). A mismatched hash aborts the install; missing checksums warn instead of failing.
- **CI on Windows:** the test job now runs `go vet`, `go test -race -cover`, and a build check on `windows-latest` too, covering the Windows binaries that releases publish.
- **Semantic LLM conflict judging:** `sv-memory conflicts scan --semantic [--agent claude|opencode] [--max-semantic N] [--concurrency N]` and `sv_mem_conflicts action=scan semantic=true` LLM-judge candidate conflict pairs using the configured agent CLI. Verdicts (`supersedes`/`conflicts_with`/`relates_to`/`none`) are persisted with `judged_by='llm'`; failed judgments stay pending for retry.

### Changed

- The injected protocol (`AGENTS.md` / `.cursorrules` / `.windsurfrules`) gains a **Tool Usage in Any Mode** section: the sv-memory MCP tools may be used in any operational mode (plan, build, or review), since they persist only to the project memory store (`.sv-memory/`).
- `sv_mem_conflicts` tool surface expanded with `semantic`, `agent`, `max_semantic`, and `concurrency` parameters.

## [v0.6.0] - 2026-08-07

### Added

- `sv_mem_session_start` Auto-Boot bundle now includes a **Graph Hubs** section:
  top code nodes by degree (fan-in + fan-out) computed with a single cheap
  aggregate query (documents excluded, no centrality), so agents orient without
  a separate `sv_graph_god_nodes` call.
- `sv_graph_explain` adds an **Actionable Suggestions** section (refactor risk
  level + concrete next steps) derived from fan-in/fan-out/betweenness.
- `sv_graph_surprising_connections` now highlights the most surprising bridge
  with a score summary and a `sv_graph_path` drill-down hint.
- `sv_mem_pin` / `sv_mem_unpin`: pin local memories so key decisions surface
  first in `sv_mem_context` (📌 Pinned section). Pinned state is local-only.
- `sv_mem_search` `match_mode` (`all` default / `any`): broader FTS5 recall
  when a memory matching one or more query tokens is useful.
- Decay-driven `review_after` per memory category (decision/architecture 6mo,
  standard 12mo, bugfix/idea 3mo). `sv_mem_review` now flags and prioritizes
  memories due for policy review.
- Global `max_response_tokens` config (default 4000) enforced across read
  handlers via shared `resolveTokenBudget` / `truncateToTokenBudget` helpers;
  `sv_mem_search` / `sv_mem_get` / `sv_mem_context` accept a per-call
  `token_budget` override.
- `BenchmarkToolResponseTokens` regression guard measuring per-call bytes and
  estimated tokens for search/get/timeline/context.
- Incremental compaction tests: `TestCompactMemoriesIncrementalOnlyProcessesNewTopics`
  (watermark behavior) and `TestCompactMemoriesSkipsSingleRowHighRevision`
  (inflation fix).
- `TestTopDegreeNodes` (ranking + document exclusion) and
  `TestSessionStartIncludesGraphHubs` (bundle integration).

### Changed

- **Auto-compaction is now incremental**: the background worker only re-processes
  topic keys that changed since the last run (`projects.last_compaction_at`
  watermark, migration v11) instead of scanning the full history every tick.
- Fixed compaction inflation: a single surviving row under a topic_key (where
  upserts overwrite content rather than accumulating history) is no longer
  re-synthesized on every run, which previously bumped `revision_count` and
  created near-duplicate records.
- Removed self-referential `*Response: ~N tokens*` estimate from
  `sv_mem_search` / `sv_mem_get` / `sv_mem_review` / `sv_mem_stats` and
  `sv_graph_god_nodes` / `sv_graph_surprising_connections` outputs (the LLM
  never used it).
- Lowered default `sv_mem_get` field truncation from 1500 to 1000 chars
  (`max_chars="0"` remains unlimited).
- Reader connection pool increased from 8 to 16 with unlimited connection
  lifetime (keeps WAL readers warm; idle pruning still applies via
  `ConnMaxIdleTime`).
- `sv_mem_save` similar-memories hint now emits a ready-to-call
  `sv_mem_judge(source_id, target_id, relation_type)` per candidate.
- 6 admin/maintenance tools (`sv_mem_delete`, `sv_mem_compact`, `sv_mem_review`,
  `sv_mem_conflicts`, `sv_graph_viz`, `sv_graph_merge`) marked as
  `defer_loading` for MCP clients supporting dynamic tool loading (tool count
  documented as 28).

### Docs

- Commit language rule: **English by default** in the injected protocol template
  (was "Spanish by default"); propagated to `AGENTS.md` and the `spect.md` §8
  embedded template.
- `spect.md` MCP Tools section now covers all 28 tools (`sv_mem_pin` /
  `sv_mem_unpin` added and renumbered contiguously 1-28), documents the
  `match_mode` and `token_budget` parameters, and fixes stale values
  (`max_chars` default 2000 → 1000, reader pool 8 → 16, benchmark renamed to
  `BenchmarkToolResponseTokens`).
- `CONTRIBUTING.md`: absolute `file:///` links converted to repo-relative paths;
  `symbolScanExts` correctly pointed to `scanner.go`; tool-registration example
  updated to the real `ms.AddTool` + `handleX` pattern.
- `SECURITY.md`: supported-versions policy declared as **latest minor only**
  (0.6.x).
- `README.md` / `README_ES.md` / getting-started guide: granted-permissions
  example updated to `28 / 28`, language count corrected to 17, hook status
  output harmonized, Phase-4 `configure` keyboard hints now documented in
  English as well.
- OpenCode skill: added Pin/Priority tool group and extended the graph-tools
  summary; fixed typos in `requirement.md` and a stale test-name comment in
  `internal/mcp/mcp.go`.

## [v0.5.0] - 2026-08-05

### Added

- Auto-Boot Context Bundle expanded: now includes standards, conventions,
  and recent bugfix/journal memories alongside key architectural decisions.
- OpenCode skill expanded from 40 to 93 lines: session lifecycle, progressive
  disclosure, topic keys, capture guidelines table, graph discovery tools,
  and periodic maintenance.
- Top-1 search result expanded inline with why/learned/path on `sv_mem_search`.
- `sv_mem_timeline` surfaces the central observation rationale (truncated).
- E2E tests for the full Auto-Boot two-session workflow, edge cases, and
  post-compaction integrity.
- MCP integration tests for `sv_graph_explain`, `sv_graph_god_nodes`, and
  render helpers (`escapeMermaid`, `commLabelStr`, `tokenBenchmark`).
- Schema JSON serialization tests for `Node` and `Edge` (round-trip + omitempty).
- `sv_graph_explain` / `sv_graph_god_nodes` tool descriptions now explain usage.

### Changed

- AGENTS.md protocol rewritten: single targeted search instead of triple-search
  pattern, `sv_mem_stats` for orientation, expanded Graph Inspection with
  `sv_graph_explain` / `sv_graph_god_nodes` / `sv_graph_path`, new Memory
  Maintenance and Tool Quick Reference sections, explicit session lifecycle.
- `sv_mem_search` compact table: removed Score column and revision/duplicate
  annotations, reducing token cost.
- Schema doc comments corrected to reflect the node types and relation types
  actually used in the codebase.

### Performance

- `SyncToGit` incremental chunk writes: only changed memories rewritten after
  the first sync (O(changes) instead of O(N) file writes).
- Chunks serialized as compact JSON, cutting ~20-30% sync I/O.
- `memories.json` rewritten only on first sync, every 10th sync, or forced flush.
- `FindSimilarMemories` time-boxed (200ms) and skipped for duplicate suppressions.
- Communities and betweenness centrality computed lazily on demand.
- New `idx_memories_session` index (migration v9) for save-time session lookup.
- Auto-Boot Bundle deduplication across session and per-category sections.

### Fixed

- Soft-deleted memories no longer leave orphan chunk files on disk.
- Topic-key updates are now correctly synced (replaced mtime-based short-circuit
  with `lastSyncTime` detection).
- `searchAllMemories` excludes soft-deleted rows from the active set.

### Docs

- `spect.md` Section 8 synchronized with the current `protocol.go` source.
- Token estimates updated from ~80 to ~30 tokens/result throughout documentation.
- Tool descriptions enriched for `sv_mem_session_start`, `sv_mem_context`,
  `sv_mem_compact`, `sv_graph_sync`, and `sv_graph_explain`.

## [v0.4.1] - 2026-08-04

### Fixed

- Graph cache now invalidates on any `mtime` change (including file restorations),
  not just when the timestamp increases.
- `ShortestPath` now respects the `max_hops` limit; path queries honor the hop bound.
- Propagate cleanup errors in incremental graph sync (`updateFileMeta`, `upsertNode`)
  and wiki export instead of silently ignoring them.
- `sv-memory update` now compares versions semantically, preventing accidental
  downgrades when the installed version is newer than the latest release.

### Changed

- Migrated `golangci-lint` to configuration v2 and pinned `v2.12.2` in CI.
- Raised the `gocyclo` threshold to 30 and marked monolithic extractor/export
  functions with justified `//nolint:gocyclo` comments.
- Removed the `only-new-issues` lint gate: the full lint now runs on every build.
- Aligned the release workflow Actions (`checkout@v7`, `setup-go@v7`) with CI.

### Added

- Behavioral tests for the TUI renderers/banner and CLI helpers (config,
  permissions, and updater), raising global statement coverage to 63%.

### Docs

- Synchronized `documentation/spect.md`, READMEs, and CHANGELOG with the current
  implementation.

## [v0.4.0] - 2026-08-03

### Added

- TUI brand banner with the SV Memory logo, dynamic width, and centered text.
- Configure wizard "shortcuts summary" step that documents the keyboard shortcuts,
  and removal of the redundant "Cancel configuration (exit)" sentinel option
  (cancellation is now `Ctrl+C` or the step-3 `No, cancel` confirmation).

### Changed

- Migrated the interactive TUI from `bufio` to `charmbracelet/huh`, aligning its
  banner color and theme with the `configure` wizard.
- Compacted `sv_mem_search` / `sv_mem_timeline` MCP output and lowered the default
  `max_chars` to save tokens.

### Fixed

- Use pure hex IDs and migrate graph tables during project consolidation.

### Performance

- Cache git branch/commit/author metadata for 30s in `sv_mem_save` to avoid up to 4
  git subprocess spawns per save.

## [v0.3.0] - 2026-08-02

### Added

- `sv-memory update` command: checks GitHub Releases for a newer version, asks for
  confirmation, downloads the platform binary, verifies its SHA-256 checksum against
  `checksums.txt`, and atomically replaces the running executable (on Windows it
  prints a manual `copy` command since the running .exe cannot be overwritten).

### Changed

- The `sv-memory configure` wizard now uses the SV Tech banner color (`#00B0C2`) for its
  borders, titles, descriptions, selectors, and buttons while keeping the green
  selection indicator. The Esc key now goes back to the previous step, and every screen
  shows its keyboard shortcuts (including `Ctrl+C` to exit).

## [v0.2.0] - 2026-08-02

### Added

- `sv-memory version` command that prints the build version, commit, and Go runtime.
  The version is injected at build time via `-ldflags`, so release binaries report the
  tag they were built from.
- Interactive `sv-memory configure` wizard built on `charmbracelet/huh`: arrow keys
  navigate, space toggles multi-select, Enter advances, and Esc goes back between
  steps. The banner now shows the real version instead of a hardcoded value.

## [v0.1.0] - 2026-08-02

### Added

- CI pipeline (`.github/workflows/ci.yml`): `go vet`, tests with the race detector, and a
  build check on `ubuntu-latest` + `macos-latest`.
- Release pipeline (`.github/workflows/release.yml`): cross-compiles macOS, Linux, and
  Windows binaries on a `v*` tag push and publishes them as GitHub Releases with checksums.
- `install.sh` (macOS/Linux): installs a prebuilt binary to `$HOME/.local/bin` without
  `sudo`, printing a PATH hint when the directory is not on the user's PATH.
- `install.ps1` (Windows): installs a prebuilt binary to `%LOCALAPPDATA%\sv-memory` and
  adds it to the user PATH.
- `make release` target to cross-compile release artifacts locally into `dist/`.
- `CHANGELOG.md` to track releases.

### Changed

- READMEs and the getting-started guide now document the one-line `curl`/`iwr` install
  commands and the new no-`sudo` install location.

### Security

- Generate memory/session/relation IDs with 64 bits of entropy (`newID()`) instead of a
  32-bit `uuid[:8]` prefix, preventing silent overwrites via `INSERT ... ON CONFLICT`.
- Harden FTS5 search so quotes-only queries return zero results instead of raising a
  syntax error, and move LIKE wildcard escaping into the memory layer with a length cap.
- Bound git helper commands with a 5s timeout so a stalled `git` process cannot block
  `sv-memory save`.

### Performance

- Make the memory conflict scan incremental (O(new memories × total) instead of O(N²)),
  cache tokenizations, and stop early once the insert budget is reached.
- De-N+1 `sv_mem_review` (single grouped relation-count query) and surface review-worthy
  memories first, bounded by `default_review_limit`.

[v0.1.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.1.0
[v0.2.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.2.0
[v0.3.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.3.0
[v0.4.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.4.0
[v0.4.1]: https://github.com/svtech-code/sv-memory/releases/tag/v0.4.1
[v0.5.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.5.0
[v0.6.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.6.0
[v0.7.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.7.0
[v0.8.0]: https://github.com/svtech-code/sv-memory/compare/v0.7.0...v0.8.0
[Unreleased]: https://github.com/svtech-code/sv-memory/compare/v0.8.0...main
