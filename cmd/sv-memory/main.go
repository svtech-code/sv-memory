package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/svtech/sv-memory/internal/config"
	"github.com/svtech/sv-memory/internal/db"
	"github.com/svtech/sv-memory/internal/graph"
	"github.com/svtech/sv-memory/internal/mcp"
	"github.com/svtech/sv-memory/internal/memory"
	"github.com/svtech/sv-memory/internal/protocol"
)

var rootCmd = &cobra.Command{
	Use:   "sv-memory",
	Short: "sv-memory: Context Memory and Structural Code Graph for AI Agents",
	Long:  `sv-memory is a CLI tool and Model Context Protocol (MCP) server that records architectural decisions, coding guidelines, and code graphs to prevent context amnesia in AI agents.`,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize sv-memory for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		fmt.Printf("Initializing project: %s (ID: %s)\n", cfg.ProjName, cfg.ProjectID)
		fmt.Printf("Workspace root: %s\n", cfg.ProjPath)

		// 1. Initialize SQLite Database
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		// 2. Register project in database
		err = db.RegisterProject(database, cfg.ProjectID, cfg.ProjName, cfg.ProjPath)
		if err != nil {
			return err
		}

		// 3. Inject protocol rules into AGENTS.md / .cursorrules
		injected, err := protocol.InjectProtocol(cfg.ProjPath)
		if err != nil {
			return err
		}
		if len(injected) > 0 {
			fmt.Printf("Injected rules into: %s\n", fmt.Sprintf("%v", injected))
		}

		// 4. Sync memories from Git (.sv-memory/memories.json) if it exists
		err = memory.SyncFromGit(database, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			fmt.Printf("Warning: failed to import shared memories from Git: %v\n", err)
		} else {
			fmt.Println("Synced shared memories from Git (.sv-memory/memories.json).")
		}

		// 5. Scan project to build structural dependency graph (full rebuild on init)
		fmt.Println("Scanning files and building code dependency graph...")
		err = graph.SyncGraphFull(database, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			return fmt.Errorf("failed to build code graph: %w", err)
		}
		fmt.Println("Dependency graph built successfully in SQLite.")
		fmt.Println("sv-memory successfully initialized! You can now start the MCP server using 'sv-memory mcp'.")
		return nil
	},
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the Model Context Protocol (MCP) JSON-RPC stdio server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		pool, err := db.NewDBPool(cfg.DBPath)
		if err != nil {
			return err
		}
		defer pool.Close()

		// Register project in DB to ensure it exists
		err = db.RegisterProject(pool.Writer, cfg.ProjectID, cfg.ProjName, cfg.ProjPath)
		if err != nil {
			return err
		}

		// Sync from Git memories json in case of updates
		_ = memory.SyncFromGit(pool.Writer, cfg.ProjectID, cfg.ProjPath)

		// Start MCP Server
		return mcp.StartServer(pool, cfg)
	},
}

var obsidianExportCmd = &cobra.Command{
	Use:   "obsidian-export",
	Short: "Export all memories as Markdown files in Obsidian vault format",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		outputDir, _ := cmd.Flags().GetString("output")
		if outputDir == "" {
			outputDir = ".obsidian-sv-memory"
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		fmt.Printf("Exporting memories to Obsidian vault at %s...\n", filepath.Join(cfg.ProjPath, outputDir))
		if err := memory.ExportObsidian(database, cfg.ProjectID, cfg.ProjPath, outputDir); err != nil {
			return fmt.Errorf("obsidian export failed: %w", err)
		}
		fmt.Println("Export complete.")
		return nil
	},
}

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Run read-only health checks on the project setup (DB, schema, permissions)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		results := memory.RunDiagnostics(database, cfg.ProjectID, cfg.ProjPath, cfg.DBPath)

		fmt.Printf("=== Diagnostics for %s ===\n\n", cfg.ProjName)
		passCount, warnCount, failCount := 0, 0, 0
		for _, r := range results {
			switch r.Status {
			case "pass":
				passCount++
			case "warn":
				warnCount++
			case "fail":
				failCount++
			}
			fmt.Printf("[%s] %s\n", r.Status, r.Check)
			if r.Message != "" {
				fmt.Printf("   %s\n", r.Message)
			}
		}
		fmt.Printf("\n%d pass, %d warnings, %d failures\n", passCount, warnCount, failCount)
		if failCount > 0 {
			return fmt.Errorf("diagnostics found %d failure(s)", failCount)
		}
		return nil
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate memory statistics for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		s, err := memory.GetStats(database, cfg.ProjectID)
		if err != nil {
			return fmt.Errorf("failed to get stats: %w", err)
		}

		fmt.Printf("=== Memory Statistics for %s ===\n\n", cfg.ProjName)
		fmt.Printf("Total memories:    %d\n", s.TotalMemories)
		fmt.Printf("Deleted memories:  %d\n", s.DeletedMemories)
		fmt.Printf("Recent (24h):      %d\n", s.Recent24h)
		fmt.Printf("Total sessions:    %d\n", s.TotalSessions)
		fmt.Printf("Active sessions:   %d\n", s.ActiveSessions)
		fmt.Printf("Total relations:   %d\n", s.TotalRelations)
		if len(s.ByCategory) > 0 {
			fmt.Println("\nBy category:")
			for cat, count := range s.ByCategory {
				fmt.Printf("  %-15s %d\n", cat, count)
			}
		}
		return nil
	},
}

var exportCmd = &cobra.Command{
	Use:   "export [output-file]",
	Short: "Export all non-deleted memories to a portable JSON file",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		outputFile := "sv-memory-export.json"
		if len(args) > 0 {
			outputFile = args[0]
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		n, err := memory.ExportJSON(database, cfg.ProjectID, outputFile)
		if err != nil {
			return fmt.Errorf("export failed: %w", err)
		}

		fmt.Printf("Exported %d memories to %s\n", n, outputFile)
		return nil
	},
}

var importCmd = &cobra.Command{
	Use:   "import <input-file>",
	Short: "Import memories from a portable JSON file (upsert by ID)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		n, err := memory.ImportJSON(database, cfg.ProjectID, args[0])
		if err != nil {
			return fmt.Errorf("import failed: %w", err)
		}

		fmt.Printf("Imported %d memories from %s\n", n, args[0])
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete sessions or projects with cascade options",
}

var deleteSessionCmd = &cobra.Command{
	Use:   "session <session-id>",
	Short: "Delete a session (must have no associated memories)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		if err := memory.DeleteSession(database, args[0]); err != nil {
			return err
		}
		fmt.Printf("Session %s deleted successfully.\n", args[0])
		return nil
	},
}

var deleteProjectCmd = &cobra.Command{
	Use:   "project <project-id>",
	Short: "Cascade-delete a project (soft-deletes memories by default; --hard removes permanently)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		hard, _ := cmd.Flags().GetBool("hard")

		if err := memory.DeleteProject(database, args[0], hard); err != nil {
			return err
		}
		mode := "soft-deleted"
		if hard {
			mode = "permanently removed"
		}
		fmt.Printf("Project %s %s (cascade complete).\n", args[0], mode)
		return nil
	},
}

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage registered projects",
}

var projectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered projects with memory and session counts",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, err := config.GetDBPath()
		if err != nil {
			return err
		}
		database, err := db.InitDB(dbPath)
		if err != nil {
			return err
		}
		defer database.Close()

		projects, err := memory.ListProjects(database)
		if err != nil {
			return err
		}

		if len(projects) == 0 {
			fmt.Println("No projects registered.")
			return nil
		}

		fmt.Printf("%-20s %-8s %-5s  %s\n", "Name", "Memories", "Sess.", "Path")
		fmt.Println(strings.Repeat("-", 80))
		for _, p := range projects {
			fmt.Printf("%-20s %-8d %-5d  %s\n", p.Name, p.MemoryCount, p.SessionCount, p.Path)
		}
		return nil
	},
}

var projectsPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove projects with zero memories and zero sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, err := config.GetDBPath()
		if err != nil {
			return err
		}
		database, err := db.InitDB(dbPath)
		if err != nil {
			return err
		}
		defer database.Close()

		pruned, err := memory.PruneProjects(database)
		if err != nil {
			return err
		}

		if len(pruned) == 0 {
			fmt.Println("No empty projects to prune.")
			return nil
		}

		fmt.Printf("Pruned %d empty project(s):\n", len(pruned))
		for _, id := range pruned {
			fmt.Printf("  - %s\n", id)
		}
		return nil
	},
}

var projectsConsolidateCmd = &cobra.Command{
	Use:   "consolidate <source-project-id> <target-project-id>",
	Short: "Move all memories and sessions from source project to target, then delete source",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, err := config.GetDBPath()
		if err != nil {
			return err
		}
		database, err := db.InitDB(dbPath)
		if err != nil {
			return err
		}
		defer database.Close()

		mems, sess, err := memory.ConsolidateProjects(database, args[0], args[1])
		if err != nil {
			return err
		}

		fmt.Printf("Consolidated: moved %d memories and %d sessions from %s to %s\n", mems, sess, args[0], args[1])
		return nil
	},
}

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Manage the project dependency graph",
}

var graphPathCmd = &cobra.Command{
	Use:   "path <source> <target>",
	Short: "Find the shortest path between two nodes in the dependency graph",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		// Load graph using internal/graph package
		g, err := graph.LoadFullGraph(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		path := g.ShortestPath(args[0], args[1], 10)
		if len(path) == 0 {
			fmt.Printf("No path found between %s and %s.\n", args[0], args[1])
			return nil
		}
		fmt.Printf("Path found: %s\n", strings.Join(path, " -> "))
		return nil
	},
}

var graphExplainCmd = &cobra.Command{
	Use:   "explain <node>",
	Short: "Show detailed architectural explanation of a node in the dependency graph",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		g, err := graph.LoadFullGraph(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		nodeID := g.FindNode(args[0])
		if nodeID == "" {
			fmt.Printf("Node '%s' not found.\n", args[0])
			return nil
		}

		node := g.Nodes[nodeID]

		getBC := func(n *graph.Node) float64 {
			if n.Metadata == nil {
				return 0.0
			}
			val, ok := n.Metadata["betweenness_centrality"]
			if !ok {
				return 0.0
			}
			switch v := val.(type) {
			case float64:
				return v
			case float32:
				return float64(v)
			}
			return 0.0
		}
		getCommID := func(n *graph.Node) int {
			if n.Metadata == nil {
				return 0
			}
			val, ok := n.Metadata["community_id"]
			if !ok {
				return 0
			}
			switch v := val.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case int64:
				return int(v)
			}
			return 0
		}

		cID := getCommID(node)
		bc := getBC(node)
		fanIn := g.FanIn[nodeID]
		fanOut := g.FanOut[nodeID]

		fmt.Printf("=== Explanation for Node: %s ===\n\n", node.Label)
		fmt.Printf("ID:       %s\n", node.ID)
		fmt.Printf("Type:     %s\n", node.Type)
		fmt.Printf("Path:     %s\n", node.Path)
		if node.Metadata != nil {
			if lang, ok := node.Metadata["language"]; ok {
				fmt.Printf("Language: %v\n", lang)
			}
			if loc, ok := node.Metadata["loc"]; ok {
				fmt.Printf("LOC:      %v\n", loc)
			}
		}
		fmt.Printf("Community ID: %d\n", cID)
		fmt.Printf("Betweenness Centrality: %.2f\n", bc)
		fmt.Printf("Fan-in:   %d, Fan-out: %d\n\n", fanIn, fanOut)

		// God node evaluation
		isGod := false
		if fanIn > 10 || fanOut > 10 || bc > 50.0 {
			isGod = true
		}
		fmt.Print("Architectural Role: ")
		if isGod {
			fmt.Println("⚠️  Potential God Node / Hub")
		} else if fanIn == 0 && fanOut > 0 {
			fmt.Println("🟢 Entry Point / Controller")
		} else if fanIn > 0 && fanOut == 0 {
			fmt.Println("🟢 Leaf Node / Utility")
		} else if fanIn > 0 && fanOut > 0 {
			fmt.Println("🟢 Intermediate Component")
		} else {
			fmt.Println("🟢 Isolated Node")
		}

		var dependents []string
		var rationales []string
		for _, e := range g.EdgesByTarget[nodeID] {
			if e.RelationType == "rationale_for" {
				src := g.Nodes[e.SourceID]
				if src != nil {
					lineVal := 0
					if src.Metadata != nil {
						if l, ok := src.Metadata["line"]; ok {
							if lf, ok := l.(float64); ok {
								lineVal = int(lf)
							} else if li, ok := l.(int); ok {
								lineVal = li
							}
						}
					}
					rationales = append(rationales, fmt.Sprintf("  - Line %d: %s", lineVal, src.Label))
				}
			} else {
				src := g.Nodes[e.SourceID]
				if src != nil {
					dependents = append(dependents, fmt.Sprintf("  - %s (%s)", src.Label, e.RelationType))
				}
			}
		}

		if len(dependents) > 0 {
			fmt.Println("\nDependents (Who imports/calls this):")
			for _, dep := range dependents {
				fmt.Println(dep)
			}
		}
		if len(g.EdgesBySource[nodeID]) > 0 {
			fmt.Println("\nDependencies (What this imports/calls):")
			for _, e := range g.EdgesBySource[nodeID] {
				tgt := g.Nodes[e.TargetID]
				if tgt != nil {
					fmt.Printf("  - %s (%s)\n", tgt.Label, e.RelationType)
				}
			}
		}

		if len(rationales) > 0 {
			fmt.Println("\n💡 Code Rationales (Empirical Decisions in Comments):")
			for _, r := range rationales {
				fmt.Println(r)
			}
		}
		return nil
	},
}

var graphCommunitiesCmd = &cobra.Command{
	Use:   "communities",
	Short: "List top communities, their members, and auto-label them",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		g, err := graph.LoadFullGraph(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		comms := g.LeidenDetectCommunities()

		// Group nodes by community ID
		commGroups := make(map[int][]string)
		for id, node := range g.Nodes {
			cID := comms[id]
			commGroups[cID] = append(commGroups[cID], node.Label)
		}

		fmt.Printf("=== Graph Communities for %s ===\n\n", cfg.ProjName)
		if len(commGroups) == 0 {
			fmt.Println("No communities found.")
			return nil
		}

		for cID, members := range commGroups {
			// Auto-label: find the most common extension or content type
			extCounts := make(map[string]int)
			extCounts["other"] = 0
			for _, m := range members {
				if strings.Contains(m, ".") && !strings.Contains(m, " ") && !strings.Contains(m, "/") {
					parts := strings.Split(m, ".")
					ext := parts[len(parts)-1]
					if len(ext) <= 8 && ext != "" {
						extCounts[ext]++
						continue
					}
				}
				extCounts["other"]++
			}
			topLabel := "mixed"
			maxCount := 0
			for label, count := range extCounts {
				if count > maxCount {
					maxCount = count
					topLabel = label
				}
			}
			if topLabel == "other" {
				topLabel = "generic"
			}

			fmt.Printf("Community #%d (Auto-label: %s, size: %d):\n", cID, topLabel, len(members))
			limit := 8
			for i, m := range members {
				if i >= limit {
					fmt.Printf("    ... and %d more\n", len(members)-limit)
					break
				}
				fmt.Printf("  - %s\n", m)
			}
			fmt.Println()
		}

		return nil
	},
}

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the structural code graph by scanning files",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		fmt.Println("Rebuilding project dependency graph...")
		err = graph.SyncGraphFull(database, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			return err
		}
		fmt.Println("Dependency graph rebuild complete.")
		return nil
	},
}

var graphWikiCmd = &cobra.Command{
	Use:   "wiki",
	Short: "Export the dependency graph as a markdown wiki (per-community pages)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		g, err := graph.LoadFullGraph(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = "graph-wiki"
		}

		comms := g.LeidenDetectCommunities()
		centrality := g.BetweennessCentrality()
		commLabels := g.DetectCommunityLabels(comms, centrality)

		if err := g.ExportWiki(output, commLabels, comms, centrality); err != nil {
			return err
		}
		fmt.Printf("Wiki exported to %s/\n", output)
		return nil
	},
}

var graphMergeCmd = &cobra.Command{
	Use:   "merge <project-id-a> <project-id-b>",
	Short: "Merge two project graphs (union-merge by node ID)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		ga, err := graph.LoadFullGraph(database, args[0])
		if err != nil {
			return fmt.Errorf("failed to load graph A: %w", err)
		}

		gb, err := graph.LoadFullGraph(database, args[1])
		if err != nil {
			return fmt.Errorf("failed to load graph B: %w", err)
		}

		merged := ga.Merge(gb)
		jsonStr := merged.MergeToJSON(gb)

		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = fmt.Sprintf("merged-%s-%s.json", args[0], args[1])
		}

		if err := os.WriteFile(output, []byte(jsonStr), 0644); err != nil {
			return err
		}
		fmt.Printf("Merged graph: %d nodes, %d edges -> %s\n",
			len(merged.Nodes), len(merged.EdgesBySource), output)
		return nil
	},
}

var graphVizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Export an interactive HTML visualization of the dependency graph",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		g, err := graph.LoadFullGraph(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = "graph.html"
		}

		f, err := os.Create(output)
		if err != nil {
			return err
		}
		defer f.Close()

		comms := g.LeidenDetectCommunities()
		centrality := g.BetweennessCentrality()
		commLabels := g.DetectCommunityLabels(comms, centrality)

		if err := g.ExportHTML(f, comms, commLabels); err != nil {
			return err
		}
		fmt.Printf("Graph visualization exported to %s\n", output)

		open, _ := cmd.Flags().GetBool("open")
		if open {
			var openCmd string
			switch runtime.GOOS {
			case "darwin":
				openCmd = "open"
			case "linux":
				openCmd = "xdg-open"
			case "windows":
				openCmd = "start"
			default:
				return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
			}
			absPath, _ := filepath.Abs(output)
			execCmd := exec.Command(openCmd, absPath)
			if err := execCmd.Start(); err != nil {
				return fmt.Errorf("failed to open browser: %w", err)
			}
		}
		return nil
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Manually synchronize memories between SQLite database and Git JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		// Pull: load Git memories into SQLite database
		fmt.Println("Pulling shared memories from Git...")
		err = memory.SyncFromGit(database, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			return fmt.Errorf("failed to sync from Git: %w", err)
		}

		// Push: write all SQLite memories back to .sv-memory/memories.json
		fmt.Println("Pushing/Exporting local memories back to Git...")
		err = memory.SyncToGit(database, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			return fmt.Errorf("failed to sync to Git: %w", err)
		}

		fmt.Println("Synchronization completed successfully.")
		return nil
	},
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure local editor and CLI environments (Cursor, VS Code, Zed, Windsurf, Claude Code, OpenCode, Codex, Antigravity) with sv-memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ShowBanner()

		// 1. Phase 1: Editors
		predefinedEditors := config.GetPredefinedEditors()
		editorOptions := make([]string, len(predefinedEditors))
		editorMap := make(map[string]config.TargetTool)
		for i, editor := range predefinedEditors {
			mode := "Instrucciones manuales"
			if editor.Auto {
				mode = "Autoconfiguración"
			}
			label := fmt.Sprintf("%s (%s)", editor.Name, mode)
			editorOptions[i] = label
			editorMap[label] = editor
		}

		var selectedEditorLabels []string
		editorPrompt := &survey.MultiSelect{
			Message: "Selecciona los editores de código que deseas configurar:",
			Options: editorOptions,
		}
		
		fmt.Println("=== CONFIGURACIÓN DE EDITORES DE CÓDIGO ===")
		err := survey.AskOne(editorPrompt, &selectedEditorLabels)
		if err != nil {
			return err
		}

		var selectedTools []config.TargetTool
		for _, label := range selectedEditorLabels {
			selectedTools = append(selectedTools, editorMap[label])
		}

		fmt.Println()

		// 2. Phase 2: CLIs
		predefinedCLIs := config.GetPredefinedCLIs()
		cliOptions := make([]string, len(predefinedCLIs))
		cliMap := make(map[string]config.TargetTool)
		for i, cli := range predefinedCLIs {
			mode := "Instrucciones manuales"
			if cli.Auto {
				mode = "Autoconfiguración"
			}
			label := fmt.Sprintf("%s (%s)", cli.Name, mode)
			cliOptions[i] = label
			cliMap[label] = cli
		}

		var selectedCLILabels []string
		cliPrompt := &survey.MultiSelect{
			Message: "Selecciona las herramientas CLI que deseas configurar:",
			Options: cliOptions,
		}

		fmt.Println("=== CONFIGURACIÓN DE CLIs DE TERMINAL ===")
		err = survey.AskOne(cliPrompt, &selectedCLILabels)
		if err != nil {
			return err
		}

		for _, label := range selectedCLILabels {
			selectedTools = append(selectedTools, cliMap[label])
		}

		// 3. Phase 3: Summary and Confirmation
		if len(selectedTools) == 0 {
			fmt.Println("\nNo seleccionaste ninguna herramienta. Operación cancelada.")
			return nil
		}

		fmt.Println("\n=== RESUMEN DE SELECCIÓN ===")
		fmt.Println("Se configurarán las siguientes herramientas:")
		for _, tool := range selectedTools {
			mode := "Configuración manual"
			if tool.Auto {
				mode = "Configuración automática"
			}
			fmt.Printf(" * %s (%s)\n", tool.Name, mode)
		}
		fmt.Println()

		confirm := false
		confirmPrompt := &survey.Confirm{
			Message: "¿Deseas aplicar estos cambios y ver las instrucciones?",
			Default: true,
		}
		
		err = survey.AskOne(confirmPrompt, &confirm)
		if err != nil {
			return err
		}

		if !confirm {
			fmt.Println("Operación cancelada por el usuario.")
			return nil
		}

		fmt.Println("\n=== APLICANDO CONFIGURACIONES ===")
		var successAutoCount int
		var manuals []config.TargetTool

		for _, tool := range selectedTools {
			isAuto, msg, err := config.ConfigureTargetTool(tool)
			if err != nil {
				fmt.Printf("❌ Error al configurar %s: %v\n", tool.Name, err)
				continue
			}

			if isAuto {
				fmt.Printf("✅ %s: %s\n", tool.Name, msg)
				successAutoCount++
			} else {
				tool.ConfigPath = msg // Save manual instructions in ConfigPath for printing later
				manuals = append(manuals, tool)
			}
		}

		if len(manuals) > 0 {
			fmt.Println("\n=== GUÍA DE CONFIGURACIÓN MANUAL ===")
			fmt.Println("Las siguientes herramientas requieren que realices pasos manuales en tu interfaz:")
			fmt.Println()
			for _, m := range manuals {
				fmt.Printf("👉 %s:\n      %s\n\n", m.Name, m.ConfigPath)
			}
		}

		if successAutoCount > 0 {
			fmt.Println("Recuerda reiniciar las herramientas configuradas automáticamente para aplicar los cambios.")
		}

		// Always print the generic copy-paste command
		execPath, err := os.Executable()
		if err == nil {
			execPath = filepath.Clean(execPath)
			fmt.Printf("\n💡 Comando MCP global de sv-memory para configuración manual en cualquier otro editor:\n   %s mcp\n\n", execPath)
		}

		return nil
	},
}

var conflictsCmd = &cobra.Command{
	Use:   "conflicts",
	Short: "Manage memory conflicts and comparisons",
}

var conflictsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List surfaced conflicts for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		projID, _ := cmd.Flags().GetString("project")
		if projID == "" {
			projID = cfg.ProjectID
		}

		status, _ := cmd.Flags().GetString("status")

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		list, err := memory.ListConflicts(database, projID, status)
		if err != nil {
			return err
		}

		if len(list) == 0 {
			fmt.Println("No conflicts found matching criteria.")
			return nil
		}

		fmt.Printf("%-8s %-10s %-5s  %-40s vs %-40s\n", "ID", "Status", "Score", "Memory A", "Memory B")
		fmt.Println(strings.Repeat("-", 110))
		for _, c := range list {
			fmt.Printf("%-8s %-10s %-5.2f  %-40.40s vs %-40.40s\n",
				c.ID, c.Status, c.Score, c.SourceWhat, c.TargetWhat)
		}
		return nil
	},
}

var conflictsStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show statistics of surfaced conflicts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		stats, err := memory.ConflictStats(database, cfg.ProjectID)
		if err != nil {
			return err
		}

		fmt.Printf("=== Conflict Statistics for %s ===\n\n", cfg.ProjName)
		fmt.Printf("Pending:   %d\n", stats["pending"])
		fmt.Printf("Judged:    %d\n", stats["judged"])
		fmt.Printf("Ignored:   %d\n", stats["ignored"])
		fmt.Printf("Total:     %d\n", stats["pending"]+stats["judged"]+stats["ignored"])
		return nil
	},
}

var conflictsScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan project memories for potential conflicts using description similarity",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		apply, _ := cmd.Flags().GetBool("apply")
		maxInsert, _ := cmd.Flags().GetInt("max-insert")
		threshold, _ := cmd.Flags().GetFloat64("threshold")
		if !cmd.Flags().Changed("threshold") {
			threshold = viper.GetFloat64("conflict_threshold")
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if dryRun {
			apply = false
		}

		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		fmt.Printf("Scanning memories in %s for conflicts (threshold: %.2f)...\n", cfg.ProjName, threshold)
		found, err := memory.ScanConflicts(database, cfg.ProjectID, apply, maxInsert, threshold)
		if err != nil {
			return err
		}

		if len(found) == 0 {
			fmt.Println("No new potential conflicts detected.")
			return nil
		}

		fmt.Printf("Found %d potential conflict(s):\n\n", len(found))
		fmt.Printf("%-8s %-5s  %-40s vs %-40s\n", "ID", "Score", "Memory A", "Memory B")
		fmt.Println(strings.Repeat("-", 100))
		for _, c := range found {
			fmt.Printf("%-8s %-5.2f  %-40.40s vs %-40.40s\n",
				c.ID, c.Score, c.SourceWhat, c.TargetWhat)
		}

		if apply {
			fmt.Printf("\nSuccessfully saved %d conflict relation(s) to database (status: pending).\n", len(found))
		} else {
			fmt.Println("\nRun with '--apply' to persist these potential conflicts to database.")
		}
		return nil
	},
}

var conflictsIgnoreCmd = &cobra.Command{
	Use:   "ignore <relation-id>",
	Short: "Ignore a potential conflict by relation ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}
		database, err := db.InitDB(cfg.DBPath)
		if err != nil {
			return err
		}
		defer database.Close()

		err = memory.IgnoreConflict(database, cfg.ProjectID, args[0])
		if err != nil {
			return err
		}

		fmt.Printf("Conflict relation %s marked as ignored.\n", args[0])
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration parameter value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		_, _ = config.LoadConfig(cwd)

		key := args[0]
		if !viper.IsSet(key) {
			return fmt.Errorf("configuration key %q is not set", key)
		}
		fmt.Printf("%s: %v\n", key, viper.Get(key))
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration parameter value globally (or --local)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return err
		}

		key := args[0]
		valStr := args[1]
		local, _ := cmd.Flags().GetBool("local")

		var val interface{}
		switch key {
		case "git_sync_enabled":
			val = valStr == "true"
		case "conflict_threshold":
			var f float64
			if _, err := fmt.Sscanf(valStr, "%f", &f); err != nil {
				return fmt.Errorf("value %q must be a float", valStr)
			}
			val = f
		case "default_review_limit":
			var i int
			if _, err := fmt.Sscanf(valStr, "%d", &i); err != nil {
				return fmt.Errorf("value %q must be an integer", valStr)
			}
			val = i
		default:
			val = valStr
		}

		err = config.WriteConfigKey(cfg.ProjPath, key, val, local)
		if err != nil {
			return err
		}

		scope := "globally"
		if local {
			scope = "locally"
		}
		fmt.Printf("Successfully set %s %s to %v\n", key, scope, val)
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active configuration parameter values",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		_, _ = config.LoadConfig(cwd)

		fmt.Println("=== Active Configuration ===")
		keys := []string{"default_db_path", "git_sync_enabled", "conflict_threshold", "default_review_limit"}
		for _, key := range keys {
			fmt.Printf("  %-22s: %v\n", key, viper.Get(key))
		}
		return nil
	},
}

func init() {
	graphCmd.AddCommand(rebuildCmd)
	graphCmd.AddCommand(graphPathCmd)
	graphCmd.AddCommand(graphExplainCmd)
	graphCmd.AddCommand(graphCommunitiesCmd)
	graphCmd.AddCommand(graphWikiCmd)
	graphWikiCmd.Flags().StringP("output", "o", "graph-wiki", "Output directory for wiki pages")
	graphCmd.AddCommand(graphMergeCmd)
	graphMergeCmd.Flags().StringP("output", "o", "", "Output JSON file path")
	graphCmd.AddCommand(graphVizCmd)
	graphVizCmd.Flags().StringP("output", "o", "graph.html", "Output HTML file path")
	graphVizCmd.Flags().Bool("open", true, "Open the visualization in the default browser automatically")
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(diagnoseCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(graphCmd)

	deleteCmd.AddCommand(deleteSessionCmd)
	deleteCmd.AddCommand(deleteProjectCmd)
	deleteProjectCmd.Flags().Bool("hard", false, "Permanently remove all associated data instead of soft-deleting")

	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsPruneCmd)
	projectsCmd.AddCommand(projectsConsolidateCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(obsidianExportCmd)
	obsidianExportCmd.Flags().StringP("output", "o", ".obsidian-sv-memory", "Output directory for the Obsidian vault")

	conflictsCmd.AddCommand(conflictsListCmd)
	conflictsCmd.AddCommand(conflictsStatsCmd)
	conflictsCmd.AddCommand(conflictsScanCmd)
	conflictsCmd.AddCommand(conflictsIgnoreCmd)
	rootCmd.AddCommand(conflictsCmd)

	conflictsListCmd.Flags().String("status", "", "Filter by status: pending, judged, or ignored")
	conflictsListCmd.Flags().String("project", "", "Filter by project ID")
	conflictsScanCmd.Flags().Bool("apply", false, "Save scanned potential conflicts to database")
	conflictsScanCmd.Flags().Bool("dry-run", false, "Do not save scanned potential conflicts to database (default)")
	conflictsScanCmd.Flags().Int("max-insert", 100, "Maximum number of conflicts to save")
	conflictsScanCmd.Flags().Float64("threshold", 0.45, "Jaccard similarity threshold for descriptions")

	configureCmd.AddCommand(configGetCmd)
	configureCmd.AddCommand(configSetCmd)
	configureCmd.AddCommand(configListCmd)
	configSetCmd.Flags().Bool("local", false, "Write to project-local configuration (.sv-memory/config.yaml)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
