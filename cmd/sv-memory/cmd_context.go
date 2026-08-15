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
			pack, err := memory.GetContextPack(database, cfg.ProjectID, args[0], contextMaxMemories, contextIncludeChg)
			if err != nil {
				return err
			}
			fmt.Println(memory.RenderContextPack(pack, contextWhyChars))
			return nil
		})
	},
}

func init() {
	contextCmd.Flags().IntVar(&contextMaxMemories, "max-memories", 5, "Maximum number of linked memories to include")
	contextCmd.Flags().IntVar(&contextWhyChars, "why-chars", 300, "Maximum characters of each memory's why to render")
	contextCmd.Flags().BoolVar(&contextIncludeChg, "include-changes", false, "Include active spec changes affecting the path")
}
