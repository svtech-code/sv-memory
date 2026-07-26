package protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const protocolTemplate = `
<!-- SV-MEMORY:START -->
# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Mandatory Agent Workflow:

1. **Context Initialization:** Before proposing or executing any changes:
   - Call 'sv_mem_search' (e.g., filter category 'journal' or 'postmortem') to check the latest work logs, achievements, and pending next steps.
   - Call 'sv_mem_search' (e.g., filter category 'architecture' or 'decision') to check past project decisions, standards, and solved bugs.
2. **Context Save:** You MUST invoke 'sv_mem_save' to persist knowledge:
   - When concluding a task or wrapping up your session, save a progress journal entry: set 'category' to 'journal', record what was done, what went well ('impact'), what went wrong/roadblocks ('errors_faced'), and pending tasks ('next_steps').
   - When fixing a complex/non-obvious bug (category 'bugfix').
   - When introducing or refactoring a design pattern/rule (category 'architecture' or 'decision').
   - When choosing to avoid a library/framework feature.
3. **Graph Inspection:** Use 'sv_graph_query' to inspect module dependencies before deleting or restructuring code.
4. **Graph Refresh:** Execute 'sv_graph_sync' after adding major new files or modifying package structures.

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages in English using the Conventional Commits format (e.g., 'feat(scope): description').
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->
`

// InjectProtocol checks and injects sv-memory agent rules into target files.
func InjectProtocol(projPath string) ([]string, error) {
	targets := []string{"AGENTS.md", ".cursorrules", ".windsurfrules"}
	var injectedFiles []string
	foundAny := false

	for _, target := range targets {
		filePath := filepath.Join(projPath, target)
		if _, err := os.Stat(filePath); err == nil {
			foundAny = true
			injected, err := injectToFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to inject to %s: %w", target, err)
			}
			if injected {
				injectedFiles = append(injectedFiles, target)
			}
		}
	}

	// If no existing files were found, create AGENTS.md in the root
	if !foundAny {
		agentsPath := filepath.Join(projPath, "AGENTS.md")
		err := os.WriteFile(agentsPath, []byte(strings.TrimSpace(protocolTemplate)+"\n"), 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create AGENTS.md: %w", err)
		}
		injectedFiles = append(injectedFiles, "AGENTS.md")
	}

	return injectedFiles, nil
}

func injectToFile(filePath string) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}

	strContent := string(content)
	if strings.Contains(strContent, "<!-- SV-MEMORY:START -->") && strings.Contains(strContent, "<!-- SV-MEMORY:END -->") {
		startIndex := strings.Index(strContent, "<!-- SV-MEMORY:START -->")
		endIndex := strings.Index(strContent, "<!-- SV-MEMORY:END -->") + len("<!-- SV-MEMORY:END -->")

		oldBlock := strContent[startIndex:endIndex]
		newBlock := strings.TrimSpace(protocolTemplate)

		if oldBlock == newBlock {
			return false, nil
		}

		newContent := strContent[:startIndex] + newBlock + strContent[endIndex:]
		err = os.WriteFile(filePath, []byte(newContent), 0644)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// Append rules to the end of the file if no block exists
	divider := "\n\n"
	if len(strContent) == 0 || strings.HasSuffix(strContent, "\n") {
		divider = "\n"
	}

	newContent := strContent + divider + strings.TrimSpace(protocolTemplate) + "\n"
	err = os.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return false, err
	}

	return true, nil
}
