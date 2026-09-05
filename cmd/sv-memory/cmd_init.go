package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/hook"
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

		// 6. Install Git post-commit hook if git repository exists
		gitDir := filepath.Join(cfg.ProjPath, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			eng := hook.New(cfg.ProjPath, hook.ModeSoft)
			results := eng.Install([]hook.Platform{hook.PlatformGit})
			for _, r := range results {
				if r.Err != nil {
					fmt.Printf("⚠️  Warning: failed to install git post-commit hook: %v\n", r.Err)
				} else {
					fmt.Println("✅ Git post-commit hook installed (.git/hooks/post-commit).")
				}
			}
		} else {
			fmt.Println("ℹ️  Git repository not detected (.git not found). Git post-commit hook skipped.")
		}

		// 7. Auto-wire / reconcile AI coding assistants (skills, hooks, MCP permissions)
		skipSetup, _ := cmd.Flags().GetBool("skip-setup")
		if !skipSetup {
			soft, _ := cmd.Flags().GetBool("soft")
			agentFlag, _ := cmd.Flags().GetString("agent")
			agentsFlag, _ := cmd.Flags().GetString("agents")
			allFlag, _ := cmd.Flags().GetBool("all")

			mode := hook.ModeStrict
			if soft {
				mode = hook.ModeSoft
			}

			var targetAgents []string
			if agentFlag != "" {
				targetAgents = []string{agentFlag}
			} else if agentsFlag != "" {
				for _, a := range strings.Split(agentsFlag, ",") {
					a = strings.TrimSpace(a)
					if a != "" {
						targetAgents = append(targetAgents, a)
					}
				}
			} else if allFlag {
				targetAgents = setupAgents
			} else {
				// Interactive prompt if terminal, else reconcile existing installed agents
				isInteractive := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
				installed := installedAgents(cfg.ProjPath)

				if isInteractive {
					selected, err := promptSelectAgents(installed)
					if err != nil {
						if errors.Is(err, huh.ErrUserAborted) {
							fmt.Println("\nOperación de asistentes cancelada por el usuario.")
						} else {
							fmt.Printf("Warning: interactive agent prompt failed: %v\n", err)
						}
					} else {
						targetAgents = selected
						if len(targetAgents) == 0 {
							fmt.Println("\nℹ️  No seleccionaste ningún asistente IA. Puedes configurarlos luego con 'sv-memory setup <agent>'.")
						}
					}
				} else {
					// Non-interactive
					if len(installed) > 0 {
						targetAgents = installed
						fmt.Printf("Reconciliando %d asistente(s) IA configurado(s): %v\n", len(targetAgents), targetAgents)
					} else {
						fmt.Println("ℹ️  Ningún asistente IA configurado (modo no interactivo). Usa 'sv-memory setup <agent>' o pasa '--agent <name>' / '--all'.")
					}
				}
			}

			if len(targetAgents) > 0 {
				fmt.Println("Configurando y reconciliando integraciones de asistentes IA...")
				if err := configureTargetAgentsMode(cfg.ProjPath, mode, targetAgents); err != nil {
					fmt.Printf("Warning: failed to configure assistant integrations: %v\n", err)
				}
			}
		}

		fmt.Println("sv-memory successfully initialized! You can now start the MCP server using 'sv-memory mcp'.")
		return nil
	},
}

func promptSelectAgents(preselected []string) ([]string, error) {
	var selected []string
	if len(preselected) > 0 {
		selected = append(selected, preselected...)
	}

	options := []huh.Option[string]{
		huh.NewOption("Claude Code (Hooks + MCP + Permissions)", "claude-code"),
		huh.NewOption("Antigravity CLI / agy (Hooks + Skill + MCP + Permissions)", "antigravity"),
		huh.NewOption("Cursor (.cursor/mcp.json)", "cursor"),
		huh.NewOption("Windsurf (.windsurf/mcp_config.json)", "windsurf"),
		huh.NewOption("OpenCode (Skill + TS Plugin + MCP)", "opencode"),
		huh.NewOption("Codex (.codex/hooks.json + MCP)", "codex"),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("\nConfiguración de Asistentes IA").
				Description("\nSelecciona los asistentes IA para configurar en este proyecto (Enter para confirmar):").
				Options(options...).
				Value(&selected),
		).Title("ASISTENTES IA"),
	).WithTheme(configureTheme()).WithKeyMap(configureKeyMap())

	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

func init() {
		initCmd.Flags().Bool("strict", false, "accepted for backward compatibility (strict is now the default)")
		initCmd.Flags().Bool("soft", false, "Install soft hooks (nudge-only, no graph-first redirect)")
		initCmd.Flags().String("agent", "", "Explicitly target a single agent during init (e.g. claude-code, antigravity)")
	initCmd.Flags().String("agents", "", "Comma-separated list of agents to target during init (e.g. claude-code,antigravity)")
	initCmd.Flags().Bool("all", false, "Configure all supported AI assistant integrations during init")
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
