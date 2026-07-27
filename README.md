# sv-memory 🧠

**sv-memory** is a high-performance, single-binary CLI tool and Model Context Protocol (MCP) server written in **Go**. Its goal is to eliminate context amnesia for AI agents by combining persistent local memories about decisions and development standards with a structural code dependency graph.

Developed under the **SVTech** ecosystem as a free and open-source tool for the developer community.

---

## 🚀 Key Features

1. **Persistent Decision Memory:** Captures complex bugfixes, architectural decisions, and coding standards using SQLite + FTS5 (Full-Text Search) for ultra-fast searches by the AI agent.
2. **Team Synchronization (Git Sync):** Automatically syncs local memories into individual chunk files (`.sv-memory/chunks/{id}.json`) inside the repository — one JSON file per memory — so parallel saves on different branches never produce merge conflicts. Team members cloning or updating the repository will automatically integrate these memories into their local SQLite databases upon initialization.
3. **Structural Code Graph:** Analyzes projects across 15+ languages, detects imports and dependencies, computes **betweenness centrality**, detects **god nodes**, and finds **surprising connections**. Uses **Leiden community detection** to cluster related modules.
4. **Graph Visualization & Export:** Export interactive **HTML visualizations** (vis.js), **per-community wiki pages** (Markdown), and **merge** multiple graph snapshots.
5. **Agent Orchestration (Protocol Rules):** Automatically injects agent guidelines into `AGENTS.md`, `.cursorrules`, or `.windsurfrules` files in the repository root to guide AI agents to query and write memory proactively.
6. **Dependency-Free Portability:** Compiled in pure Go without requiring CGO, thanks to `modernc.org/sqlite`. The compiled binary runs directly on macOS, Linux, and Windows.

---

## 🛠️ Architecture

```text
        ┌────────────────────────────────────────────────────────┐
        │     AI Agent (Antigravity CLI, Claude, Cursor, etc)    │
        └───────────────────────────┬────────────────────────────┘
                                    │  MCP Protocol via Stdio
        ┌───────────────────────────▼────────────────────────────┐
        │                   sv-memory Binary                     │
        │                                                        │
        │  ┌──────────────────┐ ┌──────────────┐ ┌────────────┐  │
        │  │  Memory Engine   │ │  Graph       │ │ Config/Env │  │
        │  │                  │ │  Engine      │ │            │  │
        │  └────────┬─────────┘ └──────┬───────┘ └─────┬──────┘  │
        └───────────┼──────────────────┼───────────────┼─────────┘
                    │                  │               │
        ┌───────────▼──────────────────▼───────────────▼─────────┐
        │      Global SQLite (+ Synchronized FTS5 Triggers)      │
        │           (~/.config/sv-memory/storage.db)             │
        └───────────────────────────┬────────────────────────────┘
                                    │  Import / Export Sync
        ┌───────────────────────────▼────────────────────────────┐
        │             Git Repository (Versioned JSON)            │
        │             (.sv-memory/chunks/*.json)                 │
        └────────────────────────────────────────────────────────┘
```

---

## 📋 Minimum Requirements

### For End Users (Using MCP Server / CLI)

If you only want to use `sv-memory` in your development projects:

- **Dependencies:** **None.** The binary is completely self-contained. It does not require Go, Node.js, or Python on your system.
- **Compatibility:** macOS (Intel/Apple Silicon), Linux, or Windows.
- **AI Clients:** Any editor or client supporting the MCP protocol (such as **Cursor**, **Windsurf**, or **Claude Desktop**).

### For Developers (Compiling from Source)

If you want to modify the code or build the binary yourself:

- **Language:** **Go 1.26+** installed on your system.
- **Version Control:** **Git** installed.

---

## 📦 Installation and Setup

To start using `sv-memory`, you must complete **two mandatory phases**:

1. **Global Phase:** Install the binary on your system and register the MCP server in your AI editor or CLI.
2. **Local Phase:** Initialize the memory in each development project directory you want to work on.

---

### Step 1: Install the Binary (Global Phase)

#### Option A: Quick Global Installation (Coming Soon — Requires GitHub Releases)
Once the first stable version is published to GitHub Releases, you will be able to download and install the tool globally on your system (macOS/Linux) with a single command:
```bash
curl -fsSL https://raw.githubusercontent.com/svtech/sv-memory/main/install.sh | bash
```
_This script will automatically detect your OS and architecture, download the appropriate binary from GitHub Releases, and install it to `/usr/local/bin/sv-memory`._

#### Option B: Compile from Source (Developers)
If you prefer to clone the repository and build the binary manually:
1. Clone the repository and enter the directory:
   ```bash
   git clone https://github.com/svtech/sv-memory.git
   cd sv-memory
   ```
2. Build the optimized executable:
   ```bash
   go build -o sv-memory ./cmd/sv-memory
   ```
3. Copy the resulting binary to a directory in your system PATH so it is available globally:
   ```bash
   sudo cp sv-memory /usr/local/bin/
   ```

---

### Step 2: Register the MCP Server in your AI Client (Global Phase)
> [!IMPORTANT]
> **This step is mandatory.** Initializing the project with `init` is not enough. If you do not register the server in your AI client (like Cursor or Claude Desktop), the AI agent will not know where to find `sv-memory` or how to communicate with it.

Go to the **[AI Clients Configuration](#%EF%B8%8F-ai-clients-configuration)** section at the end of this document and add the corresponding configuration. This instructs your editor or AI CLI to launch the `sv-memory mcp` server in the background automatically.

---

### Step 3: Initialize Your Project (Local Phase)
> [!IMPORTANT]
> **This step is mandatory for each project.** You must tell `sv-memory` which folder to index and where to inject the AI agent instruction protocol.

1. Open your terminal and navigate to the root of the project you want to work on (e.g., `cd ~/my-web-project`).
2. Run the initialization command:
   ```bash
   sv-memory init
   ```
   *(This command computes a unique project ID based on the Git repository root, registers the project in the global SQLite database, scans files to build the initial dependency graph, and creates the `AGENTS.md` file).*

---

## 💻 CLI Commands

### 1. `sv-memory init`

Initializes `sv-memory` in the current project:

- Computes a unique project ID based on the Git repository root.
- Registers the project in the global SQLite database.
- Scans files and builds the initial dependency graph.
- Imports shared memories from `.sv-memory/chunks/{id}.json` (or legacy `memories.json`) if they exist.
- Injects agent protocol guidelines into `AGENTS.md`, `.cursorrules`, or `.windsurfrules`.

```bash
sv-memory init
```

### 2. `sv-memory mcp`

Starts the Model Context Protocol (MCP) server over standard input/output (`stdio`). This is the command used by AI clients to interact with the tool.

```bash
sv-memory mcp
```

### 3. `sv-memory sync`

Manually triggers synchronization. Imports new memories from the Git chunk files (`chunks/*.json` or legacy `memories.json`) to SQLite, and exports all local SQLite memories of the project to individual chunk files in `.sv-memory/chunks/`.

```bash
sv-memory sync
```

### 4. `sv-memory configure`

Launches an interactive setup wizard in the terminal to configure your editors and CLIs.
* **Phase 1 (Editors):** Select which editors to configure (`Cursor`, `VS Code`, `Zed`, `Windsurf`).
* **Phase 2 (CLIs):** Select which terminal tools to configure (`Claude Code`, `OpenCode`, `Codex`, `Antigravity CLI (agy)`).
* **Phase 3 (Application):** Generates a summary, requests confirmation, performs configuration injection safely for supported tools, and displays manual step-by-step instructions for the rest.

```bash
sv-memory configure
```

### 5. `sv-memory diagnose`

Runs health checks verifying database connections, schemas, folders, write permissions, and active settings.

```bash
sv-memory diagnose
```

### 6. `sv-memory stats`

Displays project statistics: total memories, deleted memories, recent 24-hour saves, sessions count, active sessions, and relation counts.

```bash
sv-memory stats
```

### 7. `sv-memory graph rebuild`

Forces a full re-scan of the project directory tree and updates the code graph nodes along with their relationships.

```bash
sv-memory graph rebuild
```

### 8. `sv-memory graph path <source> <target>`

Finds the shortest dependency path between two nodes in the code graph (up to 10 hops).

```bash
sv-memory graph path utils/helpers.ts services/api.ts
```

### 9. `sv-memory graph explain <node>`

Shows detailed information for a node: type, label, path, metadata, and fan-in/fan-out metrics.

```bash
sv-memory graph explain internal/db/db.go
```

### 10. `sv-memory graph communities`

Detects and lists community clusters using the Leiden algorithm, showing each community's members, centrality scores, and god nodes.

```bash
sv-memory graph communities
```

### 11. `sv-memory graph wiki [--output dir]`

Exports Markdown wiki pages for each community, including member files, centrality scores, and inter-community dependencies.

```bash
sv-memory graph wiki --output graph-wiki
```

### 12. `sv-memory graph viz [--output file]`

Generates an interactive HTML visualization of the graph using vis.js with community coloring, physics simulation, and node filtering.

```bash
sv-memory graph viz --output graph.html
```

### 13. `sv-memory graph merge <json-file>`

Merges a JSON graph snapshot into the current project's graph, combining nodes and edges.

```bash
sv-memory graph merge backup.json
```

### 14. `sv-memory export [output-file]`

Exports all non-deleted memories for this project to a portable JSON file.

```bash
sv-memory export memories-backup.json
```

### 15. `sv-memory import <input-file>`

Imports memories from a JSON file using upsert by ID.

```bash
sv-memory import memories-backup.json
```

### 16. `sv-memory obsidian-export [-o output-dir]`

Exports all project memories as Markdown files structured as an Obsidian vault.

```bash
sv-memory obsidian-export -o my-obsidian-vault
```

### 17. `sv-memory sync`

Manually triggers synchronization. Imports new memories from the Git chunk files (`chunks/*.json` or legacy `memories.json`) to SQLite, and exports all local SQLite memories of the project to individual chunk files in `.sv-memory/chunks/`.

```bash
sv-memory sync
```

### 18. `sv-memory delete session <session-id>`

Deletes an empty session (fails if the session has associated memories).

```bash
sv-memory delete session abc12345
```

### 19. `sv-memory delete project <project-id> [--hard]`

Cascade-deletes all project data. Soft-deletes by default; `--hard` removes permanently.

```bash
sv-memory delete project proj1234 --hard
```

### 20. `sv-memory projects list`

Lists all registered projects with their ID, name, path, memory counts, and session counts.

```bash
sv-memory projects list
```

### 21. `sv-memory conflicts`

Displays conflicting memories and detected semantic overlaps across the project.

```bash
sv-memory conflicts
```

---

## 🧩 Model Context Protocol (MCP) Tools

Once connected, `sv-memory` exposes **25 MCP tools** to AI agents:

### Memory Tools
1. **`sv_mem_save`**: Persists architectural decisions, bugfixes, or development guides with automatic Git sync.
2. **`sv_mem_search`**: Full-Text Search (FTS5) on saved memories with category filters.
3. **`sv_mem_get`**: Retrieves full content of a specific memory with optional truncation.
4. **`sv_mem_timeline`**: Chronological context around a specific memory (Layer 2 of progressive disclosure).
5. **`sv_mem_suggest_topic_key`**: Generates a stable category/kebab-case topic key for upsert.
6. **`sv_mem_judge`**: Creates relations between memories (supersedes, conflicts_with, relates_to).
7. **`sv_mem_compare`**: Side-by-side comparison of two memories.
8. **`sv_mem_review`**: Finds memories needing maintenance (stale, duplicates, consolidation candidates).
9. **`sv_mem_stats`**: Aggregate memory statistics and per-category breakdowns.
10. **`sv_mem_current_project`**: Retrieves active project ID, name, and path.
11. **`sv_mem_delete`**: Soft-deletes (or hard-deletes) a memory.
12. **`sv_mem_capture_passive`**: Logs lightweight journal entries automatically.
13. **`sv_mem_conflicts`**: Detects and surfaces conflicting memories with semantic overlap analysis.

### Session Tools
14. **`sv_mem_session_start`**: Registers a new coding session.
15. **`sv_mem_session_end`**: Closes an active session with summary.
16. **`sv_mem_session_summary`**: Updates goal, discoveries, and next steps.
17. **`sv_mem_context`**: Recovers context from the last completed session (post-compaction recovery).

### Graph Tools
18. **`sv_graph_query`**: BFS dependency query with configurable depth. Returns Mermaid diagram.
19. **`sv_graph_path`**: Shortest dependency path between two nodes.
20. **`sv_graph_sync`**: Incrementally syncs the dependency graph from file changes.
21. **`sv_graph_explain`**: Detailed node information with fan-in/fan-out metrics.
22. **`sv_graph_god_nodes`**: Identifies highly-connected nodes (centrality analysis).
23. **`sv_graph_surprising_connections`**: Finds unexpected or non-obvious dependencies.
24. **`sv_graph_viz`**: Generates interactive HTML visualization with community coloring.
25. **`sv_graph_merge`**: Merges a JSON graph snapshot into the current graph.

---

## ⚙️ AI Clients Configuration

To use `sv-memory` as an MCP server, configure it in your preferred AI client using the absolute path to your compiled binary (or simply `sv-memory` if installed globally in your PATH).

### 1. Claude Desktop
Add the following configuration in your `claude_desktop_config.json` (Path on macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`, on Linux: `~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "sv-memory": {
      "command": "/path/to/your/binary/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

### 2. Claude Code (Terminal CLI)
Add the server globally to Anthropic's CLI tool:
```bash
claude mcp add sv-memory -- /path/to/your/binary/sv-memory mcp
```

### 3. Cursor / Windsurf
Go to Cursor Settings -> Features -> MCP, and add a new server:
* **Name:** `sv-memory`
* **Type:** `command`
* **Command:** `/path/to/your/binary/sv-memory mcp`

### 4. Zed Editor
Add the following to your `~/.config/zed/settings.json`:
```json
{
  "mcp_servers": {
    "sv-memory": {
      "command": "/path/to/your/binary/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

### 5. Antigravity CLI / OpenCode / Codex
If you use a compatible development environment, add the server to the global `mcp_config.json` file or configure it locally:
```json
{
  "mcpServers": {
    "sv-memory": {
      "command": "/path/to/your/binary/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

---

## 💡 Example Interaction and Workflow

Once configured, the autonomous workflow with your AI agent (such as Claude, Gemini, or Cursor) is as follows:

1. **Upon Starting in the Project:**
   The agent reads the `AGENTS.md` file and immediately performs an automatic search:
   > *Agent runs internally:* `sv_mem_search(query="bugfix")` or `sv_mem_search(query="architecture")`
   
2. **Before Proposing Code Changes:**
   Before refactoring or deleting code, the agent verifies dependencies:
   > *Agent runs internally:* `sv_graph_query(path_or_node="internal/db/db.go", depth=1)`
   > *Result:* The agent receives file relationships and visualizes a Mermaid diagram, preventing it from breaking external modules.

3. **Analyzing Architecture:**
   The agent can identify critical modules, community structure, and surprising dependencies:
   > *Agent runs internally:* `sv_graph_god_nodes()` or `sv_graph_surprising_connections()`
   > *Result:* The agent highlights central files, community clusters, and unexpected dependencies that may indicate architectural concerns.

4. **Upon Completing a Task (e.g., Fixing a Complex Bug):**
   The agent records the decision:
   > *Agent runs internally:*
   > `sv_mem_save(category="bugfix", what="Usage of modernc.org/sqlite", why="Avoid CGO dependency to enable clean cross-compilation", learned="Always use modernc.org/sqlite for portable SQLite databases in Go", where_path="internal/db/db.go")`
   > *Result:* The decision is saved in SQLite and immediately synchronized to `.sv-memory/chunks/{id}.json` (one file per memory, avoiding merge conflicts) so your team receives it on their next `git pull`.

---

## 📄 License

This project is licensed under the MIT License. See the LICENSE file for details.
