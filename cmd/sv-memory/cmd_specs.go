package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/memory"
)

var specsCmd = &cobra.Command{
	Use:   "specs",
	Short: "Manage the spec mirror (.sv-memory/specs/) for spec-driven changes",
}

var specsExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all active spec changes as Markdown mirrors under .sv-memory/specs/",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if err := memory.WriteSpecMirror(database, cfg.ProjectID, cfg.ProjPath); err != nil {
				return fmt.Errorf("failed to export spec mirror: %w", err)
			}
			fmt.Println("Spec mirror exported to .sv-memory/specs/.")
			return nil
		})
	},
}

var specsImportCmd = &cobra.Command{
	Use:   "import <slug>",
	Short: "Reconcile a human-edited spec mirror back into the SQLite store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			updated, err := memory.ImportChangeFromMarkdown(database, cfg.ProjectID, cfg.ProjPath, slug)
			if err != nil {
				return err
			}
			if updated == nil {
				fmt.Printf("No mirror found for slug %q (expected .sv-memory/specs/changes/%s.md).\n", slug, slug)
				return nil
			}
			fmt.Printf("Reconciled change %q from its mirror (status: %s).\n", updated.Slug, updated.Status)
			return nil
		})
	},
}

var specsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active spec changes in the store and their mirror status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			changes, err := memory.ListChangesByStatus(database, cfg.ProjectID, "")
			if err != nil {
				return err
			}
			if len(changes) == 0 {
				fmt.Println("No spec changes found in this project. Create one with sv_propose_spec or 'sv-memory specs new'.")
				return nil
			}
			mirrors, _ := memory.ListSpecMirrors(cfg.ProjPath)
			mirrorSet := map[string]bool{}
			for _, m := range mirrors {
				mirrorSet[m] = true
			}
			fmt.Printf("%-12s %-24s %-10s %s\n", "STATUS", "SLUG", "MIRROR", "TITLE")
			for _, c := range changes {
				mirror := "no"
				if mirrorSet[c.Slug] {
					mirror = "yes"
				}
				fmt.Printf("%-12s %-24s %-10s %s\n", c.Status, c.Slug, mirror, truncateTitle(c.Title, 60))
			}
			return nil
		})
	},
}

func truncateTitle(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max-1])) + "…"
}

var specsArchiveCmd = &cobra.Command{
	Use:   "archive <slug>",
	Short: "Move an applied change to the archived state (mirror moves to specs/archive/)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			c, err := memory.GetChangeBySlug(database, cfg.ProjectID, args[0])
			if err != nil {
				return err
			}
			if c == nil {
				return fmt.Errorf("no change with slug %q exists in the store", args[0])
			}
			if c.Status != memory.ChangeStatusApplied {
				return fmt.Errorf("only applied changes can be archived (current status: %s)", c.Status)
			}
			if _, err := memory.UpdateChangeStatus(database, cfg.ProjectID, c.ID, memory.ChangeStatusArchived); err != nil {
				return err
			}
			if err := memory.WriteSpecMirror(database, cfg.ProjectID, cfg.ProjPath); err != nil {
				return fmt.Errorf("change archived but mirror refresh failed: %w", err)
			}
			fmt.Printf("Change %q archived. Mirror moved to .sv-memory/specs/archive/.\n", args[0])
			return nil
		})
	},
}

func init() {
	specsCmd.AddCommand(specsExportCmd)
	specsCmd.AddCommand(specsImportCmd)
	specsCmd.AddCommand(specsListCmd)
	specsCmd.AddCommand(specsArchiveCmd)
	rootCmd.AddCommand(specsCmd)
}
