package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
	"github.com/svtech-code/sv-memory/internal/protocol"
	"github.com/svtech-code/sv-memory/internal/tui"
)

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

		// 3b. Ensure default .sv-memoryignore template exists
		createdIgnore, err := graph.EnsureMemoryIgnore(cfg.ProjPath)
		if err != nil {
			fmt.Printf("Warning: failed to create .sv-memoryignore: %v\n", err)
		} else if createdIgnore {
			fmt.Println("Created default .sv-memoryignore template.")
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

		// 6. Auto-wire / reconcile AI coding assistants (skills, hooks, MCP permissions)
		strict, _ := cmd.Flags().GetBool("strict")
		agentFlag, _ := cmd.Flags().GetString("agent")
		skipSetup, _ := cmd.Flags().GetBool("skip-setup")
		if !skipSetup {
			fmt.Println("Configuring and reconciling AI assistant integrations...")
			if err := autoWireProjectAgents(cfg.ProjPath, strict, agentFlag); err != nil {
				fmt.Printf("Warning: failed to auto-wire assistant integrations: %v\n", err)
			}
		}

		fmt.Println("sv-memory successfully initialized! You can now start the MCP server using 'sv-memory mcp'.")
		return nil
	},
}

func init() {
	initCmd.Flags().Bool("strict", false, "Install strict hooks during agent setup (block first raw read on Antigravity)")
	initCmd.Flags().String("agent", "", "Explicitly target a single agent during init (defaults to auto-detect/all)")
	initCmd.Flags().Bool("skip-setup", false, "Skip agent hook/skill/mcp setup during initialization")
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

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive terminal user interface for memory and graph exploration",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			return tui.RunTUI(database, cfg.ProjectID, cfg.ProjPath)
		})
	},
}
