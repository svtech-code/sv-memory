# sv-memory 🧠

**sv-memory** is a high-performance, single-binary CLI tool and Model Context Protocol (MCP) server written in **Go**. Its goal is to eliminate context amnesia for AI agents by combining persistent local memories about decisions and development standards with a structural code dependency graph.

Developed under the **SVTech** ecosystem as a free and open-source tool for the developer community.

---

## 🚀 Key Features

1. **Persistent Decision Memory:** Captures complex bugfixes, architectural decisions, and coding standards using SQLite + FTS5 (Full-Text Search) for ultra-fast searches by the AI agent.
2. **Team Synchronization (Git Sync):** Automatically syncs local memories into the `.sv-memory/memories.json` file inside the repository. Team members cloning or updating the repository will automatically integrate these memories into their local SQLite databases upon initialization.
3. **Pure Go Code Graph:** Analyzes the project directory tree, detects files, extracts imports/dependencies (Go, Python, TypeScript, JavaScript, Astro, PHP, HTML, CSS, Bash, Lua), resolves relative paths, and builds an internal dependency graph stored in SQLite.
4. **Agent Orchestration (Protocol Rules):** Automatically injects agent guidelines into `AGENTS.md`, `.cursorrules`, or `.windsurfrules` files in the repository root to guide AI agents to query and write memory proactively.
5. **Dependency-Free Portability:** Compiled in pure Go without requiring CGO, thanks to `modernc.org/sqlite`. The compiled binary runs directly on macOS, Linux, and Windows.

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
        │             (.sv-memory/memories.json)                 │
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

- **Language:** **Go 1.22+** installed on your system.
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
- Imports shared memories from `.sv-memory/memories.json` if they exist.
- Injects agent protocol guidelines into `AGENTS.md`, `.cursorrules`, or `.windsurfrules`.

```bash
sv-memory init
```

### 2. `sv-memory mcp`

Starts the Model Context Protocol (MCP) server over standard input/output (`stdio`). This is the command used by AI clients to interact with the tool.

```bash
sv-memory mcp
```

### 3. `sv-memory graph rebuild`

Forces a full re-scan of the project directory tree and updates the code graph nodes along with their relationships.

```bash
sv-memory graph rebuild
```

### 4. `sv-memory sync`

Manually triggers synchronization. Imports new memories from the Git JSON file to SQLite, and exports all local SQLite memories of the project to `.sv-memory/memories.json`.

```bash
sv-memory sync
```

### 5. `sv-memory configure`

Launches an interactive setup wizard in the terminal to configure your editors and CLIs.
* **Phase 1 (Editors):** Select which editors to configure (`Cursor`, `VS Code`, `Zed`, `Windsurf`).
* **Phase 2 (CLIs):** Select which terminal tools to configure (`Claude Code`, `OpenCode`, `Codex`, `Antigravity CLI (agy)`).
* **Phase 3 (Application):** Generates a summary, requests confirmation, performs configuration injection safely for supported tools, and displays manual step-by-step instructions for the rest.

```bash
sv-memory configure
```

---

## 🧩 Model Context Protocol (MCP) Tools

Once connected, `sv-memory` exposes several tools to AI agents:

1. **`sv_mem_save`**: Saves architectural decisions, bugfixes, or development guides. Automatically triggers immediate export to `.sv-memory/memories.json`.
2. **`sv_mem_search`**: Performs Full-Text Search (FTS) queries on saved memories. Supports category filters.
3. **`sv_graph_query`**: Queries the dependency subgraph of a file, module, or package with a configurable search depth. Returns connected nodes and generates a **Mermaid** diagram.
4. **`sv_graph_sync`**: Updates and resynchronizes the structural dependency graph in SQLite.

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

3. **Upon Completing a Task (e.g., Fixing a Complex Bug):**
   The agent records the decision:
   > *Agent runs internally:*
   > `sv_mem_save(category="bugfix", what="Usage of modernc.org/sqlite", why="Avoid CGO dependency to enable clean cross-compilation", learned="Always use modernc.org/sqlite for portable SQLite databases in Go", where_path="internal/db/db.go")`
   > *Result:* The decision is saved in SQLite and immediately synchronized to `.sv-memory/memories.json` so your team receives it on their next `git pull`.

---

## 📄 License

This project is licensed under the MIT License. See the LICENSE file for details.
