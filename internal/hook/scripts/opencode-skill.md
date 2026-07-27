# sv-memory

Persistent architectural memory and dependency graph for AI coding agents.

## Description

sv-memory provides two complementary capabilities:
- **sv_mem_search / sv_mem_get**: Retrieve past architectural decisions, bug fixes,
  discussions, and progress journals from persistent memory.
- **sv_graph_query / sv_graph_explain**: Query the project's code dependency graph
  to understand module structure, relationships, and community clusters.

Using these tools **before** reading source files directly saves tokens and provides
richer architectural awareness.

## Instructions

### Before reading source files

1. Call `sv_graph_query` on the module or file path you are about to read to
   understand its imports, dependencies, and structural role in the project.
2. Call `sv_mem_search` with keywords related to the task to check if a past
   decision, discussion, or bug fix already exists.
3. If you find relevant context, call `sv_mem_get` to retrieve the full content.
4. Only read the raw source file after the graph and memory context has been
   exhausted.

### When fixing bugs

1. Use `sv_mem_search` with category `bugfix` to check if this bug was
   previously diagnosed.
2. Use `sv_graph_query` on the affected module to see what depends on it and
   what it imports.
3. After fixing, save a memory with category `bugfix` via MCP.

### When making architectural decisions

1. Use `sv_mem_search` with category `decision` or `architecture`.
2. Use `sv_graph_explain` on key modules to understand their centrality.
3. Save the decision with category `architecture` or `decision`.
