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

This project uses 'sv-memory' for persistent architectural memory and structural context graph.

## Mandatory Agent Workflow:

1. **Context Initialization:** Before proposing or executing architectural changes, call 'sv_mem_search' to check past project decisions, standards, and solved bugs.
2. **Context Save:** You MUST invoke 'sv_mem_save' whenever you:
   - Fix a complex or non-obvious bug.
   - Introduce or refactor a design pattern / rule.
   - Make an explicit choice to avoid a library/framework feature.
3. **Graph Inspection:** Use 'sv_graph_query' to inspect module dependencies before deleting or restructuring code.
4. **Graph Refresh:** Execute 'sv_graph_sync' after adding major new files or modifying package structures.
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
	if strings.Contains(strContent, "<!-- SV-MEMORY:START -->") {
		// Protocol already exists, don't double inject
		return false, nil
	}

	// Append rules to the end of the file
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
