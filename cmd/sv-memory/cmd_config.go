package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/config"
)

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
			if _, scanErr := fmt.Sscanf(valStr, "%f", &f); scanErr != nil {
				return fmt.Errorf("value %q must be a float", valStr)
			}
			val = f
		case "default_review_limit":
			var i int
			if _, scanErr := fmt.Sscanf(valStr, "%d", &i); scanErr != nil {
				return fmt.Errorf("value %q must be an integer", valStr)
			}
			val = i
		case "prune_stale_days":
			var i int
			if _, scanErr := fmt.Sscanf(valStr, "%d", &i); scanErr != nil {
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

		if key == "git_sync_enabled" {
			enabled, _ := val.(bool)
			if config.HasGitOrGitignore(cfg.ProjPath) {
				if changed, gitErr := config.EnsureGitignoreStore(cfg.ProjPath, !enabled); gitErr != nil {
					fmt.Printf("⚠️  Warning: failed to update .gitignore: %v\n", gitErr)
				} else if changed {
					if enabled {
						fmt.Println("🤝 Updated .gitignore: removed '.sv-memory/' blanket ignore so memories and specs can be tracked in Git.")
					} else {
						fmt.Println("🔒 Updated .gitignore: added '.sv-memory/' so memories and specs stay local.")
					}
				}
			}

			if enabled {
				if config.RepoVisibility(cfg.ProjPath) == "public" {
					fmt.Println("⚠️  WARNING: This repository appears to be PUBLIC on GitHub.")
					fmt.Println("   Committing .sv-memory/ will make internal decisions, journals, and specs publicly visible.")
				}
				fmt.Println("Next steps (Private Team Git Sync):")
				fmt.Println("  1. Export current specs if needed: sv-memory specs export")
				fmt.Println("  2. Stage and commit shared store:  git add .sv-memory/ .gitignore && git commit -m \"chore(memory): enable team git sync\"")
			} else {
				fmt.Println("Next steps (Local-only / Public Repository Mode):")
				fmt.Println("  - If .sv-memory/ was previously committed to Git, untrack it (keeping local files):")
				fmt.Println("    git rm -r --cached .sv-memory && git add .gitignore && git commit -m \"chore(memory): keep .sv-memory local-only\"")
			}
		}
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
		keys := []string{
			"default_db_path", "git_sync_enabled", "conflict_threshold",
			"default_review_limit", "auto_compaction_enabled",
			"compaction_interval_minutes", "max_response_tokens",
			"max_field_chars", "search_expand_chars", "timeline_why_chars", "bundle_why_chars",
			"context_pack_max_memories", "graph_boost", "prune_stale_days",
		}
		for _, key := range keys {
			fmt.Printf("  %-26s: %v\n", key, viper.Get(key))
		}
		return nil
	},
}
