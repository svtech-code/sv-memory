package main

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/memory"
	"github.com/svtech-code/sv-memory/internal/security"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Run read-only health checks on the project setup (DB, schema, permissions)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
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
		})
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate memory statistics for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			s, err := memory.GetStats(database, cfg.ProjectID)
			if err != nil {
				return fmt.Errorf("failed to get stats: %w", err)
			}
			fmt.Printf("=== Memory Statistics for %s ===\n\n", cfg.ProjName)
			fmt.Printf("Current project:   %s (ID: %s)\n", cfg.ProjName, cfg.ProjectID)
			fmt.Printf("Total memories:    %d\n", s.TotalMemories)
			fmt.Printf("Deleted memories:  %d\n", s.DeletedMemories)
			fmt.Printf("Recent (24h):      %d\n", s.Recent24h)
			fmt.Printf("Total sessions:    %d\n", s.TotalSessions)
			fmt.Printf("Active sessions:   %d\n", s.ActiveSessions)
			fmt.Printf("Total relations:   %d\n", s.TotalRelations)
			fmt.Printf("Total user prompts:%d\n", s.TotalPrompts)
			if len(s.ByCategory) > 0 {
				fmt.Println("\nBy category:")
				for cat, count := range s.ByCategory {
					fmt.Printf("  %-15s %d\n", cat, count)
				}
			}
			return nil
		})
	},
}

var exportCmd = &cobra.Command{
	Use:   "export [output-file]",
	Short: "Export all non-deleted memories to a portable JSON file",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			outputFile := "sv-memory-export.json"
			if len(args) > 0 {
				outputFile = args[0]
			}

			n, err := memory.ExportJSON(database, cfg.ProjectID, outputFile)
			if err != nil {
				return fmt.Errorf("export failed: %w", err)
			}

			fmt.Printf("Exported %d memories to %s\n", n, outputFile)
			return nil
		})
	},
}

var importCmd = &cobra.Command{
	Use:   "import <input-file>",
	Short: "Import memories from a portable JSON file (upsert by ID)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			n, err := memory.ImportJSON(database, cfg.ProjectID, args[0])
			if err != nil {
				return fmt.Errorf("import failed: %w", err)
			}
			fmt.Printf("Imported %d memories from %s\n", n, args[0])
			return nil
		})
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Manually synchronize memories between SQLite database and Git JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			fmt.Println("Pulling shared memories from Git...")
			if err := memory.SyncFromGit(database, cfg.ProjectID, cfg.ProjPath); err != nil {
				return fmt.Errorf("failed to sync from Git: %w", err)
			}
			fmt.Println("Pushing/Exporting local memories back to Git...")
			if err := memory.SyncToGit(database, cfg.ProjectID, cfg.ProjPath); err != nil {
				return fmt.Errorf("failed to sync to Git: %w", err)
			}
			fmt.Println("Synchronization completed successfully.")
			return nil
		})
	},
}

var obsidianExportCmd = &cobra.Command{
	Use:   "obsidian-export",
	Short: "Export all memories as Markdown files in Obsidian vault format",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			outputDir, _ := cmd.Flags().GetString("output")
			if outputDir == "" {
				outputDir = ".obsidian-sv-memory"
			}
			vaultPath, err := security.ValidateWritePath(cfg.ProjPath, outputDir)
			if err != nil {
				return fmt.Errorf("invalid output path: %w", err)
			}

			fmt.Printf("Exporting memories to Obsidian vault at %s...\n", vaultPath)
			if err := memory.ExportObsidian(database, cfg.ProjectID, vaultPath); err != nil {
				return fmt.Errorf("obsidian export failed: %w", err)
			}
			fmt.Println("Export complete.")
			return nil
		})
	},
}

var captureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Passively capture a git commit or journal note into memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		commitHash, _ := cmd.Flags().GetString("commit")
		commitMsg, _ := cmd.Flags().GetString("message")
		author, _ := cmd.Flags().GetString("author")
		branch, _ := cmd.Flags().GetString("branch")
		paths, _ := cmd.Flags().GetString("paths")
		what, _ := cmd.Flags().GetString("what")
		why, _ := cmd.Flags().GetString("why")

		if what == "" && commitMsg != "" {
			what = fmt.Sprintf("git commit: %s", commitMsg)
		}
		if what == "" {
			return fmt.Errorf("either --what or --message is required")
		}
		if why == "" && commitHash != "" {
			why = fmt.Sprintf("automatic post-commit capture for %s", commitHash)
		}
		if why == "" {
			why = "passive capture"
		}

		return withProject(func(cfg *config.Config, database *sql.DB) error {
			mem := &memory.Memory{
				ProjectID: cfg.ProjectID,
				Category:  "journal",
				What:      what,
				Why:       why,
				Learned:   what,
				WherePath: paths,
				GitBranch: branch,
				GitCommit: commitHash,
				Author:    author,
			}
			saved, err := memory.SaveMemory(database, mem)
			if err != nil {
				return err
			}
			fmt.Printf("Captured memory %s: %s\n", saved.ID, saved.What)
			return nil
		})
	},
}

func init() {
	captureCmd.Flags().String("commit", "", "Git commit hash")
	captureCmd.Flags().String("message", "", "Git commit message")
	captureCmd.Flags().String("author", "", "Commit author")
	captureCmd.Flags().String("branch", "", "Git branch")
	captureCmd.Flags().String("paths", "", "Affected paths/files")
	captureCmd.Flags().String("what", "", "Observation summary (overrides --message)")
	captureCmd.Flags().String("why", "", "Observation rationale")
}
