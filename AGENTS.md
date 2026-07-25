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
