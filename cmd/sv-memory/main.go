package main

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
)

// version and commit are injected at build time via -ldflags
// (e.g. -X main.version=v0.1.0 -X main.commit=<short-sha>). They default to
// "dev"/"unknown" for local builds.
var (
	version = "dev"
	commit  = "unknown"
)

func withProject(fn func(cfg *config.Config, database *sql.DB) error) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
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
	return fn(cfg, database)
}

var rootCmd = &cobra.Command{
	Use:   "sv-memory",
	Short: "sv-memory: Context Memory and Structural Code Graph for AI Agents",
	Long:  `sv-memory is a CLI tool and Model Context Protocol (MCP) server that records architectural decisions, coding guidelines, and code graphs to prevent context amnesia in AI agents.`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the sv-memory version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sv-memory %s\n", version)
		if commit != "unknown" && commit != "" {
			fmt.Printf("commit: %s\n", commit)
		}
		fmt.Printf("go: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	graphCmd.AddCommand(rebuildCmd)
	graphCmd.AddCommand(graphPathCmd)
	graphCmd.AddCommand(graphExplainCmd)
	graphCmd.AddCommand(graphCommunitiesCmd)
	graphWikiCmd.Flags().StringP("output", "o", "graph-wiki", "Output directory for wiki pages")
	graphCmd.AddCommand(graphWikiCmd)
	graphCmd.AddCommand(graphMergeCmd)
	graphMergeCmd.Flags().StringP("output", "o", "", "Output JSON file path")
	graphCmd.AddCommand(graphVizCmd)
	graphVizCmd.Flags().StringP("output", "o", "graph.html", "Output HTML file path")
	graphVizCmd.Flags().Bool("open", true, "Open the visualization in the default browser automatically")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(tuiCmd)
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

	hooksInstallCmd.Flags().Bool("strict", false, "Enable strict mode (blocks the first raw source read)")
	hooksInstallCmd.Flags().String("platform", "", "Target platform (claude-code, codex, antigravity, opencode). Default: all")
	hooksUninstallCmd.Flags().String("platform", "", "Target platform (claude-code, codex, antigravity, opencode). Default: all")

	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)

	permissionsCmd.AddCommand(permissionsListCmd)
	permissionsCmd.AddCommand(permissionsStatusCmd)
	permissionsCmd.AddCommand(permissionsGrantCmd)
	permissionsCmd.AddCommand(permissionsRevokeCmd)
	rootCmd.AddCommand(permissionsCmd)
	permissionsGrantCmd.Flags().String("platform", "", "Target platform (antigravity, claude-code, opencode, codex)")
	permissionsGrantCmd.Flags().Bool("all", false, "Grant all sv-memory tools")
	permissionsGrantCmd.Flags().StringSlice("tool", nil, "Comma-separated tool names to grant (see 'permissions list')")
	permissionsGrantCmd.Flags().Bool("dry-run", false, "Show what would be granted without writing")
	permissionsRevokeCmd.Flags().String("platform", "", "Target platform (antigravity, claude-code, opencode, codex)")
	permissionsRevokeCmd.Flags().Bool("dry-run", false, "Show what would be revoked without writing")
	permissionsStatusCmd.Flags().String("platform", "", "Show status for a single platform (default: all)")

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
