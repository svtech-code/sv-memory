package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// sessionCmd groups the coding-session lifecycle commands. These are the CLI
// counterpart of the sv_mem_session_* MCP tools, callable from agent hooks and
// plugins (opencode, Claude Code, etc.) without an MCP round-trip, so the
// session lifecycle does not depend on the model choosing to call a tool.
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage coding sessions (start, summary, end, active)",
}

var sessionStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new coding session and print the Auto-Boot context bundle",
	RunE: func(cmd *cobra.Command, args []string) error {
		goal, _ := cmd.Flags().GetString("goal")
		semantic, _ := cmd.Flags().GetBool("semantic")
		agent, _ := cmd.Flags().GetString("semantic-agent")
		dir, _ := cmd.Flags().GetString("directory")
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if dir == "" {
				dir = cfg.ProjPath
			}
			session, err := memory.StartSession(database, cfg.ProjectID, goal, dir)
			if err != nil {
				return fmt.Errorf("failed to start session: %w", err)
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "Session started (ID: %s). Use sv_mem_save with session_id=%q to associate memories, then sv_mem_session_end to close.\n\n", session.ID, session.ID)

			autoBundle, bundleErr := memory.GetAutoBootBundle(context.Background(), database, cfg.ProjectID, memory.AutoBootOptions{
				Goal:     goal,
				Semantic: semantic,
				Agent:    agent,
			})
			if bundleErr == nil && autoBundle != "" {
				sb.WriteString(autoBundle)
			}

			if hubs, hErr := graph.TopDegreeNodes(database, cfg.ProjectID, 3); hErr == nil && len(hubs) > 0 {
				sb.WriteString("\n\n### 🕸️ Graph Hubs (top connected code nodes):\n")
				for _, h := range hubs {
					fmt.Fprintf(&sb, "- **%s** (`%s`, degree: %d)\n", h.Label, h.ID, h.Degree)
				}
				sb.WriteString("\n*Use `sv_graph_explain` on a hub before refactoring it.*\n")
			}

			fmt.Println(strings.TrimSpace(sb.String()))
			return nil
		})
	},
}

var sessionSummaryCmd = &cobra.Command{
	Use:   "summary <session-id>",
	Short: "Save a structured summary for a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		goal, _ := cmd.Flags().GetString("goal")
		discoveries, _ := cmd.Flags().GetString("discoveries")
		accomplished, _ := cmd.Flags().GetString("accomplished")
		nextSteps, _ := cmd.Flags().GetString("next-steps")
		files, _ := cmd.Flags().GetString("files")
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if err := memory.SaveSessionSummary(database, args[0], goal, discoveries, accomplished, nextSteps, files); err != nil {
				return fmt.Errorf("failed to save session summary: %w", err)
			}
			fmt.Printf("Session summary saved for session %s.\n", args[0])
			return nil
		})
	},
}

var sessionEndCmd = &cobra.Command{
	Use:   "end",
	Short: "End the active session (or an explicit one) with an optional summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session-id")
		goal, _ := cmd.Flags().GetString("goal")
		discoveries, _ := cmd.Flags().GetString("discoveries")
		accomplished, _ := cmd.Flags().GetString("accomplished")
		nextSteps, _ := cmd.Flags().GetString("next-steps")
		files, _ := cmd.Flags().GetString("files")
		summary, _ := cmd.Flags().GetString("summary")

		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if sessionID == "" {
				active, actErr := memory.GetActiveSession(database, cfg.ProjectID)
				if actErr != nil || active == nil {
					return errors.New("no active session found to end; pass --session-id explicitly")
				}
				sessionID = active.ID
			}

			if goal != "" || discoveries != "" || accomplished != "" || nextSteps != "" || files != "" {
				if err := memory.SaveSessionSummary(database, sessionID, goal, discoveries, accomplished, nextSteps, files); err != nil {
					return fmt.Errorf("failed to save session summary: %w", err)
				}
				if summary == "" {
					if accomplished != "" {
						summary = accomplished
					} else {
						summary = fmt.Sprintf("Goal: %s\nAccomplished: %s\nDiscoveries: %s\nNext Steps: %s\nFiles: %s",
							goal, accomplished, discoveries, nextSteps, files)
					}
				}
			}

			if err := memory.EndSession(database, sessionID, summary); err != nil {
				if errors.Is(err, memory.ErrSessionAlreadyCompleted) {
					fmt.Printf("Session %s is already completed; summary preserved.\n", sessionID)
					return nil
				}
				return fmt.Errorf("failed to end session: %w", err)
			}

			// Opportunistic auto-compaction, matching the MCP handler.
			_, _, _ = memory.MaybeAutoCompact(database, cfg.ProjectID, memory.DefaultCompactionThreshold)

			fmt.Printf("Session %s ended successfully.\n", sessionID)
			return nil
		})
	},
}

var sessionActiveCmd = &cobra.Command{
	Use:   "active",
	Short: "Print the active session ID, or 'none' when no session is open",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			active, err := memory.GetActiveSession(database, cfg.ProjectID)
			if err != nil {
				return fmt.Errorf("failed to get active session: %w", err)
			}
			if active == nil {
				fmt.Println("none")
				return nil
			}
			fmt.Println(active.ID)
			return nil
		})
	},
}

// capturePromptCmd persists the user's prompt as a local observation attached
// to the active (or explicit) session — CLI counterpart of sv_mem_capture_prompt.
var capturePromptCmd = &cobra.Command{
	Use:   "prompt \"<text>\"",
	Short: "Capture the user's prompt as a local observation attached to the active session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content := args[0]
		sessionID, _ := cmd.Flags().GetString("session-id")
		return withProject(func(cfg *config.Config, database *sql.DB) error {
			if sessionID == "" {
				if active, actErr := memory.GetActiveSession(database, cfg.ProjectID); actErr == nil && active != nil {
					sessionID = active.ID
				}
			}
			prompt, err := memory.SavePrompt(database, cfg.ProjectID, sessionID, content)
			if err != nil {
				return fmt.Errorf("failed to capture user prompt: %w", err)
			}
			msg := fmt.Sprintf("User prompt captured (ID: %s).", prompt.ID)
			if sessionID != "" {
				msg += fmt.Sprintf(" Associated with session %s.", sessionID)
			} else {
				msg += " No active session — run `sv-memory session start` to group prompts with a session."
			}
			fmt.Println(msg)
			return nil
		})
	},
}

func init() {
	sessionStartCmd.Flags().String("goal", "", "Optional session goal (ranks the Auto-Boot bundle by relevance)")
	sessionStartCmd.Flags().Bool("semantic", false, "Re-rank the Auto-Boot bundle candidates semantically via the agent CLI (fails open)")
	sessionStartCmd.Flags().String("semantic-agent", "", "Agent CLI for semantic ranking (default $SV_MEMORY_SEMANTIC_AGENT, then 'claude')")
	sessionStartCmd.Flags().String("directory", "", "Working directory (defaults to the current project root)")

	sessionSummaryCmd.Flags().String("goal", "", "Session goal")
	sessionSummaryCmd.Flags().String("discoveries", "", "Key discoveries or findings")
	sessionSummaryCmd.Flags().String("accomplished", "", "What was accomplished")
	sessionSummaryCmd.Flags().String("next-steps", "", "Next steps or pending tasks")
	sessionSummaryCmd.Flags().String("files", "", "Relevant files modified or created")

	sessionEndCmd.Flags().String("session-id", "", "Session ID to end (defaults to the active session)")
	sessionEndCmd.Flags().String("summary", "", "Optional summary of what was accomplished")
	sessionEndCmd.Flags().String("goal", "", "Session goal")
	sessionEndCmd.Flags().String("discoveries", "", "Key discoveries or findings")
	sessionEndCmd.Flags().String("accomplished", "", "What was accomplished")
	sessionEndCmd.Flags().String("next-steps", "", "Next steps or pending tasks")
	sessionEndCmd.Flags().String("files", "", "Relevant files modified or created")

	capturePromptCmd.Flags().String("session-id", "", "Session ID to associate the prompt with (defaults to the active session)")

	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionSummaryCmd)
	sessionCmd.AddCommand(sessionEndCmd)
	sessionCmd.AddCommand(sessionActiveCmd)
}
