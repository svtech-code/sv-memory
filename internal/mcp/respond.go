package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// maxFieldTruncateChars is the default maximum character count per text field
// in sv_mem_get responses. When a field exceeds this limit it is truncated
// with a "[truncated N chars]" suffix to keep token consumption bounded.
// Callers can override with the max_chars tool argument (0 = unlimited).
// Tunable via the max_field_chars config key; this constant is the fallback.
const maxFieldTruncateChars = 1000

// timelineWhyChars caps the rationale shown for the central observation in
// sv_mem_timeline, keeping the response lean while avoiding a full
// sv_mem_get round-trip. Tunable via the timeline_why_chars config key.
const timelineWhyChars = 200

// similarCheckTimeout bounds how long a save waits for the similar-memories
// hint. The search is best-effort; exceeding this budget just omits the hint.
const similarCheckTimeout = 200 * time.Millisecond

// searchExpandChars caps the why/learned fields shown inline for the top
// search result, keeping the expanded section token-efficient. Tunable via the
// search_expand_chars config key.
const searchExpandChars = 300

// configuredInt returns a configured positive integer (a truncation limit or a
// memory count, depending on the key), falling back to the compiled-in default
// when the config key is unset or non-positive. This makes the limits tunable
// via ~/.sv-memory/config.yaml (global) or .sv-memory/config.yaml (local)
// without recompiling, while keeping the constants above as the safe defaults.
func configuredInt(key string, fallback int) int {
	if v := viper.GetInt(key); v > 0 {
		return v
	}
	return fallback
}

// configuredBool returns a configured boolean flag, falling back to the given
// default when the config key is unset (viper.Get returns nil for unset keys).
// Used for feature toggles like graph_boost that default on in production but
// must not depend on config loading inside unit tests.
func configuredBool(key string, fallback bool) bool {
	if v := viper.Get(key); v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return fallback
}

// truncateField shortens a string to maxChars with a truncation notice. It
// delegates to memory.TruncateText (rune-safe) so all truncation in the tool
// responses shares one implementation.
func truncateField(s string, maxChars int) string {
	return memory.TruncateText(s, maxChars)
}

// resolveTokenBudget returns the token budget for a tool response. An explicit
// per-tool token_budget argument wins when positive; otherwise the global
// max_response_tokens config default applies (0 = unlimited).
func resolveTokenBudget(explicit string) int {
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

// respond wraps a text response with the shared token-budget truncation. The
// per-call token_budget argument wins; otherwise the global default applies.
// It also accrues the estimated token count (chars/4) of the final text into
// the session token ledger so sv_mem_stats can report context injected so far.
func (s *Server) respond(req mcp.CallToolRequest, text string) *mcp.CallToolResult {
	final := truncateToTokenBudget(text, resolveTokenBudget(req.GetString("token_budget", "")))
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
