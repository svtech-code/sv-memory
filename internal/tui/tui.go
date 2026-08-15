package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// bannerCyan is the brand color used by the SV Tech banner (#00B0C2). It
// matches the theme used by `sv-memory configure` so both UIs look alike.
const bannerCyan = "#00B0C2"

// tuiTheme returns a huh theme that matches the SV Tech brand color, mirroring
// configureTheme() in cmd/sv-memory so the interactive TUI and the configure
// wizard share the same look.
func tuiTheme() *huh.Theme {
	t := huh.ThemeCharm()

	cyan := lipgloss.Color(bannerCyan)
	lightCyan := lipgloss.Color("#4FB8C4")

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

// RunTUI launches an interactive terminal interface for exploring project
// memories and graph health. The main menu is a huh select rendered in a loop;
// each sub-screen is its own form. Ctrl+C aborts a sub-form (returning to the
// menu) and quits the whole TUI from the main menu.
func RunTUI(db *sql.DB, projectID, projPath string) error {
	showBannerTUI()
	for {
		choice, err := mainMenu(projectID)
		if err != nil || choice == "quit" {
			return nil
		}
		switch choice {
		case "recent":
			showRecentMemories(db, projectID)
		case "search":
			searchMemoriesTUI(db, projectID)
		case "inspect":
			inspectMemoryByID(db, projectID)
		case "diagnostics":
			runDiagnosticsTUI(db, projectID, projPath)
		case "obsidian":
			exportObsidianTUI(db, projectID)
		case "cypher":
			exportCypherTUI(db, projectID)
		}
	}
}

// showBannerTUI prints the SV Tech styled ASCII logo in a box, mirroring the
// banner shown by `sv-memory configure`.
func showBannerTUI() {
	cHex := "\x1b[38;2;0;176;194m"
	reset := "\x1b[39m"

	logo := []string{
		"███████╗██╗   ██╗    ███╗   ███╗███████╗███╗   ███╗ ██████╗ ██████╗ ██╗   ██╗",
		"██╔════╝██║   ██║    ████╗ ████║██╔════╝████╗ ████║██╔═══██╗██╔══██╗╚██╗ ██╔╝",
		"███████╗██║   ██║    ██╔████╔██║█████╗  ██╔████╔██║██║   ██║██████╔╝ ╚████╔╝ ",
		"╚════██║╚██╗ ██╔╝    ██║╚██╔╝██║██╔══╝  ██║╚██╔╝██║██║   ██║██╔══██╗  ╚██╔╝  ",
		"███████║ ╚████╔╝     ██║ ╚═╝ ██║███████╗██║ ╚═╝ ██║╚██████╔╝██║  ██║   ██║   ",
		"╚══════╝  ╚═══╝      ╚═╝     ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝ ",
	}

	logoW := 77
	for _, line := range logo {
		if w := utf8.RuneCountInString(strings.TrimRight(line, " ")); w > logoW {
			logoW = w
		}
	}
	pad := 4
	contentW := logoW + pad

	topEdge := "╔" + strings.Repeat("═", contentW) + "╗"
	bottomEdge := "╚" + strings.Repeat("═", contentW) + "╝"
	emptyLine := "║" + strings.Repeat(" ", contentW) + "║"

	fmt.Println(cHex + topEdge + reset)
	fmt.Println(cHex + emptyLine + reset)

	for _, line := range logo {
		lineRunes := utf8.RuneCountInString(line)
		fmt.Printf("%s║  %s%s  ║%s\n", cHex, line, strings.Repeat(" ", logoW-lineRunes), reset)
	}

	fmt.Println(cHex + emptyLine + reset)

	printCenter := func(text string) {
		pad := contentW - len(text)
		left := pad / 2
		right := pad - left
		fmt.Printf("%s║%s%s%s║%s\n", cHex, strings.Repeat(" ", left), text, strings.Repeat(" ", right), reset)
	}

	printCenter("Context Memory & Code Graph Builder")
	printCenter("Prevent context amnesia in your workspace")

	fmt.Println(cHex + emptyLine + reset)
	fmt.Println(cHex + bottomEdge + reset)
	fmt.Println()
}

// mainMenu renders the top-level selection. Returning an error means the user
// aborted with Ctrl+C, which the caller treats as "quit TUI".
func mainMenu(projectID string) (string, error) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("SV-MEMORY INTERACTIVE TERMINAL\n").
				Description(fmt.Sprintf("\nProject: %s", projectID)).
				Options(
					huh.NewOption("List Recent Memories (Compact)", "recent"),
					huh.NewOption("Search Memories (Keyword / FTS5 BM25)", "search"),
					huh.NewOption("Inspect Memory Details by ID", "inspect"),
					huh.NewOption("Run Graph Health Diagnostics", "diagnostics"),
					huh.NewOption("Export Obsidian Vault", "obsidian"),
					huh.NewOption("Export Neo4j Cypher Script", "cypher"),
					huh.NewOption("Quit TUI", "quit"),
				).
				Value(&choice),
		).Title("MAIN MENU\n"),
	).WithTheme(tuiTheme())
	if err := form.Run(); err != nil {
		return "", err
	}
	return choice, nil
}

// showNote renders a display-only screen. A lone note is blocking (Enter
// submits) so it doubles as the "press Enter to return" affordance. On abort
// (Ctrl+C) the error is ignored and control returns to the main menu. Long
// content is truncated so it never overflows the terminal.
func showNote(title, content string) {
	// Truncate by runes so multibyte characters are never split.
	const maxChars = 4000
	if runes := []rune(content); len(runes) > maxChars {
		content = string(runes[:maxChars]) + "\n\n… (content truncated for this view)"
	}
	_ = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(content).
				Height(18).
				Next(true).
				NextLabel("Back to main menu"),
		).Title("SV-MEMORY"),
	).WithTheme(tuiTheme()).Run()
}

func showRecentMemories(db *sql.DB, projectID string) {
	mems, err := memory.SearchMemoriesCompact(db, projectID, "", "", 15, 0)
	showNote("Recent Memories", renderRecent(mems, err))
}

func searchMemoriesTUI(db *sql.DB, projectID string) {
	var query string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Search Memories").
				Description("Enter a search query (FTS5 BM25 keyword search).").
				Value(&query),
		).Title("SEARCH"),
	).WithTheme(tuiTheme()).Run(); err != nil {
		return // aborted → back to main menu
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	mems, err := memory.SearchMemoriesCompact(db, projectID, query, "", 10, 0)
	showNote("Search Results", renderSearchResults(mems, err))
}

func inspectMemoryByID(db *sql.DB, projectID string) {
	var id string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Inspect Memory Details").
				Description("Enter a memory ID to view its full details.").
				Value(&id),
		).Title("INSPECT"),
	).WithTheme(tuiTheme()).Run(); err != nil {
		return // aborted → back to main menu
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	mem, err := memory.GetMemory(db, projectID, id)
	showNote("Memory Details", renderMemoryDetail(mem, err))
}

func runDiagnosticsTUI(db *sql.DB, projectID, projPath string) {
	report, err := graph.DiagnoseGraph(db, projectID, projPath)
	content := ""
	if err != nil {
		content = fmt.Sprintf("Diagnostics error: %v", err)
	} else {
		content = report.String()
	}
	showNote("Graph Health Diagnostics", content)
}

func exportObsidianTUI(db *sql.DB, projectID string) {
	outDir := "./obsidian_vault"
	if err := graph.ExportObsidianVault(db, projectID, outDir); err != nil {
		showNote("Export Obsidian Vault", fmt.Sprintf("Export failed: %v", err))
		return
	}
	showNote("Export Obsidian Vault", fmt.Sprintf("Exported Obsidian Vault successfully to %s!", outDir))
}

func exportCypherTUI(db *sql.DB, projectID string) {
	outFile := "./graph.cypher"
	if err := graph.ExportCypher(db, projectID, outFile); err != nil {
		showNote("Export Cypher Script", fmt.Sprintf("Export failed: %v", err))
		return
	}
	showNote("Export Cypher Script", fmt.Sprintf("Exported Cypher script successfully to %s!", outFile))
}

func renderRecent(mems []*memory.MemorySearchResult, err error) string {
	var sb strings.Builder
	if err != nil {
		return fmt.Sprintf("Error fetching memories: %v", err)
	}
	if len(mems) == 0 {
		return "No memories found in project."
	}
	fmt.Fprintf(&sb, "Found %d recent memories:\n\n", len(mems))
	for i, m := range mems {
		fmt.Fprintf(&sb, "%2d. [%s] %s (ID: %s, %s)\n",
			i+1, strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02"))
	}
	return sb.String()
}

func renderSearchResults(mems []*memory.MemorySearchResult, err error) string {
	var sb strings.Builder
	if err != nil {
		return fmt.Sprintf("Error searching: %v", err)
	}
	if len(mems) == 0 {
		return "No matching memories found."
	}
	fmt.Fprintf(&sb, "Found %d matching memories:\n\n", len(mems))
	for i, m := range mems {
		fmt.Fprintf(&sb, "%2d. [%s] %s (ID: %s)\n", i+1, strings.ToUpper(m.Category), m.What, m.ID)
	}
	return sb.String()
}

func renderMemoryDetail(mem *memory.Memory, err error) string {
	var sb strings.Builder
	if err != nil || mem == nil {
		sb.WriteString("Memory not found or error retrieving it.")
		if err != nil {
			fmt.Fprintf(&sb, "\nError: %v", err)
		}
		return sb.String()
	}
	fmt.Fprintf(&sb, "Title: %s\n", mem.What)
	fmt.Fprintf(&sb, "Category: %s\n", strings.ToUpper(mem.Category))
	fmt.Fprintf(&sb, "ID: %s | Topic: %s\n", mem.ID, mem.TopicKey)
	fmt.Fprintf(&sb, "Created: %s\n", mem.CreatedAt.Format("2006-01-02 15:04:05"))
	if mem.WherePath != "" {
		fmt.Fprintf(&sb, "Path: %s\n", mem.WherePath)
	}
	fmt.Fprintf(&sb, "\nWhy:\n%s\n", mem.Why)
	fmt.Fprintf(&sb, "\nLearned:\n%s\n", mem.Learned)
	return sb.String()
}
