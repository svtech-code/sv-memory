package tui

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/svtech/sv-memory/internal/graph"
	"github.com/svtech/sv-memory/internal/memory"
)

// RunTUI launches an interactive terminal interface for exploring project memories and graph health.
func RunTUI(db *sql.DB, projectID, projPath string) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("\033[H\033[2J") // Clear screen
		fmt.Println("================================================================================")
		fmt.Printf(" 🧠 SV-MEMORY INTERACTIVE TERMINAL (Project: %s)\n", projectID)
		fmt.Println("================================================================================")
		fmt.Println("  [1] List Recent Memories (Compact)")
		fmt.Println("  [2] Search Memories (Keyword / FTS5 BM25)")
		fmt.Println("  [3] Inspect Memory Details by ID")
		fmt.Println("  [4] Run Graph Health Diagnostics")
		fmt.Println("  [5] Export Obsidian Vault")
		fmt.Println("  [6] Export Neo4j Cypher Script")
		fmt.Println("  [q] Quit TUI")
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Print("Select an option > ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		choice := strings.TrimSpace(input)

		switch choice {
		case "1":
			showRecentMemories(reader, db, projectID)
		case "2":
			searchMemoriesTUI(reader, db, projectID)
		case "3":
			inspectMemoryByID(reader, db, projectID)
		case "4":
			runDiagnosticsTUI(reader, db, projectID, projPath)
		case "5":
			exportObsidianTUI(reader, db, projectID, projPath)
		case "6":
			exportCypherTUI(reader, db, projectID, projPath)
		case "q", "quit", "exit":
			fmt.Println("\nExiting sv-memory TUI. Goodbye!")
			return nil
		default:
			fmt.Println("Invalid choice. Press Enter to try again...")
			_, _ = reader.ReadString('\n')
		}
	}
}

func showRecentMemories(reader *bufio.Reader, db *sql.DB, projectID string) {
	fmt.Println("\n--- 🕒 Recent Memories (Top 15) ---")
	mems, err := memory.SearchMemoriesCompact(db, projectID, "", "", 15, 0)
	if err != nil {
		fmt.Printf("Error fetching memories: %v\n", err)
	} else if len(mems) == 0 {
		fmt.Println("No memories found in project.")
	} else {
		for i, m := range mems {
			fmt.Printf("%2d. [%s] %s (ID: %s, %s)\n", i+1, strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02"))
		}
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}

func searchMemoriesTUI(reader *bufio.Reader, db *sql.DB, projectID string) {
	fmt.Print("\nEnter search query (or category filter like 'architecture'): ")
	query, _ := reader.ReadString('\n')
	query = strings.TrimSpace(query)

	if query == "" {
		return
	}

	mems, err := memory.SearchMemoriesCompact(db, projectID, query, "", 10, 0)
	if err != nil {
		fmt.Printf("Error searching: %v\n", err)
	} else if len(mems) == 0 {
		fmt.Println("No matching memories found.")
	} else {
		fmt.Printf("\nFound %d matching memories:\n", len(mems))
		for i, m := range mems {
			fmt.Printf("%2d. [%s] %s (ID: %s)\n", i+1, strings.ToUpper(m.Category), m.What, m.ID)
		}
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}

func inspectMemoryByID(reader *bufio.Reader, db *sql.DB, projectID string) {
	fmt.Print("\nEnter Memory ID: ")
	id, _ := reader.ReadString('\n')
	id = strings.TrimSpace(id)

	if id == "" {
		return
	}

	mem, err := memory.GetMemory(db, projectID, id)
	if err != nil || mem == nil {
		fmt.Printf("Memory ID '%s' not found.\n", id)
	} else {
		fmt.Println("\n--------------------------------------------------------------------------------")
		fmt.Printf("📌 TITLE: %s\n", mem.What)
		fmt.Printf("🏷️ CATEGORY: %s\n", strings.ToUpper(mem.Category))
		fmt.Printf("🆔 ID: %s | Topic Key: %s\n", mem.ID, mem.TopicKey)
		fmt.Printf("📅 Created: %s\n", mem.CreatedAt.Format("2006-01-02 15:04:05"))
		if mem.WherePath != "" {
			fmt.Printf("📁 File Path: %s\n", mem.WherePath)
		}
		fmt.Printf("\n❓ WHY:\n%s\n", mem.Why)
		fmt.Printf("\n💡 LEARNED:\n%s\n", mem.Learned)
		fmt.Println("--------------------------------------------------------------------------------")
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}

func runDiagnosticsTUI(reader *bufio.Reader, db *sql.DB, projectID, projPath string) {
	fmt.Println("\nRunning Graph Health Diagnostics...")
	report, err := graph.DiagnoseGraph(db, projectID, projPath)
	if err != nil {
		fmt.Printf("Diagnostics error: %v\n", err)
	} else {
		fmt.Println("\n" + report.String())
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}

func exportObsidianTUI(reader *bufio.Reader, db *sql.DB, projectID, projPath string) {
	outDir := "./obsidian_vault"
	fmt.Printf("\nExporting Obsidian Vault to %s...\n", outDir)
	if err := graph.ExportObsidianVault(db, projectID, outDir); err != nil {
		fmt.Printf("Export failed: %v\n", err)
	} else {
		fmt.Printf("✅ Exported Obsidian Vault successfully to %s!\n", outDir)
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}

func exportCypherTUI(reader *bufio.Reader, db *sql.DB, projectID, projPath string) {
	outFile := "./graph.cypher"
	fmt.Printf("\nExporting Cypher script to %s...\n", outFile)
	if err := graph.ExportCypher(db, projectID, outFile); err != nil {
		fmt.Printf("Export failed: %v\n", err)
	} else {
		fmt.Printf("✅ Exported Cypher script successfully to %s!\n", outFile)
	}
	fmt.Print("\nPress Enter to return to main menu...")
	_, _ = reader.ReadString('\n')
}
