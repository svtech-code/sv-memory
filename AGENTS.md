<!-- SV-MEMORY:START -->
# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Mandatory Agent Workflow:

1. **Context Initialization:** Before proposing or executing any changes:
   - Call 'sv_mem_search' (e.g., filter category 'journal', 'postmortem', 'discussion', 'idea' or 'qa') to check the latest work logs, achievements, pending next steps, and past conversations, questions, or ideas.
   - Call 'sv_mem_search' (e.g., filter category 'architecture' or 'decision') to check past project decisions, standards, and solved bugs.
2. **Context Save (Session Compaction):** You MUST invoke 'sv_mem_save' to persist knowledge and compact the session context before finishing:
   - **Session Compaction:** At the end of every work session or major task, save a progress journal entry (category 'journal'): write a compacted summary of the conversation, key decisions made, code changes, and precise next steps. This serves as a lightweight checkpoint for future agents to resume context immediately.
   - When a significant Q&A, discussion, or improvement idea is discussed or agreed upon, save it: set 'category' to 'discussion', 'idea', or 'qa' to record the insights, questions, answers, and rationale.
   - When fixing a complex/non-obvious bug (category 'bugfix').
   - When introducing or refactoring a design pattern/rule (category 'architecture' or 'decision').
   - When choosing to avoid a library/framework feature.
3. **Graph Inspection:** Use 'sv_graph_query' to inspect module dependencies before deleting or restructuring code.
4. **Graph Refresh:** Execute 'sv_graph_sync' after adding major new files or modifying package structures.

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): descripcion'). Use Spanish by default, unless specified otherwise for the project.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->

## Project-Specific Rules (sv-memory):
- **Commit Language:** For this repository specifically, all commit messages must be written in English.
