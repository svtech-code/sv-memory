package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/hook"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage PreToolUse hooks / skills for AI assistants (Claude Code, Codex, Antigravity CLI, OpenCode)",
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install PreToolUse hooks or skills for sv-memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		strict, _ := cmd.Flags().GetBool("strict")
		mode := hook.ModeSoft
		if strict {
			mode = hook.ModeStrict
		}

		platformFilter, _ := cmd.Flags().GetString("platform")
		var platforms []hook.Platform
		if platformFilter != "" {
			p := hook.Platform(platformFilter)
			platforms = []hook.Platform{p}
		}

		eng := hook.New(cwd, mode)
		results := eng.Install(platforms)

		success := 0
		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("❌ %s: %v\n", r.Platform, r.Err)
				continue
			}
			success++
			fmt.Printf("✅ %s:\n", r.Platform)
			for _, f := range r.Files {
				fmt.Printf("   Created/Updated: %s\n", f)
			}
		}

		if success > 0 {
			modeLabel := "soft (nudge)"
			if strict {
				modeLabel = "strict (blocks first raw read)"
			}
			fmt.Printf("\nHooks/skills installed successfully (%s mode).\n", modeLabel)
			fmt.Println("Restart your AI assistant to activate.")
		}
		return nil
	},
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove PreToolUse hooks or skills for sv-memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		platformFilter, _ := cmd.Flags().GetString("platform")
		var platforms []hook.Platform
		if platformFilter != "" {
			p := hook.Platform(platformFilter)
			platforms = []hook.Platform{p}
		}

		eng := hook.New(cwd, hook.ModeSoft)
		results := eng.Uninstall(platforms)

		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("❌ %s: %v\n", r.Platform, r.Err)
				continue
			}
			fmt.Printf("✅ %s: removed %d file(s)\n", r.Platform, len(r.Files))
			for _, f := range r.Files {
				fmt.Printf("   Removed: %s\n", f)
			}
		}
		return nil
	},
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show hook installation status for each platform",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		eng := hook.New(cwd, hook.ModeSoft)
		status := eng.Status(nil)

		fmt.Println("=== Hook Installation Status ===")
		fmt.Println()
		for _, p := range hook.SupportedPlatforms() {
			if status[p] {
				fmt.Printf("  ✅ %s: installed\n", p)
			} else {
				fmt.Printf("  ❌ %s: not installed\n", p)
			}
		}
		return nil
	},
}
