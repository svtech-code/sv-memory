# Agent Setup

Connect sv-memory to your AI coding assistant. The unified `sv-memory setup <agent>`
command wires everything in one shot — MCP server config, hooks/skills/plugins, protocol
injection (`AGENTS.md`), and MCP tool permissions — mirroring the one-shot integration
style of Engram's `engram setup`.

## Supported agents

| Agent          | Command                  | What gets installed                                                                 |
| :------------- | :----------------------- | :---------------------------------------------------------------------------------- |
| Claude Code    | `sv-memory setup claude-code` | MCP server config, `PreToolUse` + lifecycle hooks (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`), `AGENTS.md` protocol, 34-tool permission allow-list |
| OpenCode       | `sv-memory setup opencode`    | MCP server config, `SKILL.md` skill, native TypeScript plugin (`sv_memory_context` tool), `AGENTS.md` protocol |
| Cursor         | `sv-memory setup cursor`      | `.cursor/mcp.json` MCP config, `.cursorrules` protocol injection |
| Windsurf       | `sv-memory setup windsurf`    | `.windsurf/mcp_config.json` MCP config, `.windsurfrules` protocol injection |
| Antigravity CLI | `sv-memory setup antigravity` | MCP config, native skill (`.agents/skills/sv-memory/SKILL.md`), `PreToolUse` hooks (soft/strict), `AGENTS.md` protocol, 34-tool permission allow-list |
| Codex          | `sv-memory setup codex`       | MCP config in `~/.codex/config.toml`, hooks, `AGENTS.md` protocol |

## Quick start

```bash
cd /path/to/your-project
sv-memory init                # one-time project initialization
sv-memory setup claude-code   # wire one agent
sv-memory setup --all         # or wire every agent at once
sv-memory setup               # show per-agent install status
```

`sv-memory setup` without arguments is read-only: it prints the installation status of
every supported agent. `setup <agent>` is idempotent — re-running it refreshes the config
without duplicating entries. After installing, **restart your assistant** so it reloads the
MCP config and hooks.

### Options

- `--strict`: install strict hooks. On Antigravity CLI this blocks the first raw file read
  of each session so the agent must consult `sv_mem_search`/`sv_graph_query` first. On
  Claude Code strict mode is nudge-only and never blocks.
- `--all`: install for every supported agent.

## Updating sv-memory (post-update)

`sv-memory update` replaces the binary atomically. To refresh skills, hooks, protocol rules, and grant newly added MCP tool permissions across your existing projects, simply run `sv-memory init` inside each project:

```bash
sv-memory update                # 1. replace the binary
cd /path/to/your-project
sv-memory init                  # 2. auto-reconciles skills, hooks, AGENTS.md, and MCP permissions
```

Why step 2 is recommended:
- `sv-memory init` re-scans the current tool set and **grants any newly added MCP tools** to the static allow-lists (`~/.claude/settings.json`, `opencode.json`, `.cursor/mcp.json`, `.windsurf/mcp_config.json`, Antigravity `mcp_config.json`).
- It **re-injects the protocol** (`AGENTS.md` / `.cursorrules` / `.windsurfrules`) and refreshes skills (`.agents/skills/sv-memory/SKILL.md`, OpenCode `SKILL.md`), ensuring agent instructions match the latest binary features.
- It is **idempotent and non-destructive** — it detects configured assistants and refreshes only what is needed.

Then restart your assistant so it reloads the MCP config and new tools. Verify with `sv-memory version` and `sv-memory setup` (read-only status table).

## Per-agent details

### Claude Code

- **MCP config:** if the `claude` CLI is on your PATH, run the printed
  `claude mcp add sv-memory ...` command to register the server at user scope. Otherwise a
  project-local `.mcp.json` is written and Claude Code picks it up automatically.
- **Hooks:** `sv-memory setup claude-code` installs five hook scripts under
  `.claude/hooks/` and registers them in `.claude/settings.json`:
  - `PreToolUse` (matcher `Read|Glob|Grep`) — nudges the agent to query memory/graph before
    reading files. With the opt-in silent context injection
    (`sv-memory hooks install --platform claude-code --context-injection`) the first `Read`
    of each file also injects a compact graph+memory context pack as `additionalContext`.
  - `SessionStart` — reminds the agent to call `sv_mem_session_start`.
  - `SessionEnd` — reminds the agent to close the session with `sv_mem_session_end`.
  - `PreCompact` — fires right before context compaction and tells the agent to save a
    session summary first (context recovery).
  - `SubagentStop` — reminds the agent to persist durable findings from subagents.
- **Permissions:** the 34 sv-memory tools are added to the `~/.claude/settings.json`
  allow-list (`mcp__sv-memory__<tool>`) so the agent calls them without prompting.

### OpenCode

- **MCP config:** written to `~/.config/opencode/opencode.json` (merged, existing servers
  preserved).
- **Skill:** `SKILL.md` installed under `.opencode/skills/sv-memory/` so the agent can load
  the sv-memory workflow with the `skill` tool.
- **Native plugin:** `.opencode/plugin/sv-memory.ts` registers the `sv_memory_context` tool
  — a first-class way to fetch a context pack for a file/package/symbol by shelling out to
  `sv-memory context <path>`, without needing MCP approval.
- **Protocol:** sv-memory rules are injected into `AGENTS.md`.

### Cursor

- **MCP config:** `.cursor/mcp.json` is written at the project root (merged). Cursor reads
  it automatically.
- **Protocol:** sv-memory rules are injected into `.cursorrules`.

### Windsurf

- **MCP config:** `.windsurf/mcp_config.json` is written at the project root (merged).
- **Protocol:** sv-memory rules are injected into `.windsurfrules`.

### Antigravity CLI (agy)

- **MCP config:** written to the Antigravity `mcp_config.json` (merged).
- **Skill:** native skill installed under `.agents/skills/sv-memory/SKILL.md` with YAML
  frontmatter for progressive on-demand disclosure by the agent.
- **Hooks:** `.agents/hooks.json` + `.agents/hooks/sv-memory.sh`. Soft mode always allows
  the read and nudges via `AGENTS.md`; `--strict` blocks the first raw file read of each
  session. Strict is fail-open: it never deadlocks when sv-memory is missing or
  `SV_MEMORY_STRICT_DISABLE=1` is set.
- **Permissions:** the 34 sv-memory tools are added to the Antigravity settings allow-list
  (`mcp(sv-memory/<tool>)`).

### Codex

- **MCP config:** the `[mcp_servers.sv-memory]` block is written into
  `~/.codex/config.toml` (or created). Codex approves tool calls interactively, so no
  static allow-list is written.
- **Hooks:** `.codex/hooks.json` with a no-op script — `AGENTS.md` is the always-on nudge
  on this platform (Codex Desktop rejects `additionalContext` on PreToolUse).

## Surviving compaction

The sv-memory protocol (injected into `AGENTS.md` / `.cursorrules` / `.windsurfrules`)
instructs the agent, after a context compaction or reset, to:

1. call `sv_mem_session_summary` with the compacted summary content,
2. call `sv_mem_context` to recover the last session state,
3. only then continue working.

This recovery flow is what makes sv-memory survive context compaction — the system-prompt
protocol block (not a hook) guarantees the agent remembers to restore context even when the
working window is lost.

## Status & teardown

```bash
sv-memory setup                 # per-agent install status
sv-memory hooks status          # hook + context-injection status
sv-memory hooks uninstall       # remove hooks/skills/plugins
sv-memory permissions revoke    # remove the MCP tool allow-list
```

See [spect.md](spect.md) and the [README](../README.md) for the full CLI and MCP tool
reference.
