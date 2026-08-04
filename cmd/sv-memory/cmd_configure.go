package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/perm"
)

// bannerCyan is the brand color used by the SV Tech banner (#00B0C2).
const bannerCyan = "#00B0C2"

// configureTheme returns a huh theme that matches the SV Tech banner color.
// The selection colors (green ✓ from ThemeCharm) are kept untouched so the
// selected-option indicator stays green as before.
func configureTheme() *huh.Theme {
	t := huh.ThemeCharm()

	cyan := lipgloss.Color(bannerCyan)
	lightCyan := lipgloss.Color("#4FB8C4")

	// Structural elements → banner cyan; selection stays green (from ThemeCharm).
	t.Focused.Base = t.Focused.Base.BorderForeground(cyan)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(cyan).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(cyan).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(lightCyan)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(cyan)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(cyan)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(cyan)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(cyan)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("#000000")).Background(cyan)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(cyan).Background(lipgloss.Color("#111111"))
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(cyan)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(cyan)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}

// configureKeyMap binds the Esc key to go back to the previous step. Ctrl+C
// remains the global cancel (huh default) and carries a Spanish help label.
// Note: on the first step there is no previous step, so Esc does nothing there.
func configureKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "salir"),
	)
	km.MultiSelect.Prev = key.NewBinding(
		key.WithKeys("shift+tab", "esc"),
		key.WithHelp("esc", "retroceder"),
	)
	km.Confirm.Prev = key.NewBinding(
		key.WithKeys("shift+tab", "esc"),
		key.WithHelp("esc", "retroceder"),
	)
	return km
}

// toolLabel builds a display label for a TargetTool, e.g.
// "Cursor (Autoconfiguración)" or "VS Code (Instrucciones manuales)".
func toolLabel(t config.TargetTool) string {
	mode := "Instrucciones manuales"
	if t.Auto {
		mode = "Autoconfiguración"
	}
	return fmt.Sprintf("%s (%s)", t.Name, mode)
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure local editor and CLI environments (Cursor, VS Code, Zed, Windsurf, Claude Code, OpenCode, Codex, Antigravity) with sv-memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ShowBanner(version)

		// 1. Phase 1: Editors
		predefinedEditors := config.GetPredefinedEditors()
		editorLabels := make([]string, len(predefinedEditors))
		editorMap := make(map[string]config.TargetTool)
		for i, editor := range predefinedEditors {
			label := toolLabel(editor)
			editorLabels[i] = label
			editorMap[label] = editor
		}

		// 2. Phase 2: CLIs
		predefinedCLIs := config.GetPredefinedCLIs()
		cliLabels := make([]string, len(predefinedCLIs))
		cliMap := make(map[string]config.TargetTool)
		for i, cli := range predefinedCLIs {
			label := toolLabel(cli)
			cliLabels[i] = label
			cliMap[label] = cli
		}

		var selectedEditors, selectedCLIs []string
		var confirmed bool

		// 3. Interactive form: Editors → CLIs → Confirmation. The Confirm
		// step's "No, cancelar" exits immediately without any further screen.
		editorOptions := huh.NewOptions(editorLabels...)
		cliOptions := huh.NewOptions(cliLabels...)

		form1 := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("\nPaso 1 de 4 — Editores de código").
					Description("\nSelecciona los editores de código a configurar.").
					Options(editorOptions...).
					Value(&selectedEditors),
			).Title("EDITORES"),

			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("\nPaso 2 de 4 — CLIs de terminal").
					Description("\nSelecciona las CLIs de terminal a configurar.").
					Options(cliOptions...).
					Value(&selectedCLIs),
			).Title("CLIs DE TERMINAL"),

			huh.NewGroup(
				huh.NewConfirm().
					Title("\nPaso 3 de 4 — Confirmación").
					Description("\n¿Aplicar estos cambios y ver las instrucciones?").
					Affirmative("Sí, aplicar").
					Negative("No, cancelar").
					Value(&confirmed),
			).Title("CONFIRMACIÓN"),
		).WithTheme(configureTheme()).WithKeyMap(configureKeyMap())

		if err := form1.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("\nOperación cancelada por el usuario.")
				return nil
			}
			return fmt.Errorf("failed to run configure wizard: %w", err)
		}

		if !confirmed {
			fmt.Println("\nOperación cancelada por el usuario.")
			return nil
		}

		var selectedTools []config.TargetTool
		for _, label := range selectedEditors {
			selectedTools = append(selectedTools, editorMap[label])
		}
		for _, label := range selectedCLIs {
			selectedTools = append(selectedTools, cliMap[label])
		}

		if len(selectedTools) == 0 {
			fmt.Println("\nNo seleccionaste ninguna herramienta. Operación cancelada.")
			return nil
		}

		fmt.Println("\n=== APLICANDO CONFIGURACIONES ===")
		var successAutoCount int
		var manuals []config.TargetTool

		for _, tool := range selectedTools {
			isAuto, msg, err := config.ConfigureTargetTool(tool)
			if err != nil {
				fmt.Printf("❌ Error al configurar %s: %v\n", tool.Name, err)
				continue
			}

			if isAuto {
				fmt.Printf("✅ %s: %s\n", tool.Name, msg)
				successAutoCount++
			} else {
				tool.ConfigPath = msg // Save manual instructions in ConfigPath for printing later
				manuals = append(manuals, tool)
			}
		}

		if len(manuals) > 0 {
			fmt.Println("\n=== GUÍA DE CONFIGURACIÓN MANUAL ===")
			fmt.Println("Las siguientes herramientas requieren que realices pasos manuales en tu interfaz:")
			fmt.Println()
			for _, m := range manuals {
				fmt.Printf("👉 %s:\n      %s\n\n", m.Name, m.ConfigPath)
			}
		}

		// 4. Grant MCP tool permissions to the allow-listed platforms
		// selected above (Antigravity, Claude Code). OpenCode and Codex use
		// interactive approval and are skipped here.
		var grantTargets []config.TargetTool
		for _, tool := range selectedTools {
			switch tool.ID {
			case "antigravity", "claude-code":
				grantTargets = append(grantTargets, tool)
			}
		}

		if len(grantTargets) > 0 {
			toolNames := make([]string, len(mcp.AllTools))
			for i, t := range mcp.AllTools {
				toolNames[i] = t.Name
			}

			var selectedPerms []string
			permOptions := huh.NewOptions(toolNames...)
			form2 := huh.NewForm(
				huh.NewGroup(
					huh.NewMultiSelect[string]().
						Title("\nPaso 4 de 4 — Permisos de herramientas MCP").
						Description("\nAutoriza las herramientas para " + platformNames(grantTargets) + ".").
						Options(permOptions...).
						Height(20).
						Value(&selectedPerms),
				).Title("PERMISOS DE SV-MEMORY"),
			).WithTheme(configureTheme()).WithKeyMap(configureKeyMap())

			if err := form2.Run(); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					fmt.Println("\nNo se otorgarán permisos adicionales.")
					return nil
				}
				return fmt.Errorf("failed to run permissions wizard: %w", err)
			}

			if len(selectedPerms) == 0 {
				fmt.Println("\nNo se otorgarán permisos adicionales.")
			} else {
				fmt.Println("\n=== OTORGANDO PERMISOS ===")
				for _, tool := range grantTargets {
					res, err := perm.Grant(perm.Platform(tool.ID), selectedPerms, false)
					if err != nil {
						fmt.Printf("❌ %s: %v\n", tool.Name, err)
						continue
					}
					if res.Skipped {
						fmt.Printf("ℹ️  %s: %s\n", tool.Name, res.SkippedMsg)
						continue
					}
					if len(res.Added) == 0 {
						fmt.Printf("✅ %s: ya tenía %d permiso(s).\n", tool.Name, len(res.Present))
						continue
					}
					fmt.Printf("✅ %s: %d permiso(s) otorgado(s) en %s\n", tool.Name, len(res.Added), res.ConfigPath)
				}
			}
		}

		if successAutoCount > 0 {
			fmt.Println("Recuerda reiniciar las herramientas configuradas automáticamente para aplicar los cambios.")
		}

		// Always print the generic copy-paste command
		execPath, err := os.Executable()
		if err == nil {
			execPath = filepath.Clean(execPath)
			fmt.Printf("\n💡 Comando MCP global de sv-memory para configuración manual en cualquier otro editor:\n   %s mcp\n\n", execPath)
		}

		return nil
	},
}

// platformNames joins the display names of the grant target tools.
func platformNames(tools []config.TargetTool) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}
