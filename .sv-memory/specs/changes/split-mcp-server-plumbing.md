# Split mcp.go server plumbing into cohesive files

- **ID:** `273f4e70a3d84907`
- **Slug:** `split-mcp-server-plumbing`
- **Status:** `applied`
- **Where:** `internal/mcp/mcp.go`
- **Capability:** `mcp/server-plumbing`
- **Created:** 2026-08-15T23:03:49-04:00

## Proposal

mcp.go is 833 lines mixing the server core (Server struct, AllTools registry, tool registration in NewServer) with three cohesive plumbing concerns. Split the plumbing into new files in the same package: server_sync.go (syncPathStat, maybeSyncFromGit, scheduleSync, flushPendingSync), graph_load.go (computeCentralityIfMissing, getOrLoadGraph, relinkMemoryRationales, relinkSpecCapabilities), and respond.go (response/token-budget constants and helpers: configuredInt/Bool, truncateField, resolveTokenBudget, truncateToTokenBudget, respond, SessionEstimatedTokens, ResetSessionTokens). mcp.go drops to ~550 lines keeping imports, debug helpers, AllTools, Server struct, git metadata cache, StartServer, and NewServer (all mcp.NewTool registrations stay here, so the TestAllToolsMatchesRegisteredTools guard test remains intact). Pure file reorganization: all methods stay on the same Server struct, same package, no runtime or API change.

## Design

Extract three contiguous, cohesive blocks from mcp.go into new files of the same package. All extracted code is unexported and referenced only via s.method() from tools_*.go, so the move is transparent. Files:
1. server_sync.go — lines 169-257 (syncPathStat, maybeSyncFromGit, scheduleSync, flushPendingSync). Imports: fmt, os, path/filepath, time, viper, memory. Uses Server fields syncMu/debounceMu/syncTimer/syncVersion/lastSyncMtim.
2. graph_load.go — lines 259-339 (computeCentralityIfMissing, getOrLoadGraph, relinkMemoryRationales, relinkSpecCapabilities). Imports: database/sql, fmt, os, graph, memory. Uses Server field graphMu.
3. respond.go — lines 341-450 (constants maxFieldChars/timelineWhyChars/similarCheckTimeout/searchExpandChars, configuredInt, configuredBool, truncateField, resolveTokenBudget, truncateToTokenBudget, respond, SessionEstimatedTokens, ResetSessionTokens). Imports: fmt, strings, strconv, mcp, viper, memory. Uses Server field sessionTokens.
NewServer stays in mcp.go with all tool registrations so the guard test that greps mcp.go for mcp.NewTool( stays green.