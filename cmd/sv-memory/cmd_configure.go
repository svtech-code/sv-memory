package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/perm"
)

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

		// 3. Interactive form: Editors → CLIs → Confirmation.
		// Arrow keys navigate, space toggles, enter advances, esc goes back.
		form1 := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Paso 1 de 4 — Editores de código").
					Description("Selecciona con ESPACIO, navega con ↑/↓, Enter para continuar.").
					Options(huh.NewOptions(editorLabels...)...).
					Value(&selectedEditors),
			).Title("EDITORES"),

			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Paso 2 de 4 — CLIs de terminal").
					Description("Selecciona con ESPACIO, navega con ↑/↓, Enter para continuar.").
					Options(huh.NewOptions(cliLabels...)...).
					Value(&selectedCLIs),
			).Title("CLIs DE TERMINAL"),

			huh.NewGroup(
				huh.NewConfirm().
					Title("Paso 3 de 4 — Confirmación").
					Description("¿Aplicar estos cambios y ver las instrucciones?").
					Affirmative("Sí, aplicar").
					Negative("No, cancelar").
					Value(&confirmed),
			).Title("CONFIRMACIÓN"),
		)

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
			form2 := huh.NewForm(
				huh.NewGroup(
					huh.NewMultiSelect[string]().
						Title("Paso 4 de 4 — Permisos de herramientas MCP").
						Description("Autoriza las herramientas para " + platformNames(grantTargets) + ". 'a' = todas, 'x' = ninguna.").
						Options(huh.NewOptions(toolNames...)...).
						Height(20).
						Value(&selectedPerms),
				).Title("PERMISOS DE SV-MEMORY"),
			)

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
