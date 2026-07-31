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

1. **Context Initialization (Search-Before-Work):** Before proposing or executing any changes, memory must be consulted first:
   - Call 'sv_mem_session_start' at the beginning of work to open a session and receive the Auto-Boot Context Bundle (previous session summary + key decisions).
   - Call 'sv_mem_search' with the topic keywords of the current task (feature, component, style, module) to check past decisions, standards, and solved bugs.
   - Call 'sv_mem_search' (e.g., filter category 'journal', 'postmortem', 'discussion', 'idea' or 'qa') to check the latest work logs, achievements, pending next steps, and past conversations, questions, or ideas.
   - Call 'sv_mem_search' (e.g., filter category 'architecture' or 'decision') to check past project decisions, standards, and solved bugs.
   - **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding. Never answer from assumptions alone — memory first, code second.
   - **After a context reset or compaction:** Call 'sv_mem_context' immediately to recover the last session state — goal, summary, and associated memories.

2. **Session Lifecycle (Token Economization):**
   - **Session start:** Call 'sv_mem_session_start' at the beginning of work to register a new session and load the Auto-Boot Context Bundle.
   - **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
   - **Session summary:** Call 'sv_mem_session_summary' with goal, discoveries, accomplished, next steps, and files before closing.
   - **Session end:** Call 'sv_mem_session_end' to mark the session as completed.
   - **After compaction or context reset:** Call 'sv_mem_context' immediately to recover the last session state — goal, summary, and associated memories.

3. **Progressive Disclosure (Token-Efficient Retrieval):**
   Use the 3-layer pattern to minimise tokens:
   - **Layer 1 — Search:** Call 'sv_mem_search' to get a compact list (IDs + titles) of relevant memories (~80 tokens/result).
   - **Layer 2 — Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory.
   - **Layer 3 — Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
   Never dump all fields from search — drill down on demand.

4. **Topic Keys (Upsert Semantics):**
   - Use 'sv_mem_suggest_topic_key(category, what)' to generate a stable 'category/kebab-case' key.
   - Pass 'topic_key' to 'sv_mem_save' to enable upsert: saves to the same project+topic update in place (revision_count++) instead of creating a new record.
   - Use topic keys for evolving topics (architecture decisions, design systems, long-running features, recurring patterns). Skip for one-off bugs or single facts.
   - **Convention:** Always kebab-case in English. Examples: 'standard/design-system', 'architecture/component-card', 'decision/use-bun-instead-of-npm', 'standard/workflow-git-commits', 'bugfix/tab-transition-absolute-position'.

5. **Context Save (Session Compaction):** You MUST invoke 'sv_mem_save' to persist knowledge and compact the session context before finishing:
   - **Session Compaction:** At the end of every work session or major task, save a progress journal entry (category 'journal'): write a compacted summary of the conversation, key decisions made, code changes, and precise next steps. This serves as a lightweight checkpoint for future agents to resume context immediately.
   - When a significant Q&A, discussion, or improvement idea is discussed or agreed upon, save it: set 'category' to 'discussion', 'idea', or 'qa' to record the insights, questions, answers, and rationale.
   - When fixing a complex/non-obvious bug (category 'bugfix').
   - When introducing or refactoring a design pattern/rule (category 'architecture' or 'decision').
   - When choosing to avoid a library/framework feature.
   - **Passive capture:** Use 'sv_mem_capture_passive' to log lightweight observations (files modified, tests failing, small changes) without requiring an explicit save decision.

6. **Memory Capture Guidelines (when to save what):**
   Always persist design knowledge as structured memories with a topic_key, not just session journals:

   | Situation | Category | topic_key example |
   | :--- | :--- | :--- |
   | Visual style / design system / CSS / Tailwind tokens | 'standard' | standard/design-system |
   | Reusable component or UI pattern | 'architecture' | architecture/component-card |
   | Workflow / methodology / build & dev process | 'standard' | standard/workflow-dev-process |
   | Architectural decision made (and its rationale) | 'decision' | decision/... |
   | Code convention / naming / folder structure | 'standard' | standard/code-conventions |
   | Complex or non-obvious bug fixed | 'bugfix' | bugfix/... |
   | Relevant Q&A with lasting value | 'qa' | qa/... |
   | Rejected library or framework feature | 'decision' | decision/avoid-... |

   **Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as 'standard' or 'architecture' with a topic_key. A journal is not a substitute — journals document progress, 'standard'/'architecture'/'decision' preserve the "how" and the "why" for future sessions.

7. **Graph Inspection:** Use 'sv_graph_query' to inspect module dependencies before deleting or restructuring code.

8. **Graph Refresh:** Execute 'sv_graph_sync' after adding major new files or modifying package structures.

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): descripcion'). Use Spanish by default, unless specified otherwise for the project.
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

		if endIndex <= startIndex {
			// Malformed block (END appears before START) — replace entire file
			newContent := strings.TrimSpace(protocolTemplate) + "\n"
			err = os.WriteFile(filePath, []byte(newContent), 0644)
			if err != nil {
				return false, err
			}
			return true, nil
		}

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
