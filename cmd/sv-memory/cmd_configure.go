package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/perm"
)

func promptMultiSelect(title string, options []string) ([]string, error) {
	fmt.Printf("\n=== %s ===\n", title)
	for i, opt := range options {
		fmt.Printf("  %2d. %s\n", i+1, opt)
	}
	fmt.Print("\nIngresa los números separados por coma (ej: 1,3,4) o 0 para ninguno: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	input = strings.TrimSpace(input)
	if input == "" || input == "0" {
		return nil, nil
	}
	parts := strings.Split(input, ",")
	selected := make([]string, 0, len(parts))
	seen := make(map[int]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("  Opción inválida: %s, ignorada.\n", p)
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		selected = append(selected, options[n-1])
	}
	return selected, nil
}

func promptConfirm(msg string) (bool, error) {
	fmt.Printf("%s (s/N): ", msg)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "s" || input == "si" || input == "y" || input == "yes", nil
}

// promptPermissionSelect lists the available sv-memory MCP tools with a short
// description each, so the user can grant permissions with full transparency.
// Accepts a comma-separated list of numbers, 'all' for every tool, or 0/empty
// for none. Returns the selected tool names.
func promptPermissionSelect(title string, tools []mcp.Tool) ([]string, error) {
	fmt.Printf("\n=== %s ===\n", title)
	for i, t := range tools {
		fmt.Printf("  %2d. %-30s %s\n", i+1, t.Name, t.Description)
	}
	fmt.Print("\nIngresa los números separados por coma (ej: 1,3,4), 'all' para todas, o 0 para ninguna: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || input == "0" {
		return nil, nil
	}
	if input == "all" {
		names := make([]string, len(tools))
		for i, t := range tools {
			names[i] = t.Name
		}
		return names, nil
	}
	parts := strings.Split(input, ",")
	selected := make([]string, 0, len(parts))
	seen := make(map[int]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > len(tools) {
			fmt.Printf("  Opción inválida: %s, ignorada.\n", p)
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		selected = append(selected, tools[n-1].Name)
	}
	return selected, nil
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure local editor and CLI environments (Cursor, VS Code, Zed, Windsurf, Claude Code, OpenCode, Codex, Antigravity) with sv-memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ShowBanner()

		// 1. Phase 1: Editors
		predefinedEditors := config.GetPredefinedEditors()
		editorOptions := make([]string, len(predefinedEditors))
		editorMap := make(map[string]config.TargetTool)
		for i, editor := range predefinedEditors {
			mode := "Instrucciones manuales"
			if editor.Auto {
				mode = "Autoconfiguración"
			}
			label := fmt.Sprintf("%s (%s)", editor.Name, mode)
			editorOptions[i] = label
			editorMap[label] = editor
		}

		var selectedEditorLabels []string
		var err error
		selectedEditorLabels, err = promptMultiSelect(
			"CONFIGURACIÓN DE EDITORES DE CÓDIGO",
			editorOptions,
		)
		if err != nil {
			return err
		}

		var selectedTools []config.TargetTool
		for _, label := range selectedEditorLabels {
			selectedTools = append(selectedTools, editorMap[label])
		}

		fmt.Println()

		// 2. Phase 2: CLIs
		predefinedCLIs := config.GetPredefinedCLIs()
		cliOptions := make([]string, len(predefinedCLIs))
		cliMap := make(map[string]config.TargetTool)
		for i, cli := range predefinedCLIs {
			mode := "Instrucciones manuales"
			if cli.Auto {
				mode = "Autoconfiguración"
			}
			label := fmt.Sprintf("%s (%s)", cli.Name, mode)
			cliOptions[i] = label
			cliMap[label] = cli
		}

		var selectedCLILabels []string
		selectedCLILabels, err = promptMultiSelect(
			"CONFIGURACIÓN DE CLIs DE TERMINAL",
			cliOptions,
		)
		if err != nil {
			return err
		}

		for _, label := range selectedCLILabels {
			selectedTools = append(selectedTools, cliMap[label])
		}

		// 3. Phase 3: Summary and Confirmation
		if len(selectedTools) == 0 {
			fmt.Println("\nNo seleccionaste ninguna herramienta. Operación cancelada.")
			return nil
		}

		fmt.Println("\n=== RESUMEN DE SELECCIÓN ===")
		fmt.Println("Se configurarán las siguientes herramientas:")
		for _, tool := range selectedTools {
			mode := "Configuración manual"
			if tool.Auto {
				mode = "Configuración automática"
			}
			fmt.Printf(" * %s (%s)\n", tool.Name, mode)
		}
		fmt.Println()

		confirm, err := promptConfirm("¿Deseas aplicar estos cambios y ver las instrucciones?")
		if err != nil {
			return err
		}

		if !confirm {
			fmt.Println("Operación cancelada por el usuario.")
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

		// 4. Phase 4: Grant MCP tool permissions to the allow-listed platforms
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
			selected, err := promptPermissionSelect(
				"FASE 4 · PERMISOS DE SV-MEMORY — selecciona las herramientas a autorizar",
				mcp.AllTools,
			)
			if err != nil {
				return err
			}

			if len(selected) == 0 {
				fmt.Println("No se otorgarán permisos adicionales.")
			} else {
				fmt.Println("\n=== OTORGANDO PERMISOS ===")
				for _, tool := range grantTargets {
					res, err := perm.Grant(perm.Platform(tool.ID), selected, false)
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
