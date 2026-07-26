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
<!-- SV-MEMORY:END -->
