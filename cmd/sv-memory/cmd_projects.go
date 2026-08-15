package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete sessions or projects with cascade options",
}

var deleteSessionCmd = &cobra.Command{
	Use:   "session <session-id>",
	Short: "Delete a session (must have no associated memories)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if err := memory.DeleteSession(database, args[0]); err != nil {
				return err
			}
			fmt.Printf("Session %s deleted successfully.\n", args[0])
			return nil
		})
	},
}

var deleteProjectCmd = &cobra.Command{
	Use:   "project <project-id>",
	Short: "Cascade-delete a project (soft-deletes memories by default; --hard removes permanently)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hard, _ := cmd.Flags().GetBool("hard")
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if err := memory.DeleteProject(database, args[0], hard); err != nil {
				return err
			}
			mode := "soft-deleted"
			if hard {
				mode = "permanently removed"
			}
			fmt.Printf("Project %s %s (cascade complete).\n", args[0], mode)
			return nil
		})
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
		if args[0] == args[1] {
			return fmt.Errorf("source and target project must be different (got %q for both)", args[0])
		}

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
