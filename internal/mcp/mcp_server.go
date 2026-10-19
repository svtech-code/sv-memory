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

	"github.com/mark3labs/mcp-go/server"

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

// fullToolsEnabled reports whether SV_MEMORY_FULL_TOOLS env var is set to a
// truthy value. When false (the default), the MCP server registers only the
// core tool set, omitting maintenance/admin tools so the per-request tool
// surface advertised to agents stays small (token economy). Opt-in restores
// the full surface for admin/diagnostic work.
func fullToolsEnabled() bool {
	v := os.Getenv("SV_MEMORY_FULL_TOOLS")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

// serverInstructions is the server-wide guidance returned in the MCP initialize
// response. Spec-compliant clients (notably OpenAI Codex) inject this string
// automatically as server-level instructions, so the memory-first / graph-first /
// save-after-decision / spec-cycle protocol reaches the model without any
// per-platform plugin or hook. Keep the opening sentences self-contained: some
// clients surface only the first ~512 characters when deciding how to use the
// server's tools.
const serverInstructions = "sv-memory provides persistent architectural memory and a code dependency graph. Start every session by calling sv_mem_session_start to load your Auto-Boot Context Bundle (prior session summary, key decisions, standards, and bugfixes). Workflow: (1) search memory first — call sv_mem_search before answering project questions or proposing solutions; (2) explore the graph before reading source — call sv_graph_explore or sv_graph_search instead of grep/read; (3) save knowledge immediately — call sv_mem_save after design decisions, complex bugfixes, or new standards; (4) for behavior or architecture changes, follow the Spec-Driven Decision Cycle (sv_spec_list -> sv_propose_spec -> sv_update_spec -> sv_validate_decision -> sv_commit_spec). Match tools by their sv_* suffix: your client may prefix tool names with a server namespace (e.g. sv-memory_sv_graph_explore)."

type Server struct {
	pool *db.Pool
	cfg  *config.Config

	// fileWatcher is the background watcher for the active project, assigned by
	// StartServer before ServeStdio begins serving. It is set once before any
	// tool handler can run and only read afterwards, so it needs no locking. It
	// lets getOrLoadGraph skip the O(n) DetectStaleFiles walk when the watcher
	// has already kept the graph fresh in the background.
	fileWatcher *graph.FileWatcher

	// cleanup releases background resources (the pending git-sync flush) at
	// shutdown. Set at construction; invoked at most once via cleanupOnce by
	// whichever shutdown path fires first (signal handler or ServeStdio return).
	cleanup     func()
	cleanupOnce sync.Once

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
	s, ms := newServer(pool, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Settings.AutoCompactionEnabled {
		memory.StartAutoCompaction(ctx, pool.Writer, cfg.ProjectID, cfg.Settings.CompactionIntervalMin)
	}

	// Start the background file watcher to keep the graph fresh without
	// requiring DetectStaleFiles per query. Default enabled; disable via
	// `sv-memory configure set graph_watcher_enabled false`.
	if cfg.Settings.GraphWatcherEnabled {
		debounceMs := cfg.Settings.GraphWatchDebounceMs
		debounce := time.Duration(debounceMs) * time.Millisecond
		s.fileWatcher = graph.StartWatcher(ctx, pool.Writer, cfg.ProjectID, cfg.ProjPath,
			graph.WatcherConfig{Debounce: debounce})
	}

	go func() {
		<-ctx.Done()
		debugLog("Shutdown signal received. Running cleanup...")
		s.runCleanup()
		os.Exit(0)
	}()

	err := server.ServeStdio(ms)

	s.runCleanup()

	return err
}

// runCleanup invokes the server's cleanup hook (pending git-sync flush) exactly
// once, so the two shutdown paths (signal handler and ServeStdio return) cannot
// flush twice when they race.
func (s *Server) runCleanup() {
	s.cleanupOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

// newServer builds the MCP protocol server together with its Server state.
// Keeping the Server instance reachable here lets StartServer drive the file
// watcher and the shutdown cleanup on the same instance, without package-level
// globals. NewServer wraps it for callers (tests) that only need the protocol
// server.
func newServer(pool *db.Pool, cfg *config.Config) (*Server, *server.MCPServer) {
	s := &Server{pool: pool, cfg: cfg}
	s.cleanup = s.flushPendingSync

	ms := server.NewMCPServer("sv-memory", "1.0.0", server.WithInstructions(serverInstructions))

	registerCoreMemoryTools(ms, s)
	registerSessionTools(ms, s)
	registerSearchAndReviewTools(ms, s)
	registerLifecycleTools(ms, s)
	registerSpecTools(ms, s)
	registerGraphQueryTools(ms, s)
	registerGraphAnalysisTools(ms, s)
	registerReferenceTools(ms, s)

	return s, ms
}

// NewServer initializes the MCP server, registers all tools, and returns it.
// Split from StartServer for programmatic unit testing. Tool definitions and
// handler wiring live here (single source of truth for the tool surface, kept
// in sync with AllTools); the handlers themselves are Server methods defined
// in the per-domain tools_*.go files.
func NewServer(pool *db.Pool, cfg *config.Config) *server.MCPServer {
	_, ms := newServer(pool, cfg)
	return ms
}
