package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
	"github.com/svtech-code/sv-memory/internal/security"
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Manage the project dependency graph",
}

var graphPathCmd = &cobra.Command{
	Use:   "path <source> <target>",
	Short: "Find the shortest path between two nodes in the dependency graph",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
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
		})
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

		cID := graph.NodeCommunityID(node)
		bc := graph.NodeBetweennessCentrality(node)
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
		isGod := fanIn > 10 || fanOut > 10 || bc > 50.0
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
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			fmt.Println("Rebuilding project dependency graph...")
			if err := graph.SyncGraphFull(database, cfg.ProjectID, cfg.ProjPath); err != nil {
				return err
			}
			// Re-link memory <-> code rationale_for edges, wiped by the rebuild.
			if refs, rErr := memory.ActiveMemoryRationaleRefs(database, cfg.ProjectID); rErr == nil && len(refs) > 0 {
				if rErr := graph.RelinkMemoryRationaleEdges(database, cfg.ProjectID, refs); rErr != nil {
					return rErr
				}
			}
			fmt.Println("Dependency graph rebuild complete.")
			return nil
		})
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
		output, err = security.ValidateWritePath(cfg.ProjPath, output)
		if err != nil {
			return fmt.Errorf("invalid output path: %w", err)
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
		jsonStr := merged.SerializeJSON()

		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = fmt.Sprintf("merged-%s-%s.json", args[0], args[1])
		}
		output, err = security.ValidateWritePath(cfg.ProjPath, output)
		if err != nil {
			return fmt.Errorf("invalid output path: %w", err)
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
		output, err = security.ValidateWritePath(cfg.ProjPath, output)
		if err != nil {
			return fmt.Errorf("invalid output path: %w", err)
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
