# Changelog

All notable changes follow [Conventional Commits](https://www.conventionalcommits.org/).
Releases are tagged `vX.Y.Z`; the CI pipeline builds and publishes them automatically.

## [Unreleased]

### Added

- **Aggregate Graph Report (`sv_graph_report` / `sv-memory graph report`)**: a new `GenerateGraphReport` in `internal/graph/report.go` builds a standalone `GRAPH_REPORT.md` overview from computed metrics — god nodes (via `TopDegreeNodes`, SQL aggregate excluding document/package nodes), top communities with auto-labels (via `LeidenDetectCommunities` + `DetectCommunityLabels`), surprising cross-community connections (via `FindSurprisingConnections`, bounded), hub threshold, and a deterministic suggested-questions section. Both the new MCP tool `sv_graph_report` and the CLI subcommand `sv-memory graph report` share it; the tool returns a bounded digest (path, bytes, counts). Output path is validated via `security.ValidateWritePath` and defaults to `GRAPH_REPORT.md`.

### Changed

- **Unified Graph Explore (`sv_graph_explore`)**: `sv_mem_context_pack` now accepts up to three comma-separated symbols — secondary symbols surface as surgical line-numbered source snippets and the shortest dependency path between the two most significant symbols is rendered as a call path (via `graph.ShortestPath`, bounded to 8 hops). A new `sv_graph_explore` MCP tool aliases `handleContextPack` with exploration-oriented wording. Single-symbol queries preserve the exact previous behaviour and output. `GetContextPack`/`RenderContextPack` were split into `resolveExploreSymbols`, `renderCallPath`, `renderExtraSnippets`, `renderMemories` and `renderChanges` helpers to keep cyclomatic complexity under the lint gate.
- **Graph as Grep/Read Substitute in the Injected Protocol**: the "Graph Inspection" block of the injected protocol template is now "Graph — use it instead of grep/read on synced code", leading agents to call `sv_graph_explore`/`sv_mem_context_pack` first (returned source counts as already read) and listing concrete anti-patterns (don't re-verify graph results with grep, don't grep/read first on indexed code, don't hand-reconstruct a flow). Quick Reference now lists `sv_graph_explore`. Synced to `AGENTS.md`, `documentation/spect.md`, `documentation/spect_ES.md`, the sv-memory `SKILL.md`, and guarded by a new `TestProtocolTemplateGuidesGraphFirst` test.

## [v0.17.1] - 2026-08-29

### Added

- **Interactive Agent Selection in `sv-memory init`:** `sv-memory init` in terminal sessions now prompts users interactively via a multi-select form to choose which AI assistants to configure (Claude Code, Antigravity, Cursor, Windsurf, OpenCode, Codex), pre-selecting already detected assistants and avoiding unwanted configuration files on fresh repositories. Added flags `--agent <name>`, `--agents <a,b,c>`, `--all`, and `--skip-setup`.
- **Automatic Git Post-Commit Hook Installation on Init:** `sv-memory init` now automatically checks for Git repository presence (`.git` directory or submodule/worktree file) and installs the post-commit hook (`.git/hooks/post-commit`) out of the box for automatic passive memory capture.
- **Unified Platform Status Across Commands:** Added `PlatformCursor` and `PlatformWindsurf` to `HookEngine` so that `sv-memory hooks status`, `sv-memory setup`, and `sv-memory hooks install` uniformly manage and report statuses across all 7 supported integration targets (Claude Code, Antigravity, Cursor, Windsurf, OpenCode, Codex, Git).

## [v0.17.0] - 2026-08-29

### Added

- **Smart All-in-One Project Initialization & Reconciliation (`sv-memory init`):** Enhanced `sv-memory init` to automatically configure and reconcile AI assistant integrations in one command — creating skills (`.agents/skills/`, `.opencode/skills/`), hooks (`.agents/hooks/`, `.claude/hooks/`), plugins, and granting permissions for all 34 MCP tools. In existing projects, `init` auto-detects configured assistants and refreshes their assets idempotently.
- **Auto-Derived Topic Keys in `sv_mem_save`:** If `topic_key` is omitted when saving semantic memories (`decision`, `standard`, `architecture`, `bugfix`, `qa`, etc.), `sv_mem_save` automatically derives a stable `category/kebab-case` key via `SuggestTopicKey` to enable upsert semantics without requiring a separate `sv_mem_suggest_topic_key` call.
- **Unified Single-Call Session End (`sv_mem_session_end`):** `sv_mem_session_end` now auto-resolves the active session ID if omitted, and accepts optional inline summary arguments (`summary`, `accomplished`, `goal`, `discoveries`, `next_steps`, `files`) to save structured accomplishments and mark the session completed in a single roundtrip.
- **Antigravity Native Skill & Setup Wiring:** Embedded `antigravity-skill.md` template with YAML frontmatter for progressive on-demand disclosure; updated `HookEngine.installAntigravity` and `sv-memory setup antigravity` to automatically create and maintain `.agents/skills/sv-memory/SKILL.md`.
- **Streamlined Protocol & Single-Call Context Recommendation:** Streamlined the injected `AGENTS.md` protocol rules to emphasize `sv_mem_context_pack(path=...)` as the primary, bounded tool before inspecting or editing code.
- **Streamlined Post-Update Workflow:** Updated `sv-memory update` to guide users to run `sv-memory init` across existing repositories to reconcile skills, hooks, and new MCP permissions with zero manual setup.

## [v0.16.0] - 2026-08-21

### Added

- **Go Native AST Extraction & Call References:** Implemented native AST parser using standard library `go/parser` and `go/ast` (`internal/graph/extractor/go.go`), enabling exact symbol, import, rationale, and call reference extraction (`confidence="EXTRACTED"` with `L<line>:<col>` coordinates) for Go source files, eliminating regex fallback and false positives.
- **Surgical Source Code Snippets in Context Pack:** Enhanced `GetContextPack` and `RenderContextPack` in `internal/memory/contextpack.go` to extract and include bounded source code snippets for resolved function/class symbols (up to 60 lines with syntax highlighting), eliminating the "discovery tax" and extra file read operations for AI coding agents.
- **Transitive Blast Radius Impact Analysis:** Added `CalculateBlastRadius` in `internal/graph/blast_radius.go` and integrated multi-hop upstream consumer traversal (depth 1 to 3 with hub hotspot markers) into `ContextPack` (`internal/memory/contextpack.go`), giving agents immediate visibility into downstream ripple effects before proposing or making changes.
- **Auto-Freshness Staleness Probe in Context Resolution:** Connected `SyncGraphIfHasChanges` into `GetContextPack` (`internal/memory/contextpack.go`) to automatically refresh modified AST symbols and call edges via lightweight mtime probing without manual sync or perceptible latency.
- **Live Task Progress Tracking & Execution Checklist:** Implemented `ParseTaskProgress` in `internal/memory/tasks.go` and surfaced real-time task checklist completion metrics (`completed/total percent`) in `ContextPack` (`internal/memory/contextpack.go`), `sv_propose_spec` & `sv_validate_decision` responses (`internal/mcp/tools_spec.go`), and `sv-memory specs list` (`cmd/sv-memory/cmd_specs.go`), providing AI agents with live visibility into pending tasks during coding sessions.
- **Test Suite Enhancements:** Added `TestContextPackExtractsSurgicalSnippet` / `TestContextPackComputesBlastRadius` / `TestContextPackAutoSyncsIfStale` / `TestContextPackSurfacesTasksProgress` in `internal/memory/contextpack_test.go`, `TestParseTaskProgress` in `internal/memory/tasks_test.go`, and `TestCalculateBlastRadius` in `internal/graph/blast_radius_test.go`.

## [v0.15.0] - 2026-08-21

### Added

- **Tri-Factor Relevance Ranking in Memory Search:** Combined FTS5 BM25 with importance weighting (pinned boost, category tiering for decisions/architecture/standards, revision count) and recency decay (`triFactorScoreExpr` in `internal/memory/memory_util.go` and `internal/memory/memory_search.go`), ensuring high-priority architectural decisions and fresh memories rank above stale trivial entries.
- **AST / Code Staleness Detection for Memories:** Added `DetectStaleMemoryBindings` in `internal/graph/stale.go` and wired stale memory binding diagnostics (`where_path` references to deleted or missing files) into `DiagnoseGraph` and `GraphDiagnosticReport` in `internal/graph/diagnostics.go`.
- **Git Post-Commit Hook for Passive Capture:** Added `PlatformGit` support in `internal/hook/hook.go` with automated post-commit hook script installation (`sv-memory hooks install --platform git`), extracting commit hash, message, branch, author, and changed files without delaying git workflow.
- **CLI Passive Capture Command (`sv-memory capture`):** Added `capture` command to CLI for registering commits and notes into persistent memory and git chunks seamlessly.
- **Pure-Go Local Vector Engine & Hybrid Search (`SearchMemoriesHybrid`):** Implemented subword/n-gram vector embeddings, L2 normalization, and cosine similarity in `internal/memory/semantic_vector.go` to provide resilient hybrid search (`match_mode="hybrid"`), ensuring agents find relevant architectural decisions even when terms vary or inflections differ.
- **Test Suite Enhancements:** Added `TestTriFactorScoreRanking` in `internal/memory/memory_test.go`, `TestStaleMemoryBindings` in `internal/graph/diagnostics_export_test.go`, `TestInstallGitHook` in `internal/hook/hook_test.go`, `TestCaptureCmdRegistered` in `cmd/sv-memory/cmd_coverage_test.go`, and `TestExtractSubwordsAndCosineSimilarity` / `TestSearchMemoriesHybridSemanticFallback` in `internal/memory/semantic_vector_test.go`.

## [v0.14.0] - 2026-08-18

### Fixed

- **AST call extraction no longer panics on top-level calls:** `extractASTCallEdges` used to dereference a nil caller when a call appeared at the top level of a JS/TS/Python/Rust/Java file (before its first function/class, or in a file with none) and matched a cross-file callee — crashing `sync`/`graph rebuild`. The containing-caller guard now runs *before* callee resolution, and `resolveCalleeNode` nil-guards its caller defensively. New regression test `TestSyncGraphASTCallTopLevelNoPanic` proves the top-level call is skipped without panicking.
- **`TestInitDBCreatesPrivateFile` no longer fails on Windows:** the POSIX permission assertion is now skipped on Windows, where `os.Stat().Mode().Perm()` always reports `0666` and `os.Chmod` only toggles the read-only attribute — the owner-only `0600` restriction is only meaningful and verifiable on unix-like systems.
- **Compaction is atomic (audit P1):** the synthesis row is now inserted *before* the older topic-key entries are soft-deleted and a failed insert aborts the whole transaction. Previously the soft-deletes were committed even when the synthesis insert failed, silently and permanently losing every memory of the topic. New regression test `TestCompactMemoriesRollsBackOnSynthesisFailure` forces the insert to fail via a trigger and verifies the originals survive.
- **Path-traversal resistance for imported memory IDs and spec slugs (audit P2):** memory IDs arriving via `ImportJSON`/git-sync chunks are validated against a conservative `[A-Za-z0-9_-]{1,64}` rule before reaching the DB — an unsafe id would otherwise become a chunk/vault file name and escape `.sv-memory/chunks` on the next sync. `CreateChange` and `ImportChangeFromMarkdown` now reject slugs containing absolute paths or traversal segments by reusing the existing `validateCapabilityPath` guard. The `validMemoryID` character check was later simplified to satisfy staticcheck's `QF1001` (no De Morgan negation) with a dedicated `TestValidMemoryID`.
- **Hardened atomic writes and database permissions (audit P3):** `atomicWriteFile` now writes to a unique `os.CreateTemp` sibling (owner-only 0600), `fsync`s it before the rename, and `fsync`s the parent directory afterwards — no more predictable `<path>.tmp` collision for concurrent writers, no symlink-clobber window, no crash-truncated file renamed into place. The SQLite database file is pre-created with 0600 (`ensurePrivateFile`) so the memory store and its WAL/SHM companions are no longer world-readable on multi-user machines (new `TestInitDBCreatesPrivateFile`).

### Changed

- **Schema vocabulary wired (audit P4):** the `section`/`table`/`view`/`index`/`type`/`rationale` node-type constants in `internal/graph/schema` are now referenced by the regex and tree-sitter extractors (plus `graph.go`) instead of scattered string literals; the never-emitted `NodeTypeDecision`, `NodeTypeRule`, `EdgeAffects`, and `EdgeConstrains` constants were removed along with the outdated package doc.
- **Test-only exported API removed (audit P5):** dropped the symbols that had no production caller — `MergeToJSON`/`MergeFromJSON`, `GetCommunityInfo`/`CommunityInfo`, `DetectBridgeNodes`/`BridgeNode`, `GraphCache.Clear/Len/Entries/InvalidateAll`, `SetMemoryChangeID` (the tx-level `execMemoryChangeLink` remains), and `MergeChangeDeltas` (callers use `MergeDeltas` directly). `DetectCommunities` (label propagation) is kept as the documented, benchmarked alternative to Leiden. Zero behavior change.
- **Helper deduplication (audit P6):** the duplicate line-number helpers in the regex extractor are merged into one (with the 80-char safety cap); the dead `parseSymbols` extractor-type branch in `graph.go` is collapsed to a single unconditional `GetExportsCount`; the CLI specs list uses the shared `memory.TruncateText` instead of a local `truncateTitle`; and the three identical Obsidian footer writes now share `obsidianFooter()`. Zero behavior change.
- **MCP tool count corrected to 34 (audit P7):** README, setup/getting-started guides, and the spec document (EN + ES) previously claimed "31 MCP tools"; the server actually registers 34, and the stale `'sv-memory specs new'` hint was dropped from `sv-memory specs list`.


- **Dead code removed (audit Fase A):** dropped the unused `releaseAsset` struct and `releaseInfo.Assets` field from the updater (the asset URL is derived from tag + platform, never read from the GitHub payload), the unused `version` parameter from `config.ShowBanner`, and the unused `execPath` parameter from `setupOpenCode`/`setupAntigravity`/`setupCodex` (`setupCursor`/`setupWindsurf` keep theirs — they pass it to `ConfigureCursor`/`ConfigureWindsurf`). Zero behavior change.
- **Search deduplicated (audit Fase B):** `SearchMemoriesCompactScoped` and `SearchMemoriesByPaths` (~80% identical query builders) now delegate to a single shared core `searchMemoriesCompact` that takes an optional `paths` set; the public APIs are thin wrappers with unchanged signatures, so the graph-aware community search and the plain scoped search can no longer drift apart. Zero behavior change.
- **Graph upserts and atomic writes deduplicated, schema vocabulary wired in (audit Fase C):** the three `graph_nodes` upserts (`incremental.go`, `memory_link.go`, `spec_link.go`) and the four `graph_edges` inserts now share single `upsertGraphNode`/`insertGraphEdge` helpers (`internal/graph/upsert.go`) that run on either `*sql.DB` or `*sql.Tx`; the four atomic tmp+rename write sites (JSON export, git-sync chunk + monolith, spec mirror) now use one `atomicWriteFile` helper; and the `configure` wizard theme now delegates to the exported `tui.Theme()` instead of a 96%-identical local copy. As part of the dedup, the graph node/edge vocabulary is now the single source of truth: 13 of the `schema` constants are wired in place of scattered literals (including a new `NodeTypeSQL` for the scanner's `.sql` nodes); the read-only multiline SQL filter in `memory.go` and the tree-sitter/regex extractor keep their literals (deferred — different package vocabulary, low drift risk). Zero behavior change.
- **Low-level audit cleanups (Fase D):** the inline two-format date parse in `ScanConflicts` now reuses `parseTimeOrNow`; `escapeCypherStr` (Cypher export) now escapes backslash, single quote, and the control characters (`\n \t \r \b \f`) in addition to the double quote so a crafted label cannot break out of the string literal; the graph wiki now escapes node/community labels (`escapeWikiLabel`) so a label containing `|` or HTML cannot break a markdown table or inject markup; and the change-field length cap now shares the single `maxFieldChars` constant (was a duplicate `maxChangeFieldChars`), while the mcp display-truncation constant was renamed `maxFieldTruncateChars` to stop colliding with the validation cap. Zero behavior change.
- **Spec commit is now atomic (audit Fase E):** `sv_commit_spec` persists the decision memory, the memory→change link, the capability delta merge, and the `applied` lifecycle stamp in a single SQL transaction via the new `memory.CommitChangeAtomic`. Either all of the authoritative state lands or none does — a delta merge that fails (ADDED of an existing requirement, RENAMED of a missing one) now leaves no half-committed decision memory or mutated capability behind, so the delta can be fixed and the commit retried cleanly. The graph wiring (rationale_for/implements edges) and `conflicts_with` judgments remain derived, best-effort work performed after the transaction commits, so a graph hiccup never rolls back the authoritative commit. The internal save/merge/status paths were factored into tx variants (`saveMemoryInTx`, `prepareMemoryForSave`, `mergeDeltasInTx`, `execChangeStatusUpdate`, `execMemoryChangeLink`) shared with their standalone `SaveMemory`/`MergeDeltas`/`UpdateChangeStatus`/`SetMemoryChangeID` entry points, keeping the public API unchanged. New test `TestCommitSpecAtomicOnMergeFailure` proves a failed merge leaves the memory store, the capability state, and the change status untouched.
- **`SaveMemory` slimmed to an orchestrator over a new `internal/memory/save.go`:** the ~140-line save monolith is now a clean three-path flow — validate/sanitize, then delegate to `upsertByTopicKey` (topic-key upsert, `revision_count++`), `bumpDuplicate` (24h rolling-window dedup), and `insertMemory` (fresh insert) — each operating on the same single-writer transaction so the concurrency guarantees are unchanged. Zero behavior change, zero API change; each branch is now independently testable and readable. Codebase guide (EN/ES) updated.
- **`mcp.go` split by responsibility (833 → ~546 lines):** the git-sync orchestration (`syncPathStat`, `maybeSyncFromGit`, `scheduleSync`, `flushPendingSync`), the graph lazy-load/relink plumbing (`computeCentralityIfMissing`, `getOrLoadGraph`, `relinkMemoryRationales`, `relinkSpecCapabilities`), and the response/token-budget helpers (`configuredInt`/`configuredBool`, `truncateField`, `resolveTokenBudget`, `truncateToTokenBudget`, `respond`, session token ledger) moved to `server_sync.go`, `graph_load.go`, and `respond.go`. All methods stay on the same `Server` struct in the same package, so the change is a pure reorganization with zero runtime/API impact; `mcp.go` keeps the core (debug helpers, `AllTools`, `Server` struct, git cache, `StartServer`, `NewServer`) and every `mcp.NewTool(` registration stays put, so the `TestAllToolsMatchesRegisteredTools` guard remains green. Codebase guide (EN/ES) updated.

## [v0.13.1] - 2026-08-15

### Fixed

- **Manual judgments are persisted as `judged`, not `pending`:** `SaveJudgment` now writes `status='judged'` (the schema default was `pending`), so an explicit `sv_mem_judge` relation no longer inflates the Auto-Boot "pending conflicts" hint and, crucially, is no longer silently deleted by a later semantic conflict re-scan (`ApplySemanticVerdict` only clears `status='pending'` rows). This preserves the human/agent judgment against an LLM verdict overwriting it.
- **Spec commit is safer under partial failure:** `handleCommitSpec` now persists the decision memory and links it to the change *before* merging the delta requirements into the capability state. If the merge fails (ADDED of an existing requirement, RENAMED of a missing one), the capability state is left untouched and the commit can be retried once the delta is fixed — previously the merge ran first and a later save failure left the capability mutated with no way to re-commit cleanly.
- **The spec lifecycle now transitions as documented:** `sv_propose_spec` advances the change `draft → proposed` after wiring it, and `sv_validate_decision` advances it `proposed → validated` when the pre-flight verdict is PASS/WARN (a BLOCK keeps it proposed; terminal states are never re-stamped). `sv-memory specs list` now reports `proposed`/`validated` instead of always `draft`, matching the `draft → proposed → validated → applied (→ archived) | rejected` cycle described in AGENTS.md.

### Changed

- **`sv_graph_query` and `sv_graph_path` accrue the session token ledger:** both tools now route through the shared `s.respond`, so their output counts toward `sv_mem_stats` "Estimated tokens injected this session" and the shared `truncateToTokenBudget` is used instead of a bespoke truncation block (duplicate implementation removed).
- **Field-length validation centralized:** the memory (`SaveMemory`/`UpdateMemory`) and change (`CreateChange`/`UpdateChange`) length caps now live in shared `validateMemoryFields`/`validateChangeFields` helpers in `memory_util.go`, removing four copies of the same validation blocks (single source of truth, no drift).

### Testing

- `TestGetTimelineCompact` (ordering, central-observation exclusion, soft-deleted exclusion, unknown observation error) and `TestDeleteMemoryHard` (row removal + relation cascade + not-found) were added for previously uncovered paths.
- `TestSaveJudgmentReplacesDuplicate` now asserts the persisted `status='judged'`, and `TestAutoBootBundleSurfacesPendingConflicts` generates its pending conflict through `ScanConflicts` (the natural flow) instead of `SaveJudgment`.
- `TestGraphQueryPathAccrueTokenLedger` asserts graph query/path calls add to the session token ledger.

## [v0.13.0] - 2026-08-15

### Added

- **OpenSpec-style delta requirements over the spec engine (Phase 1 — schema & parser):** the propose → validate → commit cycle now carries structured requirements instead of free text only, matching the OpenSpec delta format. Two additive migrations:
  - **v15 `spec_requirements`** — the per-change delta rows (`delta_op` ADDED/MODIFIED/REMOVED/RENAMED, `capability_path`, `requirement`, narrative `body`, `rename_to`, RFC 2119 keywords and scenarios as JSON, parse-order `sort_order`) with an FTS5 index (`spec_requirements_fts`, content-synced via triggers) so requirement deltas are searchable; and **`spec_capabilities`** — the materialized current state of each capability (`UNIQUE(project_id, capability_path, requirement)`).
  - **v16 `changes.capability_path`** — each change targets a single capability, defaulting to its slug, with a `SetChangeCapabilityPath` override.
  - `internal/memory/requirements.go` implements the lenient hand-rolled parser (matching the existing `ParseChangeMarkdown` convention, zero new dependencies): `## ADDED/MODIFIED/REMOVED/RENAMED Requirements` sections, `### Requirement:` (3 `#`) and `#### Scenario:` (4 `#`) blocks, GIVEN/WHEN/THEN/AND step bullets (bolded or bare, colon inside bold markers handled), `## RENAMED Requirements` FROM/TO pairs, and RFC 2119 keyword extraction (MUST/SHALL/SHOULD/MAY + `NOT` negations as distinct whole-word matches). `DeltasToMarkdown` renders the inverse for round-trip export. All covered by unit tests (sections, renames, lenient malformed input, round-trip, keyword extraction, tables exist).
- **Delta-spec CRUD, merge semantics, and capability mirror (Phase 2):**
  - `ReplaceChangeRequirements`/`ListChangeRequirements`/`LoadChangeDeltas` persist and reconstruct a change's deltas (transactional, parse-order preserved).
  - **`MergeDeltas`** applies OpenSpec semantics to the capability current state: RENAMED is applied first (so a MODIFIED naming the new header resolves), ADDED is strict (an existing requirement name errors — use MODIFIED), MODIFIED replaces the whole requirement block (scenarios not listed are dropped), REMOVED is lenient. `MergeChangeDeltas` drives the commit path.
  - The change mirror (`WriteSpecMirror`) now embeds the delta sections after the proposal body, and `ImportChangeFromMarkdown` reconciles them back bidirectionally (delta sections are stripped before the change-body parse so `## ADDED Requirements` never leaks into Tasks/Design; an empty delta reconciles to a clear). A new **`specs/capabilities/<cap>/spec.md`** tree projects the materialized current state (OpenSpec main-spec style) with orphan-directory cleanup when a capability is retired. The CLI gains `sv-memory specs capabilities` to list current-state capabilities and requirement counts.
- **Capabilities wired into the knowledge graph and context surfacing (Phase 3):**
  - `internal/graph/spec_link.go` makes capabilities first-class graph citizens using the schema vocabulary that was reserved in v0.12.0: a `spec` node per capability (`id` `spec:<cap>`, stable across rebuilds), `implements` edges from the code node at the change's `where_path` to the capability, and from the committed decision memory node to the capability. `RelinkSpecCapabilityEdges` + `ActiveSpecCapabilityRefs` re-create the wiring after a full graph rebuild (mcp `sv_graph_sync` and CLI `sv-memory graph rebuild`), exactly like the memory `rationale_for` relink. Decision linking is best-effort when the memory node does not exist.
  - **`sv_mem_context_pack` now surfaces "Capabilities implemented here"** for the resolved path, each with a bounded requirement summary (max 10 capabilities, 5 requirement names, total count), so the agent sees the applicable contract for the code it is about to touch without reading the full spec mirror — one bounded call, token-efficient.
  - **Tools:** `sv_propose_spec` accepts `requirements` (OpenSpec delta Markdown) and `capability_path` (single capability per change), storing the deltas and wiring the capability into the graph; `sv_validate_decision` validates the deltas against the current capability state (RFC 2119 presence warning; MODIFIED scenario-drop warning, silent when the base requirement is absent or renamed by the same change); `sv_commit_spec` merges the deltas into `spec_capabilities` (a merge conflict — ADDED of an existing name, RENAMED of a missing one — aborts the commit so the delta is fixed first) and links the committed decision to the capability via `implements`.
  - The injected `AGENTS.md` protocol (and tool quick reference) documents the requirements flow; the tool registry descriptions were updated accordingly. No new MCP tools were added (the three spec tools gained parameters only), so static allow-lists are unchanged.
- **Post-update re-wiring documented:** `AGENT-SETUP.md`/`AGENT-SETUP_ES.md` gained an "Updating sv-memory (post-update)" section and `README.md`/`README_ES.md` a matching note: after `sv-memory update` (which only replaces the binary) you must re-run `sv-memory setup <agent>` (or `--all`) so newly added MCP tools enter the static allow-lists, the injected protocol and OpenCode `SKILL.md` are refreshed, and then restart the assistant — `init`/`hooks install` are not needed again for already-configured projects.

## [v0.12.0] - 2026-08-15

### Added

- **Change lifecycle schema (spec-driven decision engine, Phase 1):** new `changes` table models a proposal through the decision cycle (`draft → proposed → validated → applied → archived/rejected`) with a project-unique kebab-case `slug`, free-text `what/goal/design/tasks` fields, and `where_path` for the AFFECTS edge wiring. A nullable `change_id` column on `memories` (ON DELETE SET NULL) lets a committed decision be traced back to the change that produced it without orphaning the memory. The `internal/memory` package gains the `Change` domain type plus `CreateChange`, `GetChange`/`GetChangeBySlug`, `ListChangesByStatus`, `UpdateChangeStatus`, and `SetMemoryChangeID` (full CRUD + lifecycle transition validation). The graph schema vocabulary is extended with `spec`/`decision`/`rule` node types and `affects`/`constrains`/`implements` edge types (values are free-form TEXT, no destructive migration). Migration v14 is additive and idempotent, following the existing migration pattern.
- **Spec-driven decision engine (Phase 2):** the propose → validate → commit cycle is now live over the change lifecycle, turning sv-memory into an active governance layer instead of a passive store. Three new MCP tools (`sv_propose_spec`, `sv_validate_decision`, `sv_commit_spec`) let the agent check a proposal against the project's rules and invariants before writing code:
  - **`sv_propose_spec`** registers a change (draft) and runs a deterministic **pre-flight check** (FTS5 token match + Jaccard vs `conflict_threshold`, default `0.45`) over `standard`/`decision`/`architecture` memories. A **pinned** rule at or above the threshold → **BLOCK**; a non-pinned overlap → **WARN**; otherwise **PASS**. Zero LLM cost in the default path.
  - **`sv_validate_decision`** re-checks a proposal after edits, with an opt-in `semantic='true'` agent re-ranking (single batched call, fails open to the deterministic verdict when the agent is unavailable — same infrastructure as `sv_mem_search`/`sv_mem_conflicts` semantic modes).
  - **`sv_commit_spec`** promotes a change into a durable `decision`/`standard` memory (`topic_key` `decision/<slug>`), links it via `change_id`, wires the `rationale_for` edge, records `conflicts_with` relations for any flagged rules, and stamps the change `applied`. A pre-flight **BLOCK** rejects the commit unless `force='true'` explicitly overrides the invariant.
  - **`sv_mem_context_pack`** gains an `include_changes='true'` parameter that appends active changes affecting a path, and the Auto-Boot bundle surfaces a zero-cost `📋 Active changes: N` hint when non-terminal changes exist. The `sv-memory context` CLI gains a matching `--include-changes` flag.
  - New `PreflightCheck`/`SemanticPreflight` (deterministic + fail-open semantic), `ChangeStats`, and a path-scoped `changesForPath` recall helper, all covered by unit tests (preflight PASS/WARN/BLOCK fixtures, commit gate, force override, context-pack change surfacing).
- **Spec mirror (Phase 3):** the change store now projects itself as human-readable Markdown under `.sv-memory/specs/`, keeping the SQLite store authoritative while giving humans (and plain-text tools) a git-versioned mirror with no drift:
  - **`sv-memory specs export`** writes every active change to `.sv-memory/specs/changes/<slug>.md` and moves archived/rejected changes to `.sv-memory/specs/archive/<date>-<slug>.md` (OpenSpec-style layout). Orphaned mirrors are pruned so the mirror never lags the DB.
  - **`sv-memory specs import <slug>`** reconciles a human-edited mirror back into the authoritative store (only the edited fields; identity is never changed; a mirror can never create a change).
  - **`sv-memory specs list`** shows each change's status and mirror state; **`sv-memory specs archive <slug>`** moves an applied change to archived and relocates its mirror.
  - The mirror is written automatically by the incremental Git sync (`SyncToGit`), so proposals stay in sync with zero manual steps and best-effort (a mirror failure never fails a memory sync). New `ChangeToMarkdown`/`ParseChangeMarkdown` (lenient, round-trip-safe), `WriteSpecMirror`, `ImportChangeFromMarkdown`, `UpdateChange`, and `ListSpecMirrors`, all covered by unit tests (round-trip, archive relocation, orphan pruning, phantom-import rejection).
- **Injected protocol now carries the spec-driven cycle:** the `protocolTemplate` in `internal/protocol/protocol.go` (injected by `sv-memory init`, `sv-memory setup <agent>`, and PreToolUse hooks into `AGENTS.md`/`.cursorrules`/`.windsurfrules`) gained the **Spec-Driven Decision Cycle** section and the `Decision Engine` + `Spec Mirror (CLI)` tool-reference lines. Projects initialized with the new binary now instruct their agents to consult context, propose, validate, and commit specs — previously the injected rules omitted the new tools, so agents in freshly-initialized projects would not discover `sv_propose_spec`/`sv_validate_decision`/`sv_commit_spec`. The OpenCode skill (source `internal/hook/scripts/opencode-skill.md`, plus the installed `.opencode/skills/sv-memory/SKILL.md`) was extended with the same decision-cycle instructions, and the spec §8 template copy and CODEBASE-GUIDE Flow 6 (`include_changes`) were re-synced. A guard test (`TestProtocolTemplateContainsSpecDriven`) fails if the template ever loses the spec-driven markers again, and a manual `sv-memory init` verification confirmed the injected block matches the template byte-for-byte and is idempotent.

## [v0.11.0] - 2026-08-15

### Added

- **Pending-conflict hint in the Auto-Boot bundle:** when there are unresolved decision conflicts (`memory_relations` with status `pending`), `sv_mem_session_start` appends a one-line `⚠ Pending memory conflicts` hint to the bundle so the agent reviews them before relying on either side. It is only emitted when conflicts exist, so the token cost is zero when the store is healthy.
- **Actionable memory hygiene in `sv_mem_review` (`action=prune_stale`):** the review tool can now prune stale transient memories instead of only reporting them. By default it targets ephemeral categories (journal, qa, discussion, idea) not seen/created within `prune_stale_days` (default 90) and is a **dry run** unless `apply='true'` is passed; pruning soft-deletes (recoverable) and syncs the change to Git. Pinned memories and durable knowledge (decisions, standards, architecture, postmortems, bugfixes) are never touched unless explicitly listed in the `category` parameter. This closes the review loop: stale notes that would otherwise keep polluting `sv_mem_search` and inflating token usage can now be cleaned safely.
- **Goal-aware Auto-Boot bundle (`sv_mem_session_start`):** the session-start context bundle now ranks its per-section candidates (architectural decisions, standards, recent work, postmortems, Q&A) by relevance to the provided `goal` instead of pure recency — pinned memories first, then keyword overlap with the goal, then recency (deterministic, the default, zero LLM cost). A new opt-in `semantic=true` re-ranks the combined candidate pool by meaning via the configured agent CLI (one batched call, reusing the semantic-recall infrastructure), appending a short relevance reason to each surfaced rationale and failing open to the deterministic keyword ranking when the agent is unavailable. Pinned memories surfaced in the previous-session section are deduplicated from the category sections.
- **Opt-in semantic recall in `sv_mem_search`:** a new `semantic=true` parameter re-ranks the keyword (FTS5) candidates with the configured agent CLI by meaning, so a query like "how did we fix the auth timeout" surfaces a memory titled "session expiration TTL" that keyword search would miss. It is token-disciplined: the FTS5 pass pre-filters to a bounded candidate pool (up to 30), fields are truncated, a single batched agent call returns a JSON relevance list, and the output is capped back to `limit` with a one-clause relevance reason per hit. Uses the same agent-CLI infrastructure as `sv_mem_conflicts semantic=true` (`$SV_MEMORY_SEMANTIC_AGENT`, default `claude`) and is fully fail-open — when the agent is unavailable the keyword results are returned unchanged with a note, so the default deterministic/local search is never degraded.

### Changed

- **`sv_graph_query` is now token-efficient by default:** it emits a compact, LLM-friendly textual edge list (`source →[rel]→ target` with confidence) instead of the token-heavy Mermaid diagram. The Mermaid rendering is still available opt-in via a new `mermaid=true` parameter, and the node list plus the edge-confidence breakdown are kept. This roughly halves the output size of the common "what does X import/depend on" query, directly reducing context tokens for the agent. The `tokenBenchmark` block was dropped from this tool's output.

### Fixed

- **Critical data-integrity and security bugs (Phase A):** the legacy graph migration no longer drops `graph_nodes`/`graph_edges` without recreating them (it now rebuilds the composite-PK schema and surfaces errors); `ValidateWritePath` detects symlink escapes for non-existent write targets; the Obsidian export uses the validated vault path; `projects consolidate` rejects self-merges; topic-key upserts preserve the original `created_at`; new memories store `last_seen_at` instead of a zero-time sentinel that read back as ~738000 days stale; Claude Code hook install/uninstall preserve user hooks instead of clobbering them; the protocol marker injector rewrites only the block region instead of the whole file; hook scripts use a portable hash instead of the broken `md5 -qs`; and lazy incremental graph sync no longer drops cross-file `calls` edges.
- **Medium correctness bugs (Phase B):** FTS5 tokens are quoted in `FindSimilarMemories` (no more `-`/`_` operator injection); timestamps are compared by value instead of fragile string formats; the 24-hour dedup window compares against a Go-computed cutoff instead of UTC `datetime('now')`; `SaveJudgment` replaces duplicate relations; `SaveMemory`/`DeleteProject`/`DeleteMemory` are transactional and clean derived data; `composer.json` dependencies parse from `require`/`require-dev`; manifest files are probed for staleness; stdlib subpackages (`net/http`) are recognized; `sv_graph_god_nodes` no longer serves stale centrality; the `sv_mem_context` limit is honored; and `NewDBPool` propagates reader-open errors instead of degrading to the writer as a reader.
- **Graph resolution (G2/G3):** `FindNode` is deterministic (the same query resolves to the same node across runs) and Python relative imports (`from .models import X`) resolve to project files; external package nodes (`pkg:*`) no longer crowd out real code hubs in `TopDegreeNodes`/`sv_graph_god_nodes`.
- **CI timing fix:** the incremental git-sync watermark is captured at the start of the sync and compared inclusively (`>=`), so the `-cover`-induced timing race that flaked `TestSyncToGitIncremental` in CI no longer drops changed-memory chunk rewrites.

## [v0.10.0] - 2026-08-12

### Added

- **`sv-memory setup <agent>` unified agent integration (multi-agent parity with Engram's `engram setup`):** one-shot wiring of MCP server config + hooks/skills/plugins + protocol injection (`AGENTS.md` / `.cursorrules` / `.windsurfrules`) + MCP tool permissions for six agents — `claude-code`, `opencode`, `cursor`, `windsurf`, `antigravity`, `codex`. `sv-memory setup` (no args) is a read-only per-agent status table; `setup <agent>` is idempotent; `--all` wires every agent; `--strict` installs strict hooks. Cursor and Windsurf are now auto-configured at project level (`.cursor/mcp.json`, `.windsurf/mcp_config.json`) instead of manual instructions, and Claude Code gains a project-local `.mcp.json` fallback when the `claude` CLI is absent.
- **Claude Code lifecycle hooks:** `sv-memory hooks install` and `sv-memory setup claude-code` now install, alongside `PreToolUse`, four lifecycle hooks under `.claude/hooks/` — `SessionStart` (call `sv_mem_session_start`), `SessionEnd` (close the session), `PreCompact` (save a summary right before context compaction, enabling context recovery), and `SubagentStop` (persist durable subagent findings) — registered in `.claude/settings.json` in the official array+matcher format. Uninstall and status detection cover both the new array format and the legacy flat `preToolUse` entry.
- **Native OpenCode TypeScript plugin:** `sv-memory setup opencode` / `hooks install` now writes `.opencode/plugin/sv-memory.ts` next to the existing `SKILL.md`. The plugin registers the `sv_memory_context` tool (context pack for a file/package/symbol via the `sv-memory context` CLI) without needing MCP approval. The plugin is typechecked against `@opencode-ai/plugin` 1.18.14.
- **`documentation/AGENT-SETUP.md` + `AGENT-SETUP_ES.md`:** per-agent setup guide (MCP config, hooks, plugins, compaction survival) for all six supported agents, linked from the READMEs and getting-started guides.
- **`sv_mem_capture_prompt` MCP tool (Engram `mem_save_prompt` parity):** captures the user's prompt as a local observation attached to a session, so future sessions have context about user goals after compaction. Secrets are redacted before write and empty prompts are rejected. Prompts live in a new `user_prompts` SQLite table (migration v13) with an FTS5 index; they are local-only (not git-synced) in this phase. Recoverable via `sv_mem_context` (recent prompts of the last session, title-previewed to 140 chars) and counted by `sv_mem_stats` (`Total user prompts`).
- **`sv_mem_merge_projects` MCP tool (Engram `mem_merge_projects` parity, admin):** merges project name variants into a single canonical project — moves all memories, sessions, relations, and graph data from `from` into `to`, then deletes the source project. Mirrors the `sv-memory projects consolidate <from> <to>` CLI. Tool count grows 29 → 31; docs (EN/ES), protocol, skills, and CHANGELOG synced.
- **`sv_mem_judge.reason` is now capped at 200 chars:** matching Engram's `mem_compare.reasoning` token discipline, so verdict annotations that later search results surface stay token-efficient (`judgeReasonMaxChars`, applied via the shared rune-safe `TruncateText`).
- **AST-precision `calls` edges (graph engine):** `calls` edges are now extracted per file by preferring the tree-sitter AST (`call_expression` / `call` / `method_invocation` / `function_call_expression` nodes) with confidence `EXTRACTED` and a precise `L<line>:<col>` source location, resolving each call site against the project's function/class nodes (same file first, then a unique cross-file match within the language group). A new optional `extractor.CallRefExtractor` interface exposes `ExtractCallRefs`, implemented by `TreeSitterExtractor` for Python, JS/TS, Java, PHP, Ruby, Rust, CSS, and HTML. Files without AST call coverage (Go — upstream parser stack-overflow workaround, Lua, Markdown, shell, Vue/Svelte/Astro script blocks) keep the tokenize heuristic (`INFERRED`). The AST path does not capture identifiers inside strings or comments, eliminating a class of false positives the heuristic produced. Tests cover the extractor (call refs with 1-based line/col, no string/comment capture, Go fallback) and graph integration (EXTRACTED cross-file Python edge, and a mixed Go+Python project proving the heuristic still covers Go).
- **`documentation/CODEBASE-GUIDE.md` + `CODEBASE-GUIDE_ES.md`:** a codebase tour focused on data flows, complementing the reference spec — package map, seven key flows (saving a memory, graph query, graph build/refresh with AST vs heuristic call edges, session lifecycle, conflict detection/judgment, context pack, chunked git sync), plus "where to add a new MCP tool / new language" walkthroughs and repo conventions & guardrails. Linked from the READMEs (TOC + intro).
- **Go Report Card badge** added to the README (EN/ES) next to the existing CI/Go/License/MCP/SQLite badges.

## [v0.9.0] - 2026-08-11

### Added

- **Session token ledger in `sv_mem_stats`:** the server tracks an atomic estimate of the tokens (chars/4) injected into the agent context by the Auto-Boot bundle and bulk-returning read tools since the last `sv_mem_session_start` (reset on session start). `sv_mem_stats` now reports `Estimated tokens injected this session` alongside the `max_response_tokens` budget, so the agent can decide when to compact.
- **Silent context injection in Claude Code hooks (`hooks install --context-injection`, opt-in, default off):** when enabled, the PreToolUse hook calls `sv-memory context <file>` on the first `Read` of each file and injects the compact graph+memory context pack (title + `why` truncated, bounded to 3 memories) as `additionalContext`. Output is cached per file for the session and time-bounded with a portable 2s timeout; the hook always exits 0 (fail-open). Enabled by a `.sv-memory/context-injection-enabled` marker created by the flag and reported by `sv-memory hooks status`; disable with `hooks uninstall --context-injection`. Antigravity, Codex, and OpenCode do not support `additionalContext` injection and keep the nudge/skill mechanism.
- **`sv_mem_search` graph-aware recall (`graph_boost`, default on):** when a `path` is provided, the search now expands to the whole graph community of that path (`where_path IN community` in addition to the exact path filter), so a module search surfaces memories for the entire module in one call instead of several path-scoped searches. Community-expanded rows are annotated with a `[graph]` marker. Falls back to the plain path filter when the graph has no community data; disable with `graph_boost=false`.
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
[v0.9.0]: https://github.com/svtech-code/sv-memory/compare/v0.8.0...v0.9.0
[v0.10.0]: https://github.com/svtech-code/sv-memory/compare/v0.9.0...v0.10.0
[v0.11.0]: https://github.com/svtech-code/sv-memory/compare/v0.10.0...v0.11.0
[v0.12.0]: https://github.com/svtech-code/sv-memory/compare/v0.11.0...v0.12.0
[v0.13.0]: https://github.com/svtech-code/sv-memory/compare/v0.12.0...v0.13.0
[v0.13.1]: https://github.com/svtech-code/sv-memory/compare/v0.13.0...v0.13.1
[v0.14.0]: https://github.com/svtech-code/sv-memory/compare/v0.13.1...v0.14.0
[Unreleased]: https://github.com/svtech-code/sv-memory/compare/v0.14.0...main
