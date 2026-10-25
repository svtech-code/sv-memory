package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// similarCheckTimeout bounds how long a save waits for the similar-memories
// hint. The search is best-effort; exceeding this budget just omits the hint.
const similarCheckTimeout = 200 * time.Millisecond

// truncateField shortens a string to maxChars with a truncation notice. It
// delegates to memory.TruncateText (rune-safe) so all truncation in the tool
// responses shares one implementation.
func truncateField(s string, maxChars int) string {
	return memory.TruncateText(s, maxChars)
}

// resolveTokenBudget returns the token budget for a tool response. An explicit
// per-tool token_budget argument wins when positive; otherwise the supplied
// default applies (from Settings.MaxResponseTokens, where 0 = unlimited).
func resolveTokenBudget(explicit string, defaultBudget int) int {
	budget := 0
	if explicit != "" {
		if t, convErr := strconv.Atoi(explicit); convErr == nil && t > 0 {
			budget = t
		}
	}
	if budget <= 0 {
		budget = defaultBudget
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

// respond wraps a text response with the shared token-budget truncation. The
// per-call token_budget argument wins; otherwise the global default applies.
// It also accrues the estimated token count (chars/4) of the final text into
// the session token ledger so sv_mem_stats can report context injected so far.
func (s *Server) respond(req mcp.CallToolRequest, text string) *mcp.CallToolResult {
	if prefix := s.ensureSessionAutoBoot(req.Params.Name); prefix != "" {
		text = prefix + text
	}
	final := truncateToTokenBudget(text, resolveTokenBudget(req.GetString("token_budget", ""), s.cfg.Settings.MaxResponseTokens))
	s.sessionTokens.Add(int64(len(final) / 4))
	return mcp.NewToolResultText(final)
}

// isSessionLifecycleTool reports whether toolName manages the session
// lifecycle directly and therefore should not trigger lazy auto-boot.
func isSessionLifecycleTool(toolName string) bool {
	switch toolName {
	case "sv_mem_session_start", "sv_mem_session_end", "sv_mem_session_summary", "sv_mem_context":
		return true
	default:
		return false
	}
}

// markSessionBooted marks the server session as booted and resets per-session
// tool chaining trackers. Called when sv_mem_session_start runs explicitly or
// when lazy auto-boot starts a session.
func (s *Server) markSessionBooted() {
	s.sessionBooted.Store(true)
	s.usedMemSearch.Store(false)
	s.usedGraphExplore.Store(false)
	s.usedSpecFlow.Store(false)
}

// ensureSessionAutoBoot lazily starts an active session on the first non-session
// tool call when neither a platform hook nor sv_mem_session_start has started
// one. Returns a concise 1-line notice prompting the agent to call
// sv_mem_session_start for the full Auto-Boot Context Bundle while guaranteeing
// an active session in SQLite immediately.
func (s *Server) ensureSessionAutoBoot(toolName string) string {
	if isSessionLifecycleTool(toolName) || s.sessionBooted.Load() {
		return ""
	}
	s.autoBootMu.Lock()
	defer s.autoBootMu.Unlock()
	if s.sessionBooted.Load() {
		return ""
	}
	s.markSessionBooted()

	if s.pool == nil || s.cfg == nil {
		return ""
	}

	staleHours := s.cfg.Settings.StaleSessionHours
	if staleHours > 0 {
		_, _ = memory.CloseStaleSessions(s.pool.Writer, s.cfg.ProjectID, time.Duration(staleHours)*time.Hour)
	}

	if active, err := memory.GetActiveSession(s.pool.Reader, s.cfg.ProjectID); err == nil && active != nil {
		return ""
	}

	session, err := memory.StartSession(s.pool.Writer, s.cfg.ProjectID, "", s.cfg.ProjPath)
	if err != nil || session == nil {
		return ""
	}
	s.lazySessionID = session.ID

	return fmt.Sprintf("> **[sv-memory Auto-Boot]** Session `%s` auto-started (call `sv_mem_session_start` to load the Auto-Boot Context Bundle, and `sv_mem_session_end` when done).\n\n", session.ID)
}

// consumeLazySession returns and clears a session that was lazily auto-started
// by ensureSessionAutoBoot so an explicit sv_mem_session_start call reuses it
// instead of creating a duplicate session row.
func (s *Server) consumeLazySession(goal string) *memory.Session {
	s.autoBootMu.Lock()
	defer s.autoBootMu.Unlock()
	if s.lazySessionID == "" {
		return nil
	}
	id := s.lazySessionID
	s.lazySessionID = ""
	active, err := memory.GetActiveSession(s.pool.Reader, s.cfg.ProjectID)
	if err != nil || active == nil || active.ID != id {
		return nil
	}
	if goal != "" && s.pool != nil {
		_, _ = s.pool.Writer.Exec(`UPDATE sessions SET goal = ? WHERE id = ?`, goal, id)
		active.Goal = goal
	}
	return active
}

// nextStepAfterMemorySearch returns a 1-line adaptive guidance footer for
// sv_mem_search based on which tools have already been used in this session.
func (s *Server) nextStepAfterMemorySearch() string {
	s.usedMemSearch.Store(true)
	if !s.usedGraphExplore.Load() {
		return "\n*Next step: call `sv_mem_get(id=\"<id>\")` for full memory details, and `sv_graph_explore(path=\"<file|symbol>\")` (or `sv_graph_search`) BEFORE reading source files.*\n"
	}
	return "\n*Next step: call `sv_mem_get(id=\"<id>\")` for full memory details, or `sv_propose_spec` before changing behavior/architecture.*\n"
}

// nextStepAfterGraphExplore returns a 1-line adaptive guidance footer for
// sv_graph_explore based on which tools have already been used in this session.
func (s *Server) nextStepAfterGraphExplore() string {
	s.usedGraphExplore.Store(true)
	if !s.usedMemSearch.Load() {
		return "\n*Next step: call `sv_mem_search(query=\"...\")` to check past decisions/bugfixes on this topic, `sv_mem_get(id=\"<id>\")` to inspect a linked memory, or `sv_propose_spec` before changing behavior/architecture.*\n"
	}
	if !s.usedSpecFlow.Load() {
		return "\n*Next step: call `sv_mem_get(id=\"<id>\")` for full linked memory details, or start the Spec Cycle (`sv_spec_list` → `sv_propose_spec`) before changing behavior/architecture.*\n"
	}
	return ""
}

// nextStepAfterGraphSearch returns a 1-line adaptive guidance footer for
// sv_graph_search based on which tools have already been used in this session.
func (s *Server) nextStepAfterGraphSearch() string {
	if !s.usedMemSearch.Load() {
		return "\n*Next step: call `sv_graph_explore(path=\"<node1>,<node2>\")` for surgical source + call paths + blast radius (or `sv_graph_explain(node=\"<node>\")`), and `sv_mem_search(query=\"...\")` for past decisions.*\n"
	}
	return "\n*Next step: call `sv_graph_explore(path=\"<node1>,<node2>\")` for surgical source + call paths + blast radius, or `sv_graph_explain(node=\"<node>\")` / `sv_graph_query(path_or_node=\"<node>\")`.*\n"
}

// SessionEstimatedTokens returns the running estimate of tokens injected into
// the agent context since the last sv_mem_session_start (chars/4 heuristic).
func (s *Server) SessionEstimatedTokens() int64 {
	return s.sessionTokens.Load()
}

// ResetSessionTokens clears the session token ledger, called at session start
// so the Auto-Boot bundle becomes the first counted injection of the session.
func (s *Server) ResetSessionTokens() {
	s.sessionTokens.Store(0)
}
