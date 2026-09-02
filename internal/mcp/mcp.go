package mcp

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// debugEnabled reports whether SV_MEMORY_DEBUG env var is set to a truthy value.
// Used to emit stderr timing logs without polluting the MCP stdio JSON channel.
func debugEnabled() bool {
	v := os.Getenv("SV_MEMORY_DEBUG")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

// debugLog writes a timing line to stderr (safe: stderr is a separate channel
// from MCP JSON-RPC which uses stdout). Skipped entirely on the hot path when
// SV_MEMORY_DEBUG is unset.
func debugLog(format string, args ...interface{}) {
	if !debugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "[sv-memory] "+format+"\n", args...)
}

// Tool describes a single MCP tool exposed by the sv-memory server.
// AllTools is the single source of truth for the tool surface, reused by the
// permission manager (sv-memory permissions / configure wizard) so the granted
// allow-list always matches the tools actually registered by the MCP server.
type Tool struct {
	Name        string
	Description string
}

// AllTools enumerates every tool the sv-memory MCP server exposes, with a
// short human-readable description used for permission selection transparency.
// Keep in sync with the NewTool/AddTool registrations in NewServer below;
// TestAllToolsMatchesRegisteredTools in mcp_test.go enforces that every
// registered tool name appears here.
var AllTools = []Tool{
	{Name: "sv_mem_save", Description: "Persist a decision, bugfix, journal, or standard to project memory (with optional topic_key upsert)."},
	{Name: "sv_mem_update", Description: "Partially update an existing memory by ID (what, why, learned, where_path, impact, errors_faced, next_steps)."},
	{Name: "sv_mem_suggest_topic_key", Description: "Generate a stable 'category/kebab-case' topic_key for upsert semantics."},
	{Name: "sv_mem_session_start", Description: "Register a new coding session and receive the Auto-Boot Context Bundle (previous session summary, key decisions, standards, recent bugfixes, journals, top graph hubs)."},
	{Name: "sv_mem_session_end", Description: "End the active session with a summary to enable context recovery."},
	{Name: "sv_mem_session_summary", Description: "Save the session goal, discoveries, accomplished work, and next steps."},
	{Name: "sv_mem_context", Description: "Recover the last completed session's goal, summary, and associated memories after compaction."},
	{Name: "sv_mem_compact", Description: "Consolidate historical topic-key revisions and duplicates into clean summaries. Call periodically or after many upserts to keep search fast."},
	{Name: "sv_mem_search", Description: "Search project memories with FTS5 BM25 ranking and category/path filters."},
	{Name: "sv_mem_get", Description: "Retrieve the full content of a specific memory by ID."},
	{Name: "sv_mem_timeline", Description: "Get chronological context around a specific memory observation."},
	{Name: "sv_mem_judge", Description: "Create a relation between memories (supersedes, conflicts_with, relates_to)."},
	{Name: "sv_mem_compare", Description: "Compare two memories side by side in a Markdown table."},
	{Name: "sv_mem_review", Description: "List stale, duplicate, or consolidation-candidate memories, or mark a memory as reviewed (action='mark_reviewed')."},
	{Name: "sv_mem_stats", Description: "Get aggregate memory statistics per category, session counts, and the current active project (ID, name, path)."},
	{Name: "sv_mem_diagnose", Description: "Run read-only health checks (database, FTS5, project, graph integrity) and return a report."},
	{Name: "sv_mem_delete", Description: "Soft-delete (default) or hard-delete a memory."},
	{Name: "sv_mem_pin", Description: "Pin (default) or unpin (action='unpin') a local memory so key decisions stay visible in session context."},
	{Name: "sv_mem_capture_passive", Description: "Log lightweight observations (files modified, tests failing) without a save decision."},
	{Name: "sv_mem_capture_prompt", Description: "Capture the user's prompt as a local observation attached to a session, so future sessions can recover the user's intent after compaction (recoverable via sv_mem_context)."},
	{Name: "sv_mem_merge_projects", Description: "Merge all memories, sessions, relations, and graph data from one project into another, then delete the source project (admin)."},
	{Name: "sv_mem_context_pack", Description: "Build a compact context pack for a code path: graph role (fan-in/fan-out, community) plus linked memories (decisions/standards/bugfixes). One bounded call."},
	{Name: "sv_graph_explore", Description: "Unified explore for code understanding in one call: pass one or more comma-separated symbols/paths to get each symbol's structural role, surgical source snippet, the shortest call path between them, blast radius, and linked memories (decisions/standards/bugfixes). Replaces chaining sv_graph_query + sv_graph_path + sv_graph_explain manually."},
	{Name: "sv_mem_conflicts", Description: "List, scan, or ignore potential memory conflicts."},
	{Name: "sv_propose_spec", Description: "Create a spec change (proposal) with its lifecycle state and run a pre-flight check against the project's rules and invariants."},
	{Name: "sv_validate_decision", Description: "Re-check a change's proposal against rules and invariants (PASS/WARN/BLOCK); opt-in semantic re-ranking."},
	{Name: "sv_commit_spec", Description: "Promote a validated change into a durable decision/standard memory, wire rationale_for edges, and stamp it applied."},
	{Name: "sv_graph_query", Description: "Query the dependency graph for a module, file, or package (returns Mermaid)."},
	{Name: "sv_graph_path", Description: "Find the shortest dependency path between two nodes."},
	{Name: "sv_graph_sync", Description: "Incrementally re-scan the codebase and rebuild the dependency graph. Call after adding major files or restructuring packages."},
	{Name: "sv_graph_explain", Description: "Explain a node's role, community, centrality, neighbors, and suggested questions. Use before refactoring or deleting a file."},
	{Name: "sv_graph_god_nodes", Description: "List the most-connected hub nodes in the dependency graph."},
	{Name: "sv_graph_surprising_connections", Description: "Find unexpected cross-community connections in the codebase."},
	{Name: "sv_graph_report", Description: "Generate GRAPH_REPORT.md with god nodes, top communities, surprising cross-community bridges, and suggested questions."},
	{Name: "sv_graph_viz", Description: "Generate an interactive HTML visualization of the dependency graph."},
	{Name: "sv_graph_merge", Description: "Merge two project graphs into one (union-merge by node ID)."},
}

// global shutdown state: the latest server instance's cleanup hook, invoked on
// shutdown signals or when the stdio transport ends.
var (
	shutdownCleanup func()
	cleanupMu       sync.Mutex
)

// Server holds the long-lived state shared by every MCP tool handler: the
// connection pool, the active project config, the debounced Git sync state,
// and the per-project graph load lock. Splitting this state onto the Server
// struct (instead of closures in NewServer) keeps the tool handlers in
// focused per-domain files.
type Server struct {
	pool *db.Pool
	cfg  *config.Config

	// Git sync debounce/coalescing state.
	syncMu       sync.Mutex
	lastSyncMtim time.Time

	debounceMu  sync.Mutex
	syncTimer   *time.Timer
	syncVersion int

	// Serializes concurrent graph loads (double-checked via GlobalGraphCache).
	graphMu sync.Mutex

	// Git metadata cache: branch/commit/author are read with shell-outs to
	// `git` (up to 4 subprocesses per call via the author email fallback).
	// Caching them collapses that to one batch per gitCacheTTL window.
	gitCacheMu sync.Mutex
	gitCache   gitMetadata
	gitCacheAt time.Time

	// sessionTokens is a running estimate of the tokens (chars/4) injected into
	// the agent context by bulk-returning read tools since the last
	// sv_mem_session_start. Reset on session start and reported by sv_mem_stats
	// so the agent can decide when to compact.
	sessionTokens atomic.Int64
}

// gitMetadata holds the branch, short commit, and author read from git for a
// project. Used to avoid shelling out to git on every sv_mem_save.
type gitMetadata struct {
	branch string
	commit string
	author string
}

// gitCacheTTL bounds how long a cached git metadata read is considered fresh.
const gitCacheTTL = 30 * time.Second

// cachedGitMetadata returns the project's git branch, commit, and author,
// refreshing them at most once every gitCacheTTL. All three values are read in
// a single batch so a TTL miss still costs only one burst of git calls, and a
// TTL hit costs zero.
func (s *Server) cachedGitMetadata() gitMetadata {
	s.gitCacheMu.Lock()
	defer s.gitCacheMu.Unlock()
	if !s.gitCacheAt.IsZero() && time.Since(s.gitCacheAt) < gitCacheTTL {
		return s.gitCache
	}
	branch, commit, author := config.GetGitMetadata(s.cfg.ProjPath)
	s.gitCache = gitMetadata{branch: branch, commit: commit, author: author}
	s.gitCacheAt = time.Now()
	return s.gitCache
}

// StartServer starts the MCP server using stdio transport.
// Reads use the pool's Reader so concurrent tool calls scale; writes (save)
// go through the Writer to keep SQLite serialized under WAL.
func StartServer(pool *db.Pool, cfg *config.Config) error {
	s := NewServer(pool, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if viper.GetBool("auto_compaction_enabled") {
		memory.StartAutoCompaction(ctx, pool.Writer, cfg.ProjectID, viper.GetInt("compaction_interval_minutes"))
	}

	go func() {
		<-ctx.Done()
		debugLog("Shutdown signal received. Running cleanup...")
		cleanupMu.Lock()
		if shutdownCleanup != nil {
			shutdownCleanup()
		}
		cleanupMu.Unlock()
		os.Exit(0)
	}()

	err := server.ServeStdio(s)

	cleanupMu.Lock()
	if shutdownCleanup != nil {
		shutdownCleanup()
	}
	cleanupMu.Unlock()

	return err
}

// NewServer initializes the MCP server, registers all tools, and returns it.
// Split from StartServer for programmatic unit testing. Tool definitions and
// handler wiring live here (single source of truth for the tool surface, kept
// in sync with AllTools); the handlers themselves are Server methods defined
// in the per-domain tools_*.go files.
func NewServer(pool *db.Pool, cfg *config.Config) *server.MCPServer {
	s := &Server{pool: pool, cfg: cfg}

	ms := server.NewMCPServer("sv-memory", "1.0.0")

	cleanupMu.Lock()
	shutdownCleanup = s.flushPendingSync
	cleanupMu.Unlock()

	// 1. Tool: sv_mem_save
	saveTool := mcp.NewTool("sv_mem_save",
		mcp.WithDescription("Persist a key architectural decision, bug fix, progress journal, or standard guidelines to the project's memory. Supports optional topic_key for upsert semantics (automatically derived from category and title if omitted for non-journal categories) and session_id for session association."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("Concise description of the decision, standard, or fix")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Detailed reasoning for this choice")),
		mcp.WithString("learned", mcp.Required(), mcp.Description("Rule or key lesson to guide future agents")),
		mcp.WithString("where_path", mcp.Description("Optional file or folder path affected by this memory")),
		mcp.WithString("impact", mcp.Description("Achievements, successes, or what went well")),
		mcp.WithString("errors_faced", mcp.Description("Errors faced, roadblocks, or what went wrong")),
		mcp.WithString("next_steps", mcp.Description("Next actions or pending tasks to continue work")),
		mcp.WithString("topic_key", mcp.Description("Optional stable topic key for upsert semantics. When omitted for non-journal categories, a stable key is auto-generated. Format: 'category/kebab-case-description'.")),
		mcp.WithString("session_id", mcp.Description("Optional session ID to associate this memory with an active session (auto-detected if omitted).")),
	)
	ms.AddTool(saveTool, s.handleSave)

	// 1b. Tool: sv_mem_update
	updateTool := mcp.NewTool("sv_mem_update",
		mcp.WithDescription("Partially update an existing memory by ID. Only the provided fields are changed; identity fields (id, category, session, topic_key) are preserved. The revision counter advances and the change is synced to Git. Use sv_mem_get to inspect the current content before updating."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to update")),
		mcp.WithString("what", mcp.Description("Optional: new concise description")),
		mcp.WithString("why", mcp.Description("Optional: new detailed reasoning")),
		mcp.WithString("learned", mcp.Description("Optional: new rule or key lesson")),
		mcp.WithString("where_path", mcp.Description("Optional: file or folder path affected (empty string clears it)")),
		mcp.WithString("impact", mcp.Description("Optional: achievements or what went well (empty string clears it)")),
		mcp.WithString("errors_faced", mcp.Description("Optional: errors faced or roadblocks (empty string clears it)")),
		mcp.WithString("next_steps", mcp.Description("Optional: next actions or pending tasks (empty string clears it)")),
	)
	ms.AddTool(updateTool, s.handleUpdate)

	// 2. Tool: sv_mem_suggest_topic_key
	suggestTool := mcp.NewTool("sv_mem_suggest_topic_key",
		mcp.WithDescription("Suggest a stable topic_key for an evolving topic before saving. The key follows 'category/kebab-case-description' format and enables upsert semantics in sv_mem_save."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("The title or description of the memory")),
	)
	ms.AddTool(suggestTool, s.handleSuggestTopicKey)

	// 3. Tool: sv_mem_session_start
	sessionStartTool := mcp.NewTool("sv_mem_session_start",
		mcp.WithDescription("Register the start of a new coding session and receive an Auto-Boot Context Bundle: previous session summary, key architectural decisions, standards, recent bugfixes, postmortems, recent Q&A, last journals, and top graph hubs. Call this at the beginning of every work session to enable session grouping and post-compaction context recovery."),
		mcp.WithString("goal", mcp.Description("Optional goal or objective for this session. When provided, the Auto-Boot bundle ranks the surfaced decisions/standards/bugfixes by relevance to it instead of pure recency.")),
		mcp.WithString("directory", mcp.Description("Optional working directory (auto-detected from repo if omitted)")),
		mcp.WithString("semantic", mcp.Description("When 'true' and a goal is given, re-rank the Auto-Boot bundle candidates with the configured agent CLI by semantic relevance (opt-in; fails open to the deterministic keyword ranking when the agent is unavailable). Default 'false'.")),
		mcp.WithString("semantic_agent", mcp.Description("Optional agent CLI for semantic recall. Defaults to $SV_MEMORY_SEMANTIC_AGENT, then 'claude'.")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(sessionStartTool, s.handleSessionStart)

	// 4. Tool: sv_mem_session_end
	sessionEndTool := mcp.NewTool("sv_mem_session_end",
		mcp.WithDescription("End an active coding session with an optional summary. If session_id is omitted, the active session is auto-detected. Call this before finishing work to enable context recovery via sv_mem_context."),
		mcp.WithString("session_id", mcp.Description("Optional session ID to end (auto-detected if omitted)")),
		mcp.WithString("summary", mcp.Description("Optional summary of what was accomplished")),
		mcp.WithString("goal", mcp.Description("Optional goal or objective of the session")),
		mcp.WithString("accomplished", mcp.Description("Optional what was accomplished during the session")),
		mcp.WithString("discoveries", mcp.Description("Optional key discoveries or findings")),
		mcp.WithString("next_steps", mcp.Description("Optional next steps or pending tasks")),
		mcp.WithString("files", mcp.Description("Optional relevant files modified or created")),
	)
	ms.AddTool(sessionEndTool, s.handleSessionEnd)

	// 5. Tool: sv_mem_session_summary
	sessionSummaryTool := mcp.NewTool("sv_mem_session_summary",
		mcp.WithDescription("Save a structured summary for a session. Call before sv_mem_session_end to record goal, discoveries, accomplished work, next steps, and relevant files."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Session ID to associate the summary with")),
		mcp.WithString("goal", mcp.Description("Original goal or objective of the session")),
		mcp.WithString("discoveries", mcp.Description("Key discoveries or findings during the session")),
		mcp.WithString("accomplished", mcp.Description("What was accomplished during the session")),
		mcp.WithString("next_steps", mcp.Description("Next steps or pending items")),
		mcp.WithString("files", mcp.Description("Relevant files modified or created")),
	)
	ms.AddTool(sessionSummaryTool, s.handleSessionSummary)

	// 6. Tool: sv_mem_context
	contextTool := mcp.NewTool("sv_mem_context",
		mcp.WithDescription("Recover context from the last completed session. Call this first after a compaction or context reset to resume work. Returns the last session's goal, summary, and up to the given number of associated memories."),
		mcp.WithString("limit", mcp.Description("Optional limit of memories to include (default '10')")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(contextTool, s.handleContext)

	// 7. Tool: sv_mem_compact
	compactTool := mcp.NewTool("sv_mem_compact",
		mcp.WithDescription("Trigger automatic memory compaction for the project. Consolidates historical topic key revisions and duplicates into clean, high-quality summary records to keep search fast and token usage minimal. Call periodically or after many topic-key upserts."),
		mcp.WithDeferLoading(true),
	)
	ms.AddTool(compactTool, s.handleCompact)

	// 8. Tool: sv_mem_search
	searchTool := mcp.NewTool("sv_mem_search",
		mcp.WithDescription("Search historical project memories using keyword/FTS5 search with BM25 ranking. Returns compact results (ID, category, title, date, topic_key). Use sv_mem_get to retrieve full content of a specific memory, or sv_mem_timeline for chronological context around it."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword or phrase to search for")),
		mcp.WithString("category", mcp.Description("Optional category to filter results: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("path", mcp.Description("Optional path/directory scope filter to narrow memories relevant to a specific file or directory")),
		mcp.WithString("match_mode", mcp.Description("FTS5 match mode: 'all' (every token must match, default) or 'any' (broader recall — a memory matching one or more tokens is returned)")),
		mcp.WithString("semantic", mcp.Description("When 'true', re-rank the keyword candidates with the configured agent CLI by semantic relevance (opt-in, one batched call; fails open to keyword results when the agent is unavailable). Default 'false'.")),
		mcp.WithString("semantic_agent", mcp.Description("Optional agent CLI for semantic recall. Defaults to $SV_MEMORY_SEMANTIC_AGENT, then 'claude'.")),
		mcp.WithString("limit", mcp.Description("Optional limit of results to return (default is '10')")),
		mcp.WithString("offset", mcp.Description("Optional offset for pagination (default is '0')")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(searchTool, s.handleSearch)

	// 9. Tool: sv_mem_get
	getTool := mcp.NewTool("sv_mem_get",
		mcp.WithDescription("Retrieve the full content of a specific memory by its ID. This is the third layer of progressive disclosure: use after sv_mem_search to inspect a memory in detail. Long text fields are truncated beyond max_chars to limit tokens."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to retrieve")),
		mcp.WithString("max_chars", mcp.Description("Optional max characters per text field (default '1000', '0' = unlimited)")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(getTool, s.handleGet)

	// 10. Tool: sv_mem_timeline
	timelineTool := mcp.NewTool("sv_mem_timeline",
		mcp.WithDescription("Get chronological context around a specific memory observation. Shows what happened before and after it in the session. This is the second layer of progressive disclosure: use after sv_mem_search to understand the context of a result."),
		mcp.WithString("observation_id", mcp.Required(), mcp.Description("The observation ID to center the timeline around")),
		mcp.WithString("before", mcp.Description("Number of memories to show before (default '5')")),
		mcp.WithString("after", mcp.Description("Number of memories to show after (default '5')")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(timelineTool, s.handleTimeline)

	// 11. Tool: sv_mem_judge
	judgeTool := mcp.NewTool("sv_mem_judge",
		mcp.WithDescription("Create a relation/judgment between two memories. Use this when a new decision supersedes, conflicts with, or relates to a previous one. Helps maintain coherence over time."),
		mcp.WithString("source_id", mcp.Required(), mcp.Description("The ID of the first (newer/source) memory")),
		mcp.WithString("target_id", mcp.Required(), mcp.Description("The ID of the second (older/target) memory")),
		mcp.WithString("relation_type", mcp.Required(), mcp.Description("Type of relation: 'supersedes' | 'conflicts_with' | 'relates_to'")),
		mcp.WithString("reason", mcp.Description("Optional explanation for the judgment")),
		mcp.WithString("judged_by", mcp.Description("Optional identifier of who judged (default: 'agent')")),
	)
	ms.AddTool(judgeTool, s.handleJudge)

	// 12. Tool: sv_mem_compare
	compareTool := mcp.NewTool("sv_mem_compare",
		mcp.WithDescription("Compare two memories side by side in a Markdown table. Use to quickly spot contradictions or similarities before judging."),
		mcp.WithString("id1", mcp.Required(), mcp.Description("The ID of the first memory")),
		mcp.WithString("id2", mcp.Required(), mcp.Description("The ID of the second memory")),
	)
	ms.AddTool(compareTool, s.handleCompare)

	// 13. Tool: sv_mem_review
	reviewTool := mcp.NewTool("sv_mem_review",
		mcp.WithDescription("List memories that may need attention: old, stale, high duplicates, or candidates for consolidation. action='mark_reviewed' (with id) resets a memory's policy-review deadline. action='prune_stale' soft-deletes stale transient memories (journal/qa/discussion/idea) older than the cutoff — dry-run by default, pass apply='true' to actually delete."),
		mcp.WithDeferLoading(true),
		mcp.WithString("action", mcp.Description("Action to perform: 'list' (default), 'mark_reviewed', or 'prune_stale'")),
		mcp.WithString("id", mcp.Description("Required for action='mark_reviewed': the memory ID to mark as reviewed")),
		mcp.WithString("older_than_days", mcp.Description("For action='prune_stale': prune memories not seen/created within this many days (default from config 'prune_stale_days', 90)")),
		mcp.WithString("category", mcp.Description("For action='prune_stale': optional comma-separated categories to prune instead of the default transient set (journal,qa,discussion,idea)")),
		mcp.WithString("apply", mcp.Description("For action='prune_stale': 'true' to actually soft-delete; default 'false' (dry run — only lists what would be pruned)")),
	)
	ms.AddTool(reviewTool, s.handleReview)

	// 14. Tool: sv_mem_stats (includes current project info)
	statsTool := mcp.NewTool("sv_mem_stats",
		mcp.WithDescription("Get aggregate memory statistics for the current project: total memories, per-category breakdown, session counts, recent activity, relation count, and the active project (ID, name, path)."),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(statsTool, s.handleStats)

	// 15. Tool: sv_mem_diagnose
	diagnoseTool := mcp.NewTool("sv_mem_diagnose",
		mcp.WithDescription("Run read-only health checks on the active project: database file, schema tables, FTS5 triggers, project registration, write permissions, chunk directory, and structural graph integrity (dangling edges, orphan nodes, self-loops, missing files)."),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(diagnoseTool, s.handleDiagnose)

	// 16. Tool: sv_mem_delete
	deleteTool := mcp.NewTool("sv_mem_delete",
		mcp.WithDescription("Delete a memory. Soft delete (default) marks it as deleted but preserves it in the database for potential recovery. Hard delete removes it permanently."),
		mcp.WithDeferLoading(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to delete")),
		mcp.WithString("hard", mcp.Description("Set to 'true' for permanent hard delete (default: 'false' = soft delete)")),
	)
	ms.AddTool(deleteTool, s.handleDelete)

	// 17. Tool: sv_mem_pin (pin/unpin consolidated via action)
	pinTool := mcp.NewTool("sv_mem_pin",
		mcp.WithDescription("Pin a local memory so it surfaces first in sv_mem_context (key decisions stay visible). Use action='unpin' to clear the pinned flag. Pinned state is local to this device."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to pin or unpin")),
		mcp.WithString("action", mcp.Description("Action to perform: 'pin' (default) or 'unpin'")),
	)
	ms.AddTool(pinTool, s.handlePin)

	// 18. Tool: sv_mem_capture_passive
	captureTool := mcp.NewTool("sv_mem_capture_passive",
		mcp.WithDescription("Save a lightweight passive observation (e.g. 'modified file X', 'test Y failed'). Unlike sv_mem_save, this requires no explicit decision — it logs context automatically. Category is set to 'journal'."),
		mcp.WithString("what", mcp.Required(), mcp.Description("Brief description of what happened")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Context or reason for the observation")),
	)
	ms.AddTool(captureTool, s.handleCapturePassive)

	// 18b. Tool: sv_mem_capture_prompt (Engram mem_save_prompt parity)
	capturePromptTool := mcp.NewTool("sv_mem_capture_prompt",
		mcp.WithDescription("Capture the user's prompt as a local observation attached to a session. Records what the user asked so future sessions have context about user goals; recoverable via sv_mem_context and counted by sv_mem_stats. Prompts are local-only (not git-synced) in this phase."),
		mcp.WithString("content", mcp.Required(), mcp.Description("The user's prompt text")),
		mcp.WithString("session_id", mcp.Description("Session ID to associate the prompt with (defaults to the active session)")),
	)
	ms.AddTool(capturePromptTool, s.handleCapturePrompt)

	// 18c. Tool: sv_mem_merge_projects (Engram mem_merge_projects parity, admin)
	mergeProjectsTool := mcp.NewTool("sv_mem_merge_projects",
		mcp.WithDescription("Merge multiple project name variants into a single canonical project (admin). Moves all memories, sessions, relations, and graph data from 'from' into 'to', then deletes the source project. Mirrors `sv-memory projects consolidate <from> <to>`."),
		mcp.WithString("from", mcp.Required(), mcp.Description("Source project ID to move data from and then delete")),
		mcp.WithString("to", mcp.Required(), mcp.Description("Target project ID receiving the data")),
	)
	ms.AddTool(mergeProjectsTool, s.handleMergeProjects)

	// 18b. Tool: sv_mem_context_pack
	contextPackTool := mcp.NewTool("sv_mem_context_pack",
		mcp.WithDescription("Build a compact context pack for a code path (file, package, or symbol): the node's structural role in the dependency graph (type, fan-in/fan-out, community) plus the memories linked to that path via where_path or rationale_for edges (decisions, standards, bugfixes). One bounded call replaces the sv_graph_explain + sv_mem_search + sv_mem_get round-trips, saving tokens. Set include_changes='true' to also list active spec changes (proposals) affecting the path."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path, package name, or symbol to resolve")),
		mcp.WithString("include_changes", mcp.Description("When 'true', also list active spec changes (proposals) whose where_path matches this path. Default 'false'.")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(contextPackTool, s.handleContextPack)

	// 18b2. Tool: sv_graph_explore (unified explore alias of sv_mem_context_pack)
	graphExploreTool := mcp.NewTool("sv_graph_explore",
		mcp.WithDescription("Understand code in ONE call (unified explore): pass one or more comma-separated symbols, file paths, or package names. Each resolved symbol gets its structural role in the dependency graph (type, fan-in/fan-out, community), a surgical source-code snippet, and the shortest call path between the two most significant symbols is rendered — plus blast radius and the memories (decisions/standards/bugfixes) linked to the primary symbol via where_path or rationale_for edges. Use this BEFORE reading/grepping files: the returned source counts as already read. Set include_changes='true' to also list active spec changes affecting the path."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Symbol(s), file path(s), or package name(s) to explore. Multiple symbols may be comma-separated (e.g. 'ResolveContextNode, extractSurgicalSnippet') to get their source + call path in one call.")),
		mcp.WithString("include_changes", mcp.Description("When 'true', also list active spec changes (proposals) whose where_path matches the primary path. Default 'false'.")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(graphExploreTool, s.handleContextPack)

	// 18c. Tool: sv_propose_spec
	proposeSpecTool := mcp.NewTool("sv_propose_spec",
		mcp.WithDescription("Create a spec change (proposal) for the spec-driven decision engine: registers the change, advances it to the proposed lifecycle state, and runs a pre-flight check against the project's rules and invariants (standards, decisions, architecture memories). A pinned rule overlapping the proposal returns a BLOCK verdict; an ordinary overlap returns WARN. Optionally carries OpenSpec-style delta requirements (requirements param) targeting a single capability (capability_path, defaulting to the slug) that are merged into the capability state on commit. Use before writing code, then sv_validate_decision to re-check after edits, and sv_commit_spec to promote the change into a durable decision memory."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("Kebab-case unique identifier for the change (e.g. 'implement-session-auth'). Project-unique.")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Concise title of the proposal")),
		mcp.WithString("what", mcp.Description("Why and what changes: the proposal body")),
		mcp.WithString("goal", mcp.Description("Optional intent/goal of the proposal")),
		mcp.WithString("where_path", mcp.Description("Optional affected code path (file, folder, or package) — used for AFFECTS wiring and context-pack recall")),
		mcp.WithString("requirements", mcp.Description("Optional OpenSpec-style delta requirements (Markdown with ## ADDED/MODIFIED/REMOVED/RENAMED Requirements, ### Requirement:, #### Scenario: and GIVEN/WHEN/THEN/AND steps). Stored for validation and merged into the capability state on commit.")),
		mcp.WithString("capability_path", mcp.Description("Optional capability this change targets (defaults to the slug). Single capability per change.")),
		mcp.WithString("design", mcp.Description("Optional technical approach")),
		mcp.WithString("tasks", mcp.Description("Optional implementation checklist")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(proposeSpecTool, s.handleProposeSpec)

	// 18d. Tool: sv_validate_decision
	validateDecisionTool := mcp.NewTool("sv_validate_decision",
		mcp.WithDescription("Re-check an existing change's proposal against the project's rules and invariants, returning a PASS/WARN/BLOCK verdict, and validate its delta requirements (RFC 2119 keyword presence and MODIFIED scenario drops against the current capability state). Deterministic by default (SQLite FTS5 + Jaccard, zero LLM cost); set semantic='true' to opt into a single batched agent re-ranking by meaning (fails open to the deterministic verdict when the agent is unavailable). Use after editing a proposal and before committing."),
		mcp.WithString("change_id", mcp.Required(), mcp.Description("The change ID returned by sv_propose_spec")),
		mcp.WithString("semantic", mcp.Description("When 'true', re-rank candidates semantically via the configured agent CLI (opt-in). Default 'false'.")),
		mcp.WithString("semantic_agent", mcp.Description("Optional agent CLI for semantic validation. Defaults to $SV_MEMORY_SEMANTIC_AGENT, then 'claude'.")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(validateDecisionTool, s.handleValidateDecision)

	// 18e. Tool: sv_commit_spec
	commitSpecTool := mcp.NewTool("sv_commit_spec",
		mcp.WithDescription("Promote a validated spec change into a durable decision/standard memory: saves the decision via the memory engine (topic_key 'decision/<slug>'), links it to the change_id, wires the rationale_for edge to the affected code path, records conflicts_with relations for any pre-flight WARN/BLOCK rules, merges the change's delta requirements into the capability state (spec_capabilities + .sv-memory/specs/capabilities/ mirror + graph spec nodes), and stamps the change as applied. A pre-flight BLOCK (pinned invariant) or a requirements merge conflict rejects the commit unless force='true' explicitly overrides the invariant. Call after implementation, before sv_mem_session_end."),
		mcp.WithString("change_id", mcp.Required(), mcp.Description("The change ID returned by sv_propose_spec")),
		mcp.WithString("category", mcp.Description("Memory category for the committed decision (default 'decision'; use 'standard' for a reusable rule)")),
		mcp.WithString("force", mcp.Description("Set 'true' to override a pre-flight BLOCK (pinned invariant) and commit anyway. Default 'false'.")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response (default from config 'max_response_tokens'). Response is truncated with a notice when exceeded.")),
	)
	ms.AddTool(commitSpecTool, s.handleCommitSpec)

	// 19. Tool: sv_graph_query
	graphQueryTool := mcp.NewTool("sv_graph_query",
		mcp.WithDescription("Retrieve project code structure, connections, imports, and dependencies for a given module, file, or package."),
		mcp.WithString("path_or_node", mcp.Required(), mcp.Description("The file path, package name, or module to inspect")),
		mcp.WithString("depth", mcp.Description("Hop distance depth in the dependency graph (default is '1')")),
		mcp.WithString("relation_type", mcp.Description("Filter by relation type ('imports', 'calls', 'depends_on')")),
		mcp.WithString("direction", mcp.Description("Filter by direction ('in', 'out', 'all')")),
		mcp.WithString("mermaid", mcp.Description("Render the edges as a Mermaid diagram instead of the compact textual edge list (default 'false')")),
		mcp.WithString("token_budget", mcp.Description("Optional max tokens for the response. Response is truncated with a notice when exceeded. Default '0' (unlimited).")),
	)
	ms.AddTool(graphQueryTool, s.handleGraphQuery)

	// 20. Tool: sv_graph_path
	graphPathTool := mcp.NewTool("sv_graph_path",
		mcp.WithDescription("Find the shortest path between two nodes in the dependency graph."),
		mcp.WithString("source", mcp.Required(), mcp.Description("The starting node ID (file path, package name, etc.)")),
		mcp.WithString("target", mcp.Required(), mcp.Description("The target node ID to reach")),
		mcp.WithString("max_hops", mcp.Description("Maximum hop distance (default '10')")),
	)
	ms.AddTool(graphPathTool, s.handleGraphPath)

	// 21. Tool: sv_graph_sync
	graphSyncTool := mcp.NewTool("sv_graph_sync",
		mcp.WithDescription("Incrementally re-scan the codebase and rebuild the dependency graph. Call after adding major new files, creating new packages, or modifying package structures and imports. Communities and centrality are computed lazily on demand."),
	)
	ms.AddTool(graphSyncTool, s.handleGraphSync)

	// 22. Tool: sv_mem_conflicts
	conflictsTool := mcp.NewTool("sv_mem_conflicts",
		mcp.WithDescription("Manage potential memory conflicts in the project: list, scan, or ignore conflicts. scan with semantic='true' LLM-judges candidate pairs using the configured agent CLI (claude/opencode)."),
		mcp.WithDeferLoading(true),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action to perform: list, scan, or ignore")),
		mcp.WithString("status", mcp.Description("Optional status filter for list (pending, judged, ignored)")),
		mcp.WithString("relation_id", mcp.Description("Required for ignore action: the conflict relation ID to ignore")),
		mcp.WithString("threshold", mcp.Description("Optional similarity threshold for scan (default: '0.45')")),
		mcp.WithString("apply", mcp.Description("For scan: 'true' to save scanned conflicts / semantic judgments to database (default: 'false')")),
		mcp.WithString("semantic", mcp.Description("For scan: 'true' to LLM-judge candidate conflicts with the agent CLI (default: 'false')")),
		mcp.WithString("agent", mcp.Description("For semantic scan: agent CLI to use ('claude', 'opencode', or a custom command; default: $SV_MEMORY_SEMANTIC_AGENT or 'claude')")),
		mcp.WithString("max_semantic", mcp.Description("For semantic scan: maximum number of candidate pairs to judge (default: all)")),
		mcp.WithString("concurrency", mcp.Description("For semantic scan: number of parallel agent judgments (default: '3')")),
	)
	ms.AddTool(conflictsTool, s.handleConflicts)

	// 23. Tool: sv_graph_explain
	graphExplainTool := mcp.NewTool("sv_graph_explain",
		mcp.WithDescription("Explain a node's structural role, community, centrality metrics, neighbors, and suggested questions. Use before refactoring, deleting, or restructuring a file/module to understand its impact — richer than sv_graph_query for a single node."),
		mcp.WithString("node", mcp.Required(), mcp.Description("The node ID (file path, package, class, or function name) to explain")),
	)
	ms.AddTool(graphExplainTool, s.handleGraphExplain)

	// 24. Tool: sv_graph_god_nodes
	godNodesTool := mcp.NewTool("sv_graph_god_nodes",
		mcp.WithDescription("List the most-connected nodes (God Nodes) in the project dependency graph. These are the concepts everything flows through — useful for architectural orientation."),
		mcp.WithString("top_n", mcp.Description("Number of top god nodes to return (default '10')")),
	)
	ms.AddTool(godNodesTool, s.handleGodNodes)

	// 25. Tool: sv_graph_surprising_connections
	surprisingTool := mcp.NewTool("sv_graph_surprising_connections",
		mcp.WithDescription("Find surprising/interesting cross-community connections (bridges between different parts of the codebase)"),
		mcp.WithString("limit", mcp.Description("Maximum number of connections to return (default '10')")),
	)
	ms.AddTool(surprisingTool, s.handleSurprisingConnections)

	// 26. Tool: sv_graph_report
	reportTool := mcp.NewTool("sv_graph_report",
		mcp.WithDescription("Generate a GRAPH_REPORT.md overview (god nodes, top communities, surprising cross-community bridges, suggested questions), returning the path, byte size, and a bounded summary digest"),
		mcp.WithDeferLoading(true),
		mcp.WithString("output", mcp.Description("Output markdown file path (default GRAPH_REPORT.md)")),
		mcp.WithString("god_nodes", mcp.Description("Number of top god nodes (default 10)")),
		mcp.WithString("communities", mcp.Description("Number of top communities (default 10)")),
		mcp.WithString("connections", mcp.Description("Number of surprising connections (default 10)")),
	)
	ms.AddTool(reportTool, s.handleGraphReport)

	// 27. Tool: sv_graph_viz
	vizTool := mcp.NewTool("sv_graph_viz",
		mcp.WithDescription("Generate an interactive HTML visualization (graph.html) of the project dependency graph"),
		mcp.WithDeferLoading(true),
		mcp.WithString("output", mcp.Description("Output HTML file path (default 'graph.html')")),
	)
	ms.AddTool(vizTool, s.handleGraphViz)

	// 28. Tool: sv_graph_merge
	mergeTool := mcp.NewTool("sv_graph_merge",
		mcp.WithDescription("Merge two project graphs into one (union-merge by node ID)"),
		mcp.WithDeferLoading(true),
		mcp.WithString("project_a", mcp.Required(), mcp.Description("First project ID")),
		mcp.WithString("project_b", mcp.Required(), mcp.Description("Second project ID")),
		mcp.WithString("output", mcp.Description("Output JSON file path")),
	)
	ms.AddTool(mergeTool, s.handleGraphMerge)

	return ms
}
