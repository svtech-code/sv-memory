package main

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/memory"
)

var (
	contextMaxMemories int
	contextWhyChars    int
	contextIncludeChg  bool
)

var contextCmd = &cobra.Command{
	Use:   "context <path>",
	Short: "Show a compact context pack (graph role + linked memories) for a code path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			// Defaults come from the typed Settings snapshot (same source the MCP
			// sv_mem_context tool reads) unless the caller overrode the flag, so
			// CLI and MCP stay in parity when config values are customized.
			maxMemories := contextMaxMemories
			if !cmd.Flags().Changed("max-memories") {
				maxMemories = cfg.Settings.ContextPackMaxMemories
			}
			whyChars := contextWhyChars
			if !cmd.Flags().Changed("why-chars") {
				whyChars = cfg.Settings.BundleWhyChars
			}
			pack, err := memory.GetContextPack(database, cfg.ProjectID, args[0], maxMemories, contextIncludeChg)
			if err != nil {
				return err
			}
			fmt.Println(memory.RenderContextPack(pack, whyChars))
			return nil
		})
	},
}

func init() {
	contextCmd.Flags().IntVar(&contextMaxMemories, "max-memories", config.DefaultContextPackMaxMemories, "Maximum number of linked memories to include")
	contextCmd.Flags().IntVar(&contextWhyChars, "why-chars", config.DefaultBundleWhyChars, "Maximum characters of each memory's why to render")
	contextCmd.Flags().BoolVar(&contextIncludeChg, "include-changes", false, "Include active spec changes affecting the path")
}
