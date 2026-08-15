package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

// handleContextPack returns the fused, token-efficient context for a code path
// (file, package, or symbol): the node's structural role in the dependency
// graph plus the memories linked to it via where_path or rationale_for edges.
// One bounded call replaces the sv_graph_explain + sv_mem_search(path) +
// sv_mem_get round-trips, and the response is guarded by the shared token
// budget (max_response_tokens or a per-call token_budget).
func (s *Server) handleContextPack(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError("missing required field: path"), nil
	}
	includeChanges := req.GetString("include_changes", "") == "true"

	pack, err := memory.GetContextPack(s.pool.Reader, s.cfg.ProjectID, path, configuredInt("context_pack_max_memories", 5), includeChanges)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build context pack: %v", err)), nil
	}

	return s.respond(req, memory.RenderContextPack(pack, memory.BundleWhyCharsLimit())), nil
}
