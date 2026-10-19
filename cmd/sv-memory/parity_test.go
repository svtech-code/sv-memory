package main

import (
	"strconv"
	"testing"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func flagDefString(t *testing.T, cmd *cobra.Command, name string) string {
	t.Helper()
	f := cmd.Flags().Lookup(name)
	if f == nil {
		t.Fatalf("%s: flag --%s is not registered", cmd.Name(), name)
	}
	return f.DefValue
}

func flagDefInt(t *testing.T, cmd *cobra.Command, name string) int {
	t.Helper()
	raw := flagDefString(t, cmd, name)
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s --%s default %q is not an int: %v", cmd.Name(), name, raw, err)
	}
	return v
}

func flagDefFloat(t *testing.T, cmd *cobra.Command, name string) float64 {
	t.Helper()
	raw := flagDefString(t, cmd, name)
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("%s --%s default %q is not a float: %v", cmd.Name(), name, raw, err)
	}
	return v
}

func flagDefBool(t *testing.T, cmd *cobra.Command, name string) bool {
	t.Helper()
	raw := flagDefString(t, cmd, name)
	v, err := strconv.ParseBool(raw)
	if err != nil {
		t.Fatalf("%s --%s default %q is not a bool: %v", cmd.Name(), name, raw, err)
	}
	return v
}

// TestCLIMCPDefaultParity guards the six operations exposed by both front-ends
// against silent default drift. For each op the CLI flag default MUST equal the
// shared constant the MCP handler reads, so behavior is identical whichever
// surface is used:
//
//  1. conflicts scan    `conflicts scan`        ↔ sv_mem_conflicts action=scan
//  2. compact           `compact`               ↔ sv_mem_compact
//  3. context           `context`               ↔ sv_mem_context
//  4. graph path        `graph path`            ↔ sv_graph_path
//  5. graph communities `graph communities`     ↔ sv_graph_communities
//  6. graph diff        `graph diff`            ↔ sv_graph_diff
//
// Known presentation-only divergence (intentionally not asserted): `graph
// communities` prints every community (at most 8 members each), while
// sv_graph_communities lists only top_n (default graph.DefaultTopCommunities)
// and can drill into one community. Their shared numeric default is still
// guarded below.
func TestCLIMCPDefaultParity(t *testing.T) {
	// 1. conflicts scan (sv_mem_conflicts action=scan).
	if got := flagDefFloat(t, conflictsScanCmd, "threshold"); got != config.DefaultConflictThreshold {
		t.Errorf("conflicts scan --threshold default = %v, MCP uses %v", got, config.DefaultConflictThreshold)
	}
	if got := flagDefInt(t, conflictsScanCmd, "max-insert"); got != memory.DefaultScanLimit {
		t.Errorf("conflicts scan --max-insert default = %d, MCP uses %d", got, memory.DefaultScanLimit)
	}

	// 2. compact (sv_mem_compact).
	if got := flagDefInt(t, compactCmd, "threshold"); got != memory.DefaultCompactionThreshold {
		t.Errorf("compact --threshold default = %d, MCP uses %d", got, memory.DefaultCompactionThreshold)
	}

	// 3. context (sv_mem_context).
	if got := flagDefInt(t, contextCmd, "max-memories"); got != config.DefaultContextPackMaxMemories {
		t.Errorf("context --max-memories default = %d, MCP uses %d", got, config.DefaultContextPackMaxMemories)
	}
	if got := flagDefInt(t, contextCmd, "why-chars"); got != config.DefaultBundleWhyChars {
		t.Errorf("context --why-chars default = %d, MCP uses %d", got, config.DefaultBundleWhyChars)
	}

	// 4. graph path (sv_graph_path).
	if got := flagDefInt(t, graphPathCmd, "max-hops"); got != graph.DefaultMaxHops {
		t.Errorf("graph path --max-hops default = %d, MCP uses %d", got, graph.DefaultMaxHops)
	}

	// 5. graph communities (sv_graph_communities): the shared numeric default.
	if graph.DefaultTopCommunities != 10 {
		t.Errorf("graph.DefaultTopCommunities = %d, want 10 (MCP sv_graph_communities default)", graph.DefaultTopCommunities)
	}

	// 6. graph diff (sv_graph_diff).
	if got := flagDefBool(t, graphDiffCmd, "blast-radius"); !got {
		t.Errorf("graph diff --blast-radius default = false, MCP uses true")
	}
}
