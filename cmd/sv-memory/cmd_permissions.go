package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/perm"
)

var permissionsCmd = &cobra.Command{
	Use:   "permissions",
	Short: "Manage sv-memory MCP tool permissions per AI assistant (Antigravity, Claude Code, OpenCode, Codex)",
}

var permissionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sv-memory MCP tools with descriptions",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== sv-memory MCP Tools (26) ===")
		fmt.Println()
		for _, t := range mcp.AllTools {
			fmt.Printf("  %-30s %s\n", t.Name, t.Description)
		}
		fmt.Println()
		fmt.Println("Grant permissions with: sv-memory permissions grant --platform <platform>")
		return nil
	},
}

var permissionsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current permission state for each platform",
	RunE: func(cmd *cobra.Command, args []string) error {
		platformFilter, _ := cmd.Flags().GetString("platform")

		var platforms []perm.Platform
		if platformFilter != "" {
			p, err := parsePlatform(platformFilter)
			if err != nil {
				return err
			}
			platforms = []perm.Platform{p}
		} else {
			platforms = perm.SupportedPlatforms
		}

		for _, p := range platforms {
			st, err := perm.Status(p)
			if err != nil {
				fmt.Printf("❌ %s: %v\n", p, err)
				continue
			}
			fmt.Printf("\n=== %s (%s) ===\n", st.Name, p)
			fmt.Printf("  Config: %s\n", st.ConfigPath)
			if !st.AllowListed {
				fmt.Printf("  Allow-list: N/A (%s)\n", st.Message)
				continue
			}
			if !st.Configured {
				fmt.Printf("  Status: not configured (%s)\n", st.Message)
				continue
			}
			fmt.Printf("  Granted: %d / %d\n", len(st.Granted), len(mcp.AllTools))
			if len(st.Missing) > 0 {
				fmt.Printf("  Missing: %s\n", strings.Join(st.Missing, ", "))
			} else {
				fmt.Println("  All tools granted ✅")
			}
		}
		return nil
	},
}

var permissionsGrantCmd = &cobra.Command{
	Use:   "grant",
	Short: "Grant sv-memory MCP tool permissions to a platform allow-list",
	RunE: func(cmd *cobra.Command, args []string) error {
		platformFlag, _ := cmd.Flags().GetString("platform")
		if platformFlag == "" {
			return fmt.Errorf("--platform is required (one of: %s)", strings.Join(platformIDs(), ", "))
		}
		p, err := parsePlatform(platformFlag)
		if err != nil {
			return err
		}

		var tools []string
		all, _ := cmd.Flags().GetBool("all")
		toolList, _ := cmd.Flags().GetStringSlice("tool")
		if all {
			tools = nil // Grant() grants all when empty
		} else if len(toolList) > 0 {
			tools = toolList
		} else {
			return fmt.Errorf("specify --all or --tool <name>[,<name>...]; list tools with 'sv-memory permissions list'")
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		res, err := perm.Grant(p, tools, dryRun)
		if err != nil {
			return err
		}
		printGrantResult(res)
		return nil
	},
}

var permissionsRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke sv-memory MCP tool permissions from a platform allow-list",
	RunE: func(cmd *cobra.Command, args []string) error {
		platformFlag, _ := cmd.Flags().GetString("platform")
		if platformFlag == "" {
			return fmt.Errorf("--platform is required (one of: %s)", strings.Join(platformIDs(), ", "))
		}
		p, err := parsePlatform(platformFlag)
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		res, err := perm.Revoke(p, dryRun)
		if err != nil {
			return err
		}
		if res.Skipped {
			fmt.Printf("ℹ️  %s\n", res.SkippedMsg)
			return nil
		}
		if len(res.Removed) == 0 {
			fmt.Printf("No sv-memory permissions to revoke for %s.\n", p)
			return nil
		}
		if dryRun {
			fmt.Printf("[dry-run] Would revoke %d permission(s) from %s:\n", len(res.Removed), res.ConfigPath)
		} else {
			fmt.Printf("✅ Revoked %d permission(s) from %s:\n", len(res.Removed), res.ConfigPath)
		}
		for _, tool := range res.Removed {
			fmt.Printf("   - %s\n", tool)
		}
		return nil
	},
}

func printGrantResult(res *perm.Result) {
	if res.Skipped {
		fmt.Printf("ℹ️  %s\n", res.SkippedMsg)
		return
	}
	if dryRun := res.DryRun; dryRun {
		fmt.Printf("[dry-run] Target config: %s\n", res.ConfigPath)
		fmt.Printf("  Would add %d: %s\n", len(res.Added), strings.Join(res.Added, ", "))
		if len(res.Present) > 0 {
			fmt.Printf("  Already granted (%d): %s\n", len(res.Present), strings.Join(res.Present, ", "))
		}
		return
	}
	fmt.Printf("✅ Updated %s\n", res.ConfigPath)
	if len(res.Added) > 0 {
		fmt.Printf("  Added %d: %s\n", len(res.Added), strings.Join(res.Added, ", "))
	}
	if len(res.Present) > 0 {
		fmt.Printf("  Already granted (%d): %s\n", len(res.Present), strings.Join(res.Present, ", "))
	}
	fmt.Println("Restart your AI assistant to load the new permissions.")
}

func platformIDs() []string {
	ids := make([]string, len(perm.SupportedPlatforms))
	for i, p := range perm.SupportedPlatforms {
		ids[i] = string(p)
	}
	return ids
}

func parsePlatform(id string) (perm.Platform, error) {
	for _, p := range perm.SupportedPlatforms {
		if string(p) == id {
			return p, nil
		}
	}
	return "", fmt.Errorf("unsupported platform %q (valid: %s)", id, strings.Join(platformIDs(), ", "))
}
