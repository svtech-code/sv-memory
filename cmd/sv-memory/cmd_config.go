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
