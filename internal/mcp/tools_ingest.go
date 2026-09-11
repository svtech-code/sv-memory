package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleFetchReference(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url, err := request.RequireString("url")
	if err != nil || url == "" {
		return mcp.NewToolResultError("missing required field: url"), nil
	}

	text, err := memory.FetchReference(ctx, url)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch %s: %v", url, err)), nil
	}

	res := fmt.Sprintf("Source: %s\n\n%s", url, text)
	return mcp.NewToolResultText(res), nil
}
