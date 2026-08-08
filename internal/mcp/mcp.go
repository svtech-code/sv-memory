package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
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
	{Name: "sv_mem_review", Description: "List stale, duplicate, or consolidation-candidate memories."},
	{Name: "sv_mem_stats", Description: "Get aggregate memory statistics per category and session counts."},
	{Name: "sv_mem_current_project", Description: "Get the current project's ID and display name."},
	{Name: "sv_mem_delete", Description: "Soft-delete (default) or hard-delete a memory."},
	{Name: "sv_mem_pin", Description: "Pin a local memory so it surfaces first in session context (key decisions stay visible)."},
	{Name: "sv_mem_unpin", Description: "Clear the pinned flag on a local memory."},
	{Name: "sv_mem_capture_passive", Description: "Log lightweight observations (files modified, tests failing) without a save decision."},
	{Name: "sv_mem_conflicts", Description: "List, scan, or ignore potential memory conflicts."},
	{Name: "sv_graph_query", Description: "Query the dependency graph for a module, file, or package (returns Mermaid)."},
	{Name: "sv_graph_path", Description: "Find the shortest dependency path between two nodes."},
	{Name: "sv_graph_sync", Description: "Incrementally re-scan the codebase and rebuild the dependency graph. Call after adding major files or restructuring packages."},
	{Name: "sv_graph_explain", Description: "Explain a node's role, community, centrality, neighbors, and suggested questions. Use before refactoring or deleting a file."},
	{Name: "sv_graph_god_nodes", Description: "List the most-connected hub nodes in the dependency graph."},
	{Name: "sv_graph_surprising_connections", Description: "Find unexpected cross-community connections in the codebase."},
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
// struct (instead of closures in NewServer) keeps the 28 tool handlers in
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
	s.gitCache = gitMetadata{
		branch: config.GetGitBranch(s.cfg.ProjPath),
		commit: config.GetGitCommit(s.cfg.ProjPath),
		author: config.GetGitAuthor(s.cfg.ProjPath),
	}
	s.gitCacheAt = time.Now()
	return s.gitCache
}

// syncPathStat returns the path + mtime of the signal file/dir to watch:
// the chunks dir if it exists, otherwise the legacy memories.json.
func (s *Server) syncPathStat() (string, time.Time) {
	syncFile := filepath.Join(s.cfg.ProjPath, ".sv-memory", "memories.json")
	chunkDir := filepath.Join(s.cfg.ProjPath, ".sv-memory", "chunks")
	if fi, err := os.Stat(chunkDir); err == nil {
		return chunkDir, fi.ModTime()
	}
	if fi, err := os.Stat(syncFile); err == nil {
		return syncFile, fi.ModTime()
	}
	return "", time.Time{}
}

// maybeSyncFromGit imports shared memories only when their mtime advanced
// since the last call. Falls back to a full sync the first time (zero mtime).
func (s *Server) maybeSyncFromGit() {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	_, mtim := s.syncPathStat()
	if mtim.IsZero() {
		s.lastSyncMtim = time.Time{}
		return
	}
	if !mtim.After(s.lastSyncMtim) {
		return
	}
	start := time.Now()
	if err := memory.SyncFromGit(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
		fmt.Fprintf(os.Stderr, "[sv-memory] syncFromGit failed: %v\n", err)
		return
	}
	s.lastSyncMtim = mtim
	debugLog("syncFromGit pulled in %s", time.Since(start))
}

// scheduleSync resets the debounce timer. Each call to sv_mem_save invokes
// this instead of writing to disk immediately. The timer fires 500ms after the
// last save, so a burst of saves triggers only one Git write.
func (s *Server) scheduleSync() {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
	}
	s.syncVersion++
	currentVersion := s.syncVersion
	s.syncTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.debounceMu.Lock()
		if currentVersion != s.syncVersion {
			s.debounceMu.Unlock()
			return
		}
		s.debounceMu.Unlock()

		if !viper.GetBool("git_sync_enabled") {
			return
		}
		startSync := time.Now()
		if err := memory.SyncToGit(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
			fmt.Fprintf(os.Stderr, "[sv-memory] syncToGit (debounced) failed: %v\n", err)
			return
		}
		debugLog("syncToGit (debounced) took %s", time.Since(startSync))
		s.syncMu.Lock()
		_, mtim := s.syncPathStat()
		if !mtim.IsZero() {
			s.lastSyncMtim = mtim
		}
		s.syncMu.Unlock()
	})
}

// flushPendingSync stops the debounce timer and, if a write is pending, flushes
// the Git sync synchronously. Invoked during graceful shutdown. It forces the
// monolithic memories.json rewrite so the fallback file is always up to date.
func (s *Server) flushPendingSync() {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		if viper.GetBool("git_sync_enabled") {
			fmt.Fprintf(os.Stderr, "[sv-memory] Flushing pending Git sync...\n")
			if err := memory.SyncToGitForceFull(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
				fmt.Fprintf(os.Stderr, "[sv-memory] Final syncToGit failed: %v\n", err)
			}
		}
	}
}

// computeCentralityIfMissing recalculates communities and betweenness
// centrality when they are missing from the graph metadata.
// The caller should invalidate the cache and reload the graph afterwards
// via GlobalGraphCache.Invalidate + getOrLoadGraph().
func (s *Server) computeCentralityIfMissing() {
	_ = graph.UpdateCommunitiesAndCentrality(s.pool.Writer, s.cfg.ProjectID)
	graph.GlobalGraphCache.Invalidate(s.cfg.ProjectID)
}

// getOrLoadGraph returns the in-memory graph for the active project, refreshing
// it lazily only when files changed on disk. This keeps the graph current
// across edits without a filesystem watcher, and without re-scanning on every
// query when nothing changed.
func (s *Server) getOrLoadGraph() (*graph.InMemoryGraph, error) {
	startStale := time.Now()
	if synced, err := graph.SyncGraphIfStale(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
		return nil, err
	} else if synced {
		debugLog("graph lazy sync ran in %s", time.Since(startStale))
	}

	if cached, ok := graph.GlobalGraphCache.Get(s.pool.Reader, s.cfg.ProjectID); ok {
		return cached, nil
	}

	s.graphMu.Lock()
	defer s.graphMu.Unlock()

	// Double-check after lock
	if cached, ok := graph.GlobalGraphCache.Get(s.pool.Reader, s.cfg.ProjectID); ok {
		return cached, nil
	}

	var count int
	if err := s.pool.Reader.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", s.cfg.ProjectID).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		startBuild := time.Now()
		if err := graph.SyncGraph(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
			return nil, err
		}
		debugLog("graph_query auto-built graph in %s", time.Since(startBuild))
	}
	g, err := graph.LoadFullGraph(s.pool.Reader, s.cfg.ProjectID)
	if err != nil {
		return nil, err
	}

	var fileCount sql.NullInt64
	var maxMtime sql.NullInt64
	_ = s.pool.Reader.QueryRow("SELECT COUNT(*), COALESCE(MAX(mtime_ms), 0) FROM graph_files_meta WHERE project_id = ?", s.cfg.ProjectID).Scan(&fileCount, &maxMtime)
	graph.GlobalGraphCache.Put(s.cfg.ProjectID, g, int(fileCount.Int64), maxMtime.Int64)
	return g, nil
}

// maxFieldChars is the default maximum character count per text field in
// sv_mem_get responses. When a field exceeds this limit it is truncated
// with a "[truncated N chars]" suffix to keep token consumption bounded.
// Callers can override with the max_chars tool argument (0 = unlimited).
const maxFieldChars = 1000

// timelineWhyChars caps the rationale shown for the central observation in
// sv_mem_timeline, keeping the response lean while avoiding a full
// sv_mem_get round-trip.
const timelineWhyChars = 200

// similarCheckTimeout bounds how long a save waits for the similar-memories
// hint. The search is best-effort; exceeding this budget just omits the hint.
const similarCheckTimeout = 200 * time.Millisecond

// searchExpandChars caps the why/learned fields shown inline for the top
// search result, keeping the expanded section token-efficient.
const searchExpandChars = 300

// truncateField shortens a string to maxChars and appends a truncation
// notice when the original length exceeds the limit.
func truncateField(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("... [truncated %d chars]", len(s)-maxChars)
}

// resolveTokenBudget returns the token budget for a tool response. An explicit
// per-tool token_budget argument wins when positive; otherwise the global
// max_response_tokens config default applies (0 = unlimited).
func resolveTokenBudget(req mcp.CallToolRequest, explicit string) int {
	budget := 0
	if explicit != "" {
		if t, convErr := strconv.Atoi(explicit); convErr == nil && t > 0 {
			budget = t
		}
	}
	if budget <= 0 {
		budget = viper.GetInt("max_response_tokens")
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

// truncateToTokenBudget caps a built response to roughly tokenBudget tokens
// (chars/4) when it exceeds that limit. Truncation cuts at the last newline so
// lines stay intact, and a notice explains how to get the full output.
func truncateToTokenBudget(responseText string, tokenBudget int) string {
	if tokenBudget <= 0 || len(responseText) <= tokenBudget*4 {
		return responseText
	}
	maxChars := tokenBudget * 4
	truncated := responseText[:maxChars]
	if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > 0 {
		truncated = truncated[:lastNewline]
	}
	return fmt.Sprintf(
		"[!] Response truncated to ~%d tokens (~%d chars) of estimated %d total. Narrow the query or increase token_budget.\n\n%s",
		tokenBudget, maxChars, len(responseText)/4, truncated)
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

// NewServer initializes the MCP server, registers all 28 tools, and returns it.
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
		mcp.WithDescription("Persist a key architectural decision, bug fix, progress journal, or standard guidelines to the project's memory. Supports optional topic_key for upsert semantics (update in place on same project+topic) and session_id for session association."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("Concise description of the decision, standard, or fix")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Detailed reasoning for this choice")),
		mcp.WithString("learned", mcp.Required(), mcp.Description("Rule or key lesson to guide future agents")),
		mcp.WithString("where_path", mcp.Description("Optional file or folder path affected by this memory")),
		mcp.WithString("impact", mcp.Description("Achievements, successes, or what went well")),
		mcp.WithString("errors_faced", mcp.Description("Errors faced, roadblocks, or what went wrong")),
		mcp.WithString("next_steps", mcp.Description("Next actions or pending tasks to continue work")),
		mcp.WithString("topic_key", mcp.Description("Optional stable topic key for upsert semantics. When set, saves to the same project+topic update in place instead of creating a new record. Format: 'category/kebab-case-description'. Use sv_mem_suggest_topic_key to generate one.")),
		mcp.WithString("session_id", mcp.Description("Optional session ID to associate this memory with an active session.")),
	)
	ms.AddTool(saveTool, s.handleSave)

	// 2. Tool: sv_mem_suggest_topic_key
	suggestTool := mcp.NewTool("sv_mem_suggest_topic_key",
		mcp.WithDescription("Suggest a stable topic_key for an evolving topic before saving. The key follows 'category/kebab-case-description' format and enables upsert semantics in sv_mem_save."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("The title or description of the memory")),
	)
	ms.AddTool(suggestTool, s.handleSuggestTopicKey)

	// 3. Tool: sv_mem_session_start
	sessionStartTool := mcp.NewTool("sv_mem_session_start",
		mcp.WithDescription("Register the start of a new coding session and receive an Auto-Boot Context Bundle: previous session summary, key architectural decisions, standards, recent bugfixes, last journals, and top graph hubs. Call this at the beginning of every work session to enable session grouping and post-compaction context recovery."),
		mcp.WithString("goal", mcp.Description("Optional goal or objective for this session")),
		mcp.WithString("directory", mcp.Description("Optional working directory (auto-detected from repo if omitted)")),
	)
	ms.AddTool(sessionStartTool, s.handleSessionStart)

	// 4. Tool: sv_mem_session_end
	sessionEndTool := mcp.NewTool("sv_mem_session_end",
		mcp.WithDescription("End an active coding session with a summary. Call this before finishing a work session to enable context recovery via sv_mem_context."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The session ID to end")),
		mcp.WithString("summary", mcp.Description("Optional summary of what was accomplished")),
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
		mcp.WithDescription("List memories that may need attention: old, stale, high duplicates, or candidates for consolidation. Useful for high-level maintenance."),
		mcp.WithDeferLoading(true),
	)
	ms.AddTool(reviewTool, s.handleReview)

	// 14. Tool: sv_mem_stats
	statsTool := mcp.NewTool("sv_mem_stats",
		mcp.WithDescription("Get aggregate memory statistics for the current project: total memories, per-category breakdown, session counts, recent activity, and relation count."),
	)
	ms.AddTool(statsTool, s.handleStats)

	// 15. Tool: sv_mem_current_project
	currentProjectTool := mcp.NewTool("sv_mem_current_project",
		mcp.WithDescription("Get the current project's ID and display name. Useful for confirming which project context is active before saving or searching."),
	)
	ms.AddTool(currentProjectTool, s.handleCurrentProject)

	// 16. Tool: sv_mem_delete
	deleteTool := mcp.NewTool("sv_mem_delete",
		mcp.WithDescription("Delete a memory. Soft delete (default) marks it as deleted but preserves it in the database for potential recovery. Hard delete removes it permanently."),
		mcp.WithDeferLoading(true),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to delete")),
		mcp.WithString("hard", mcp.Description("Set to 'true' for permanent hard delete (default: 'false' = soft delete)")),
	)
	ms.AddTool(deleteTool, s.handleDelete)

	// 17. Tool: sv_mem_pin
	pinTool := mcp.NewTool("sv_mem_pin",
		mcp.WithDescription("Pin a local memory so it surfaces first in sv_mem_context (key decisions stay visible). Pinned state is local to this device."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to pin")),
	)
	ms.AddTool(pinTool, s.handlePin)

	// 17b. Tool: sv_mem_unpin
	unpinTool := mcp.NewTool("sv_mem_unpin",
		mcp.WithDescription("Clear the pinned flag on a local memory."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to unpin")),
	)
	ms.AddTool(unpinTool, s.handleUnpin)

	// 18. Tool: sv_mem_capture_passive
	captureTool := mcp.NewTool("sv_mem_capture_passive",
		mcp.WithDescription("Save a lightweight passive observation (e.g. 'modified file X', 'test Y failed'). Unlike sv_mem_save, this requires no explicit decision — it logs context automatically. Category is set to 'journal'."),
		mcp.WithString("what", mcp.Required(), mcp.Description("Brief description of what happened")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Context or reason for the observation")),
	)
	ms.AddTool(captureTool, s.handleCapturePassive)

	// 19. Tool: sv_graph_query
	graphQueryTool := mcp.NewTool("sv_graph_query",
		mcp.WithDescription("Retrieve project code structure, connections, imports, and dependencies for a given module, file, or package."),
		mcp.WithString("path_or_node", mcp.Required(), mcp.Description("The file path, package name, or module to inspect")),
		mcp.WithString("depth", mcp.Description("Hop distance depth in the dependency graph (default is '1')")),
		mcp.WithString("relation_type", mcp.Description("Filter by relation type ('imports', 'calls', 'depends_on')")),
		mcp.WithString("direction", mcp.Description("Filter by direction ('in', 'out', 'all')")),
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
		mcp.WithDescription("Manage potential memory conflicts in the project: list, scan, or ignore conflicts."),
		mcp.WithDeferLoading(true),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action to perform: list, scan, or ignore")),
		mcp.WithString("status", mcp.Description("Optional status filter for list (pending, judged, ignored)")),
		mcp.WithString("relation_id", mcp.Description("Required for ignore action: the conflict relation ID to ignore")),
		mcp.WithString("threshold", mcp.Description("Optional similarity threshold for scan (default: '0.45')")),
		mcp.WithString("apply", mcp.Description("For scan: 'true' to save scanned conflicts to database (default: 'false')")),
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

	// 26. Tool: sv_graph_viz
	vizTool := mcp.NewTool("sv_graph_viz",
		mcp.WithDescription("Generate an interactive HTML visualization (graph.html) of the project dependency graph"),
		mcp.WithDeferLoading(true),
		mcp.WithString("output", mcp.Description("Output HTML file path (default 'graph.html')")),
	)
	ms.AddTool(vizTool, s.handleGraphViz)

	// 27. Tool: sv_graph_merge
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
