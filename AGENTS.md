<!-- SV-MEMORY:START -->
# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Session Lifecycle (REQUIRED, in this order):

1. **Start:** Call 'sv_mem_session_start' at the beginning of work. It returns an **Auto-Boot Context Bundle** with the previous session summary, key architectural decisions, standards, recent bugfixes, last journals, and top graph hubs — read it and use it as your starting context.
2. **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
3. **Capture knowledge as you go:** Save journals, decisions, standards, and bugfixes with 'sv_mem_save' (see the Memory Capture Guidelines below). For evolving categories ('decision', 'standard', 'architecture', 'bugfix'), 'topic_key' is automatically derived if omitted to enable upsert semantics. Use 'sv_mem_capture_passive' for lightweight observations that do not need an explicit save decision.
4. **End:** Call 'sv_mem_session_end(accomplished=...)' to save the summary and mark the session as completed in a single call (session_id is auto-detected if omitted). Alternatively, call 'sv_mem_session_summary' before 'sv_mem_session_end'.

After a compaction or context reset, call 'sv_mem_context' to recover the last session state (goal, summary, associated memories).

## Tool Usage in Any Mode:

The sv-memory tools (session, memory, graph, diagnostics) may be called in ANY operational mode — plan, build, or review. They persist only to the project memory store ('.sv-memory/'), which is project data, not source code. Do not skip memory capture, context recovery, or the session lifecycle because of the current mode.

## Context Initialization (Search-Before-Work):

Memory must be consulted before proposing or executing changes:
- **Single-Call Context Pack (Recommended):** Call 'sv_mem_context_pack(path="<file|pkg>")' before reading or editing code. It surfaces the node role, linked decisions/standards, active changes, and capability state in one call.
- **Orientation:** On a new project, call 'sv_mem_stats' first — it is the cheapest overview of memory distribution (categories, counts, sessions).
- **Targeted search:** Call 'sv_mem_search' with the topic keywords of your task (feature, component, style, module). Filter by category when relevant ('journal', 'postmortem', 'discussion', 'idea', 'qa', 'architecture', 'decision'). Avoid repeating redundant searches — the Auto-Boot Bundle already carries the previous session context.
- **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding. Never answer from assumptions alone — memory first, code second.

## Progressive Disclosure (Token-Efficient Retrieval):

Use the 3-layer pattern to minimise tokens:
- **Layer 1 — Search:** Call 'sv_mem_search' to get a compact list (IDs + titles + topic keys) of relevant memories (~30 tokens/result).
- **Layer 2 — Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory (includes the central observation rationale).
- **Layer 3 — Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
Never dump all fields from search — drill down on demand. The top search result is already expanded inline, so only drill further when you need deeper detail.

## Topic Keys (Upsert Semantics):

- 'sv_mem_save' automatically derives a stable 'category/kebab-case' key for evolving categories if 'topic_key' is omitted.
- Or use 'sv_mem_suggest_topic_key(category, what)' to preview/generate a custom key.
- Saves to the same project+topic update in place (revision_count++) instead of creating duplicate records.
- **Convention:** Always kebab-case in English. Examples: 'standard/design-system', 'architecture/component-card', 'decision/use-bun-instead-of-npm', 'standard/workflow-git-commits', 'bugfix/tab-transition-absolute-position'.

## Memory Capture Guidelines (when to save what):

Always persist design knowledge as structured memories with a topic_key, not just session journals:

| Situation | Category | topic_key example |
| :--- | :--- | :--- |
| Visual style / design system / CSS / Tailwind tokens | 'standard' | standard/design-system |
| Reusable component or UI pattern | 'architecture' | architecture/component-card |
| Workflow / methodology / build & dev process | 'standard' | standard/workflow-dev-process |
| Architectural decision made (and its rationale) | 'decision' | decision/... |
| Code convention / naming / folder structure | 'standard' | standard/code-conventions |
| Complex or non-obvious bug fixed | 'bugfix' | bugfix/... |
| Relevant Q&A with lasting value | 'qa' | qa/... |
| Rejected library or framework feature | 'decision' | decision/avoid-... |
| Session progress checkpoint | 'journal' | journal/... |

**Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as 'standard' or 'architecture' with a topic_key. A journal is not a substitute — journals document progress, 'standard'/'architecture'/'decision' preserve the "how" and the "why" for future sessions.

## Graph — use it instead of grep/read on synced code:

The sv-memory graph is a pre-computed structural index of the project (source, call paths, blast radius you would otherwise re-derive by reading). For any symbol or path the graph has synced, prefer graph tools over a raw grep/read loop: one call returns line-numbered source, structure, and consequences in far fewer tokens and round-trips.

- **Explore first (read-equivalent):** Call 'sv_graph_explore' BEFORE reading or grepping a file. Pass one or more comma-separated symbols/paths; it returns each symbol's structural role, a surgical line-numbered source snippet (treat it as already read), the shortest call path between them, blast radius, and linked memories. 'sv_mem_context_pack(path="<file|pkg>")' is the same contract for a single path plus active changes and capabilities.
- **Orient before touching code:** Call 'sv_graph_god_nodes' to see the most-connected hub nodes — these are the architectural hotspots any change may ripple through.
- **Understand a module:** Call 'sv_graph_explain(node=...)' before refactoring, deleting, or restructuring a file/module. It reports the node's role, community, centrality, fan-in/fan-out, neighbors, and suggested questions.
- **Inspect dependencies:** Call 'sv_graph_query(path_or_node=...)' to see a module's dependency sub-graph (imports/calls/depends_on) with depth, direction, and relation-type filters.
- **Trace a connection:** Call 'sv_graph_path(source=..., target=...)' to find the shortest dependency path between two nodes.

### Anti-patterns (don't):

- **Don't re-verify graph results with grep.** They come from a full parse; re-checking with grep is slower, less accurate, and wastes context.
- **Don't grep or read first** to find or understand synced code — ONE 'sv_graph_explore'/'sv_mem_context_pack' returns the relevant source in a single round-trip. Reach for raw Read/Grep only to confirm a specific detail the graph didn't cover, or for what the graph doesn't index (configs, docs).
- **Don't hand-reconstruct a flow** — name both endpoints in one 'sv_graph_explore' call and it surfaces the path between them.
- **Trust auto-freshness after editing:** context packs re-check the disk and auto-sync when files changed, so snippets stay current; re-run 'sv_graph_explore'/'sv_mem_context_pack' after your edit to see ripple effects instead of re-reading files.

## Spec-Driven Decision Cycle (before proposing or changing behavior):

Proposals go through a lifecycle before code is written. Use it for any behavior/architecture change, not just large features:
- **Consult context:** 'sv_mem_context_pack(path="<file|pkg>", include_changes="true")' surfaces the node role, linked decisions/standards, active changes, and the capabilities implemented at that path (with their requirements) in one call.
- **Propose:** 'sv_propose_spec(slug="<kebab-case>", title=..., what=..., where_path=..., requirements=..., tasks=..., capability_path=...)' registers the change and runs a pre-flight check against rules/invariants (standards, decisions, architecture memories). A pinned rule that overlaps the proposal returns a BLOCK verdict. The optional 'requirements' param carries OpenSpec-style delta requirements (## ADDED/MODIFIED/REMOVED/RENAMED Requirements, ### Requirement:, #### Scenario: with GIVEN/WHEN/THEN/AND steps) targeting a single capability (defaults to the slug).
- **Apply & Update Tasks:** As implementation proceeds, call 'sv_update_spec(change_id=..., tasks=...)' to update completed task checkboxes ('- [x]') and refine design/requirements in real-time.
- **Validate:** 'sv_validate_decision(change_id=...)' re-checks a proposal after edits (PASS/WARN/BLOCK) and validates the delta requirements (RFC 2119 keyword presence, MODIFIED scenario drops vs the current capability state). Deterministic by default; pass semantic="true" to opt into agent re-ranking.
- **Commit:** 'sv_commit_spec(change_id=...)' promotes the change into a durable decision/standard memory, links it to the change_id, wires the rationale_for edge, merges the delta requirements into the capability's current state (.sv-memory/specs/capabilities/ + graph spec nodes), and stamps it applied. A pre-flight BLOCK or a requirements merge conflict rejects the commit unless force="true" explicitly overrides the invariant. Call after implementation, before 'sv_mem_session_end'.
- Lifecycle states: 'draft' → 'proposed' → 'validated' → 'applied' (→ 'archived') | 'rejected'. Committed decisions get topic_key 'decision/<slug>'.
- **Human-visible mirror:** every change is auto-projected to '.sv-memory/specs/changes/<slug>.md' and 'openspec/changes/<slug>/' (git-synced) including its delta requirements; the merged current state lives under '.sv-memory/specs/capabilities/<cap>/spec.md'. Humans can edit those files; 'sv-memory specs import <slug>' reconciles the edits back into the store (the SQLite DB stays authoritative). 'sv-memory specs export/list/archive/capabilities' manage the mirror.

## Graph Refresh:

Execute 'sv_graph_sync' after adding major new files, creating new packages, or modifying package structures/imports. The graph is rebuilt incrementally and communities/centrality are computed lazily when queried.

## Memory Maintenance (periodic):

- **Review:** Call 'sv_mem_review' to list stale, duplicate, or consolidation-candidate memories.
- **Conflicts:** Call 'sv_mem_conflicts action=scan' to detect potential duplicate memories; judge them with 'sv_mem_judge' (supersedes / conflicts_with / relates_to) or with 'action=scan semantic=true' to LLM-judge candidates via the agent CLI. Keep relations coherent.
- **Compare:** Call 'sv_mem_compare(id1, id2)' before judging two similar memories.
- **Compact:** Call 'sv_mem_compact' periodically or after many topic-key upserts to consolidate revisions and keep search fast.

## Tool Quick Reference:

- **Session:** sv_mem_session_start, sv_mem_session_summary, sv_mem_session_end, sv_mem_context
- **Memory CRUD:** sv_mem_save, sv_mem_update, sv_mem_get, sv_mem_delete, sv_mem_search, sv_mem_timeline
- **Pin / Priority:** sv_mem_pin (action='unpin' to clear)
- **Knowledge quality:** sv_mem_suggest_topic_key, sv_mem_judge, sv_mem_compare, sv_mem_compact, sv_mem_review, sv_mem_capture_passive, sv_mem_conflicts, sv_mem_stats, sv_mem_diagnose
- **User intent:** sv_mem_capture_prompt (record what the user asked, recoverable via sv_mem_context)
- **Project admin:** sv_mem_merge_projects (merge project variants into a canonical project)
- **Context Pack:** sv_mem_context_pack (one bounded call: graph role + linked memories + active changes + capabilities for a file/package/symbol)
- **Decision Engine:** sv_propose_spec, sv_update_spec, sv_validate_decision, sv_commit_spec (propose → update tasks/design → validate → commit cycle with pre-flight checks and delta requirements)
- **Spec Mirror (CLI):** sv-memory specs export | import <slug> | list | archive | capabilities (human-readable Markdown projection of changes and capability state under .sv-memory/specs/ and openspec/)
- **Graph:** sv_graph_explore, sv_graph_query, sv_graph_diff, sv_graph_explain, sv_graph_god_nodes, sv_graph_path, sv_graph_sync, sv_graph_surprising_connections, sv_graph_report, sv_graph_viz, sv_graph_merge

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): description'). Use the project's configured commit language (default: English), unless the project specifies otherwise.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->

## Project-Specific Rules (sv-memory)

- **Commit Language:** For this repository specifically, all commit messages must be written in English.
- **Phase-Gated Workflow:** Multi-phase tasks run one phase at a time. After completing and verifying each phase (go build, go vet, go test -race, gofmt), deliver the commit message (Conventional Commits, English) for the user to apply manually, then ASK FOR CONFIRMATION before starting the next phase. Never run git add/commit/push autonomously. Keep docs EN/ES, skills, and CHANGELOG in sync within each phase.
- **Lint Before Push:** The CI Lint gate is golangci-lint v2.12.2; 'go vet' does NOT include govet 'shadow' (only enabled by golangci-lint's `govet enable-all`). Always run `golangci-lint run ./...` locally before pushing. In tests, never write `if err := X(); err != nil` when an outer `err` already exists in the function scope reuse it with the repo pattern `if err = X(); err != nil` (see history commits 9adc79f / 6613b56).
