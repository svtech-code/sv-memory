# 📖 Getting Started Guide: Installing and Initial Workflow for sv-memory

> **Language:** English | [Español](getting_started_guide_ES.md)

**sv-memory** is a persistent architectural memory and code dependency graph system designed to eliminate context amnesia in AI agents (Cursor, Claude Code, Antigravity CLI, Windsurf, etc.).

This guide explains step by step how to install **sv-memory**, configure it on your system, and start using it in your development projects.

---

## 🎯 Why Use sv-memory?

When working with AI assistants in medium or large repositories, three recurring problems arise:

1. **Session Amnesia:** The AI forgets architectural decisions made in previous sessions.
2. **Token Waste:** The AI needs to read dozens of source files over and over to understand the code structure.
3. **Lack of Continuity:** Technical decisions stay trapped in individual chats instead of being shared with the team.

**sv-memory** solves this by combining **persistent memories indexed with SQLite FTS5 BM25** and a **structural code dependency graph** exposed through 34 MCP (_Model Context Protocol_) tools.

---

## 🚀 5-Step Startup Flow

```mermaid
flowchart TD
    P1[Step 1: Global Binary Installation] --> P2[Step 2: Configuring Editors and CLIs with 'sv-memory configure']
    P2 --> P3[Step 3: Project Initialization with 'sv-memory init']
    P3 --> P4[Step 4: Installing Hooks with 'sv-memory hooks install']
    P4 --> P5[Step 5: Restarting the Agent and Verifying]
    P5 --> D[Daily Workflow with AI & TUI]
```

---

### Step 1: Global Binary Installation (Global Setup)

The `sv-memory` executable is a single Go binary, fully self-contained and with no external dependencies (it uses SQLite embedded in pure Go).

#### Installing with a prebuilt binary (recommended)

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.sh | bash

# Windows (PowerShell)
iwr -useb https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.ps1 | iex
```

#### Building from source

```bash
# 1. Clone the repository
git clone https://github.com/svtech-code/sv-memory.git
cd sv-memory

# 2. Build the executable binary
go build -o sv-memory ./cmd/sv-memory

# 3. Move the binary to a global PATH location (no sudo needed)
mkdir -p ~/.local/bin
mv sv-memory ~/.local/bin/

# 4. Verify the installation
sv-memory --help
```

> **Why this step?**
> By placing the executable in `~/.local/bin/` (a standard user PATH location), any terminal tool or code editor on your system can invoke `sv-memory mcp` or run diagnostic commands no matter which directory you are in. The automatic installer (`install.sh` / `install.ps1`) does this step for you without requiring `sudo`.

#### Updating sv-memory

```bash
sv-memory update
```

The command looks for the latest release published on GitHub, compares it with your current version
(`sv-memory version`), and if there is a newer one:

1. It shows you both versions and **asks for confirmation** before doing anything.
2. It downloads the correct binary for your operating system and architecture.
3. It **verifies its SHA-256 checksum** against the one published in the release (protection against corrupt or tampered downloads).
4. It replaces the binary atomically (on Windows it tells you the manual command, because it cannot overwrite a running `.exe`).

> Your memories (SQLite DB in `~/.config/sv-memory/`) and your editor configuration are not affected by the update only the binary is replaced.

---

### Step 2: Interactive Configuration of Editors and CLIs (`sv-memory configure`)

For your editor or terminal assistant (Cursor, Claude Code, Windsurf, Antigravity CLI, OpenCode, etc.) to recognize the `sv-memory` MCP server, run the interactive wizard:

```bash
sv-memory configure
```

#### What does this command do?

The wizard guides you through interactive phases in the terminal, navigable with the `↑/↓` arrows, multiple selection with `SPACE`, `Enter` to advance, `Esc` to go back, and `Ctrl+C` to exit:

1. **Phase 1 (GUI Editors):** Lets you select editors such as **Cursor**, **VS Code**, **Zed**, or **Windsurf**. It automatically registers the MCP server in their user configuration files (e.g. `claude_desktop_config.json` or Cursor settings).
2. **Phase 2 (Terminal Assistants):** Lets you select CLI clients such as **Claude Code**, **Antigravity CLI (agy)**, or **OpenCode**.
3. **Phase 3 (Confirmation and application):** Shows the summary of selected tools and applies the automatic or manual configurations.
4. **Phase 4 (MCP Permissions):** Lists the **34 sv-memory MCP tools** for you to select which ones to authorize (press `a` to select all and `x` to select none). It grants the permissions on the configured platforms that use a static allow-list (Antigravity CLI, Claude Code).

> **Why this step?**
> It prevents you from having to manually edit complex JSON configuration files. With a couple of keystrokes in the terminal, all your editors get linked to the `sv-memory` MCP server and the tool permissions are granted with full transparency.

---

### Step 3: Initialization inside Your Project (`sv-memory init`)

Navigate to the root of the repository or code project where you want to start working and initialize `sv-memory`:

```bash
cd /path/to/your-project
sv-memory init
```

#### What happens internally when running `sv-memory init`?

1. **Calculates the Project ID:** Derives a unique identifier based on the Git repository hash.
2. **SQLite Registration:** Registers the project in the local SQLite database (`~/.config/sv-memory/storage.db`).
3. **Code Graph Scan:** Analyzes the file tree and builds the initial dependency graph (imports, god nodes, Leiden communities).
4. **Git Sync:** Imports previous memories shared by your team if the `.sv-memory/chunks/` folder exists.
5. **Protocol Rules Injection (`AGENTS.md`):** Creates or updates the `AGENTS.md` file at the project root. This file contains the instructions so any AI agent automatically knows **when to consult**, **when to save**, and **when to compact** information autonomously.

---

### Step 4: Installing PreToolUse Hooks (`sv-memory hooks install`)

Run this step **inside your project root** (hooks are installed in the project's `.agents/`, not globally):

```bash
cd /path/to/your-project
sv-memory hooks install --platform antigravity
```

#### What does this command do?

It creates `.agents/hooks.json` and `.agents/hooks/sv-memory.sh` so the agent intercepts file reads (`view_file`, `grep_search`, `list_dir`) and queries the project memory before reading code blindly.

There are two modes:

| Mode               | Command                                                   | Behavior                                                                                                                  |
| :----------------- | :-------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------ |
| **Soft** (default) | `sv-memory hooks install --platform antigravity`          | Blocks nothing. The real "nudge" is done by the `AGENTS.md` injected in Step 3, which forces the agent to query memory.   |
| **Strict**         | `sv-memory hooks install --platform antigravity --strict` | Blocks the **first** file read of each session, forcing the agent to run `sv_mem_search`/`sv_graph_query` before reading. |

> You can switch modes by re-running the command with or without `--strict`.

> **Degradation & fail-open:** the hook scripts never call the sv-memory server they only inspect local files and env vars. If sv-memory is not initialized (no `.sv-memory/`), the binary is missing from PATH, or `SV_MEMORY_STRICT_DISABLE=1` is set, strict mode **allows** the read instead of blocking, so a missing/unconfigured sv-memory never deadlocks the agent. Note that strict _blocking_ is only implemented on Antigravity CLI; on Claude Code strict mode is nudge-only (it never blocks).

> **Silent context injection (opt-in, default off):** Claude Code hooks can auto-inject a compact graph+memory context pack (`sv-memory context <file>` output) as `additionalContext` on the first `Read` of each file. Enable it with `sv-memory hooks install --platform claude-code --context-injection`, which creates a `.sv-memory/context-injection-enabled` marker. Output is cached per file for the session and time-bounded (2s); the hook always exits 0, so a missing binary or `.sv-memory` never breaks a tool call. Disable with `sv-memory hooks uninstall --context-injection`. Antigravity, Codex, and OpenCode do not support `additionalContext` injection and keep the nudge/skill mechanism.

> **Per project:** Repeat this command in every repository where you work with AI. The supported platforms are `claude-code`, `codex`, `antigravity`, and `opencode` (omit `--platform` to install it on all of them).

#### One-shot integration: `sv-memory setup <agent>`

For a fully wired agent (MCP config + hooks/skills/plugins + protocol injection +
tool permissions) in one command, use `sv-memory setup`:

```bash
cd /path/to/your-project
sv-memory setup claude-code   # Claude Code (MCP + PreToolUse & lifecycle hooks + allow-list)
sv-memory setup opencode      # OpenCode (MCP + SKILL.md + native TS plugin)
sv-memory setup cursor        # Cursor (.cursor/mcp.json + .cursorrules)
sv-memory setup windsurf      # Windsurf (.windsurf/mcp_config.json + .windsurfrules)
sv-memory setup antigravity   # Antigravity CLI (MCP + hooks + allow-list)
sv-memory setup codex         # Codex (MCP config.toml + hooks)
sv-memory setup --all         # every agent
sv-memory setup               # read-only per-agent status
```

For Claude Code, `setup claude-code` additionally installs the lifecycle hooks
(`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`) so the agent is nudged to
start/end sessions, save a summary right before compaction, and persist subagent findings.
See [AGENT-SETUP.md](AGENT-SETUP.md) for the full per-agent guide.

---

### Step 5: Restarting the Agent and Verifying

Close and reopen your AI assistant so it loads the MCP, permissions, and freshly configured hooks. Then verify the status:

```bash
cd /path/to/your-project
sv-memory permissions status --platform antigravity   # Granted: 34 / 34
sv-memory hooks status                                # antigravity: ✅ installed
sv-memory diagnose                                    # 17 pass, 0 failures
```

---

### Step 6: Daily Workflow (AI Agents and Human Use)

Once the previous steps are complete, you are ready to work.

#### 🤖 1. AI Agent Autonomy (Transparent to you)

When you open your editor (Cursor, Windsurf, Claude Code, etc.) and send any message or task to the AI:

- **Session Startup (Auto-Boot Context Bundle):** The agent transparently runs `sv_mem_session_start`, immediately receiving the previous session's goals, the top 3 code hubs, and the latest postmortems / Q&A.
- **Smart Search (FTS5 BM25 + Path Scoping + Graph Boost):** When you ask it about a module or bug, the AI calls `sv_mem_search` with path filtering to find past decisions without spending thousands of tokens. With `graph_boost` (default on), a module search expands to the whole graph community in one call, annotating community rows with a `[graph]` marker.
- **Graph Query (<1ms):** If the AI needs to know which files import a module before refactoring, it queries `sv_graph_query`, getting an instant answer thanks to the in-RAM LRU cache.
- **Context Pack (Graph + Memory in one call):** Before touching a file, the AI calls `sv_mem_context_pack` (or the `sv-memory context <path>` CLI) to get the node's structural role (fan-in/fan-out, community) plus the decisions, standards, and bugfixes linked to that path — one bounded call instead of several searches, with each `why` truncated.
- **Silent Context Injection (opt-in):** With Claude Code hooks + `--context-injection`, the first Read of each file automatically injects its context pack as `additionalContext` — relevant context at the exact moment, with no search round-trip.
- **Automatic Saving:** When solving a problem or defining a standard, the AI runs `sv_mem_save`, recording the learning in SQLite and syncing it to `.sv-memory/chunks/` for your Git version control.
- **Token Ledger:** `sv_mem_stats` reports the estimated tokens injected into the session since `sv_mem_session_start` alongside the `max_response_tokens` budget, so the agent knows when to compact.

#### 📋 2. Spec-Driven Decisions (Governance)

For anything beyond a trivial fix, the agent runs the native **propose → validate → commit** cycle before writing code, optionally carrying OpenSpec-style delta requirements:

- **Consult:** `sv_mem_context_pack(path="<file|pkg>", include_changes="true")` returns the node's role, linked decisions/standards, active changes, and the **capabilities implemented at that path** (bounded requirement summary) in one call.
- **Propose:** `sv_propose_spec(slug="...", title=..., what=..., where_path=..., requirements=..., capability_path=...)` registers the change, runs a pre-flight check (a pinned overlapping rule → **BLOCK**, an ordinary overlap → **WARN**, otherwise **PASS**), and stores the delta requirements targeting a single capability (defaults to the slug).
- **Validate:** `sv_validate_decision(change_id=...)` re-checks the proposal (PASS/WARN/BLOCK) and validates the deltas — RFC 2119 keyword presence and MODIFIED scenario drops vs the current capability state.
- **Commit:** `sv_commit_spec(change_id=...)` saves the durable `decision`/`standard` memory, merges the deltas into the capability state (`.sv-memory/specs/capabilities/` + graph `spec` nodes), and stamps the change `applied`. A BLOCK or a merge conflict rejects the commit.
- **Mirror:** every change and capability is projected to `.sv-memory/specs/` (git-synced). Humans can edit the Markdown; `sv-memory specs import <slug>` reconciles the edits back into the authoritative store. `sv-memory specs capabilities` lists the current requirement state.

**Delta requirements format (OpenSpec):**

```markdown
## ADDED Requirements

### Requirement: Theme selection
The app SHALL let users switch between light and dark themes,
defaulting to the system preference.

#### Scenario: User toggles dark mode
- **WHEN** the user clicks the theme toggle
- **THEN** the app switches to dark mode and persists the choice
```

Use `## MODIFIED Requirements` to replace a whole requirement block (unlisted scenarios are dropped), `## REMOVED Requirements` to deprecate one, and `## RENAMED Requirements` (`- **FROM:**` / `- **TO:**`) to rename a header.

#### 👤 3. Human Exploration and Inspection in the Terminal (`sv-memory tui`)

As a developer, you can interactively inspect the state of knowledge and the health of your project by running:

```bash
sv-memory tui
```

From the TUI interface you can:

- **[1] List recent memories** categorized by category (`architecture`, `bugfix`, `decision`, etc.).
- **[2] Search memory** with the FTS5 BM25 engine.
- **[3] Inspect full details** of a decision by its ID or Topic Key.
- **[4] Diagnose graph health** (broken links, orphan nodes).
- **[5] Export to an Obsidian Vault** (linked `.md` notes).
- **[6] Export Cypher scripts for Neo4j / FalkorDB**.

---

## 🛠️ Quick Reference Commands

| Command                        | Description                                                      | When to use it?                                              |
| :----------------------------- | :--------------------------------------------------------------- | :----------------------------------------------------------- |
| `sv-memory configure`          | Interactive MCP setup wizard (includes Phase 4 permissions)      | On first install or when adding a new editor                 |
| `sv-memory init`               | Initializes the current project and creates `AGENTS.md`          | When starting work on a new repository                       |
| `sv-memory hooks install`      | Installs PreToolUse hooks to query memory before reading files   | When configuring Claude Code, Antigravity CLI, or OpenCode   |
| `sv-memory permissions grant`  | Grants MCP tools in the agent's allow-list (`--all` or `--tool`) | When the agent asks for permission on every MCP call         |
| `sv-memory permissions status` | Shows granted/missing MCP permissions per platform               | To audit the permission state of agents                      |
| `sv-memory permissions revoke` | Removes sv-memory MCP permissions while keeping the rest         | If you want to remove an agent's access                      |
| `sv-memory tui`                | Graphical terminal interface for querying memories               | When you want to explore past decisions interactively        |
| `sv-memory sync`               | Syncs SQLite with Git files `.sv-memory/chunks/`                 | Before running `git commit` or after `git pull`              |
| `sv-memory specs export/list/import/archive/capabilities` | Manages the spec mirror and capability state under `.sv-memory/specs/` | To review proposals, reconcile human edits, or list capabilities |
| `sv-memory diagnose`           | Health check of the system, permissions, and DB                  | If you experience any connection issue with the AI           |
| `sv-memory graph viz`          | Generates an HTML visualization of the code graph                | To visually audit your software architecture                 |
| `sv-memory obsidian-export`    | Exports memories as linked notes for Obsidian                    | To integrate technical knowledge into your personal Obsidian |

---

## 📌 Recommended Best Practices for Teams

1. **Include `.sv-memory/chunks/` in Git:** Allows the whole team to share architectural decisions. Distinct memory IDs never conflict on merge; if two agents edit the _same_ memory, resolve the resulting `{id}.json` conflict markers and re-run `sv-memory sync`.
2. **Review commits before pushing:** `sv-memory` updates memory JSONs locally, but it never runs `git commit` or `git push` automatically.
3. **Run `sv_mem_compact` periodically:** If you notice a topic has accumulated many revisions, the AI or you can run compaction to summarize the history into a clean synthesis.
4. **Keep secrets out of the graph:** `.env`, keys, and credentials are never indexed, and memory text is redacted on save/import. Add `SECRETS.md` and similar files to `.sv-memoryignore` so they are not indexed either.
