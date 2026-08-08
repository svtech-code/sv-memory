package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

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
		return withProject(func(cfg *config.Config, database *sql.DB) error {
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
		})
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
		semantic, _ := cmd.Flags().GetBool("semantic")

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

		if !semantic {
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
		}

		// Semantic mode: LLM-judge candidate pairs using the configured agent CLI.
		agent, _ := cmd.Flags().GetString("agent")
		if agent == "" {
			agent = os.Getenv("SV_MEMORY_SEMANTIC_AGENT")
		}
		if agent == "" {
			agent = "claude"
		}
		maxSemantic, _ := cmd.Flags().GetInt("max-semantic")
		concurrency, _ := cmd.Flags().GetInt("concurrency")

		candidates := found
		if len(candidates) == 0 {
			candidates, _ = memory.ListConflicts(database, cfg.ProjectID, "pending")
		}
		if len(candidates) == 0 {
			fmt.Println("No candidate conflicts to judge semantically. Run 'sv-memory conflicts scan --apply' first to surface candidates.")
			return nil
		}

		fmt.Printf("Judging %d conflict candidate(s) with agent '%s' (max: %d, concurrency: %d)...\n",
			len(candidates), agent, maxSemantic, concurrency)
		verdicts, err := memory.SemanticJudgeCandidates(context.Background(), database, cfg.ProjectID, candidates, agent, maxSemantic, concurrency)
		if err != nil {
			return err
		}

		judged, ignored, failed := 0, 0, 0
		fmt.Println("\nSemantic judgments:")
		for _, v := range verdicts {
			if v.Error != "" {
				failed++
				fmt.Printf("- ❌ %s vs %s: %s\n", v.SourceID, v.TargetID, v.Error)
				continue
			}
			label := strings.ToUpper(v.Relation)
			icon := "🔀"
			if v.Relation == memory.SemanticNone {
				icon = "➖"
				ignored++
			} else {
				judged++
			}
			fmt.Printf("- %s **%s** %s ↔ %s (score %.2f)\n  Reason: %s\n", icon, label, v.SourceID, v.TargetID, v.Score, v.Reason)
			if apply {
				if err := memory.ApplySemanticVerdict(database, cfg.ProjectID, v); err != nil {
					return err
				}
			}
		}

		if failed > 0 {
			fmt.Printf("\n⚠️  %d judgment(s) failed and were left pending for retry.\n", failed)
		}
		if apply {
			fmt.Printf("\nPersisted %d judged and %d ignored relation(s) (judged_by: llm).\n", judged, ignored)
		} else {
			fmt.Println("\nRun with '--apply' to persist these judgments to database.")
		}
		return nil
	},
}

func init() {
	conflictsScanCmd.Flags().Bool("semantic", false, "LLM-judge candidate conflicts using the configured agent CLI (claude/opencode)")
	conflictsScanCmd.Flags().String("agent", "", "Agent CLI to use for semantic judging ('claude', 'opencode', or a custom command; default: $SV_MEMORY_SEMANTIC_AGENT or 'claude')")
	conflictsScanCmd.Flags().Int("max-semantic", 0, "Maximum number of candidate pairs to judge semantically (default: all)")
	conflictsScanCmd.Flags().Int("concurrency", 3, "Number of parallel agent judgments")
}

var conflictsIgnoreCmd = &cobra.Command{
	Use:   "ignore <relation-id>",
	Short: "Ignore a potential conflict by relation ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if err := memory.IgnoreConflict(database, cfg.ProjectID, args[0]); err != nil {
				return err
			}
			fmt.Printf("Conflict relation %s marked as ignored.\n", args[0])
			return nil
		})
	},
}
