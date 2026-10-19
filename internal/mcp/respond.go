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
	final := truncateToTokenBudget(text, resolveTokenBudget(req.GetString("token_budget", ""), s.cfg.Settings.MaxResponseTokens))
	s.sessionTokens.Add(int64(len(final) / 4))
	return mcp.NewToolResultText(final)
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
