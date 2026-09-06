import { tool, type Plugin } from "@opencode-ai/plugin"
import { mkdtemp, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { execFile } from "node:child_process"
import { promisify } from "node:util"

const execFileAsync = promisify(execFile)
const SV = "sv-memory"

// ---------------------------------------------------------------------------
// Per-session automation state. Everything is fail-open: any CLI error or
// timeout leaves the request untouched, so a missing binary or an
// uninitialized project never breaks a session. The goal is that the sv-memory
// behavior does not depend on which model runs — deterministic hooks do the
// session lifecycle, prompt capture, Auto-Boot injection, model-switch
// re-orientation, and corrective spec nudging regardless of the model.
// ---------------------------------------------------------------------------
type SessionState = {
  booted: boolean // Auto-Boot bundle already injected for this session
  model: string // last modelID seen for this session
  sessionId: string // sv-memory session id (when auto-managed)
  goal: string // first user-message text, used to rank the Auto-Boot bundle
  nudged: boolean // corrective spec nudge already emitted
  pendingNudge: boolean // model edited code without consulting the spec flow
  specSeen: boolean // model called sv_spec_list / sv_propose_spec / sv_spec_get
}

const sessions = new Map<string, SessionState>()

function stateFor(sessionID: string): SessionState {
  let s = sessions.get(sessionID)
  if (!s) {
    s = {
      booted: false,
      model: "",
      sessionId: "",
      goal: "",
      nudged: false,
      pendingNudge: false,
      specSeen: false,
    }
    sessions.set(sessionID, s)
  }
  return s
}

async function runCLI(args: string[], timeoutMs = 8000): Promise<string> {
  try {
    const out = await execFileAsync(SV, args, { timeout: timeoutMs, maxBuffer: 4 * 1024 * 1024 })
    return (out.stdout || "").toString().trim()
  } catch {
    return ""
  }
}

function extractText(parts: Array<{ type?: string; text?: string }>): string {
  return (parts || [])
    .filter((p) => p && p.type === "text" && typeof p.text === "string")
    .map((p) => p.text)
    .join("\n")
    .trim()
}

// ensureSession auto-starts a session if none is active and returns the
// Auto-Boot Context Bundle (plus graph hubs) printed by `sv-memory session
// start`, or "" if a session was already active.
async function ensureSession(s: SessionState): Promise<string> {
  const active = await runCLI(["session", "active"], 5000)
  if (active && active !== "none") {
    s.sessionId = active
    return ""
  }
  const args = ["session", "start"]
  if (s.goal) args.push("--goal", s.goal)
  const out = await runCLI(args, 10000)
  const m = out.match(/Session started \(ID: ([a-zA-Z0-9]+)\)/)
  if (m) s.sessionId = m[1]
  return out
}

// prependNote injects a synthetic user message ahead of the messages sent to
// the model. synthetic: true marks it as plugin-generated, not a user message.
function prependNote(
  messages: Array<{ info: MessageInfo; parts: unknown[] }>,
  sessionID: string,
  text: string,
) {
  const now = Date.now()
  const id = `svm-${now}-${Math.random().toString(36).slice(2, 8)}`
  messages.unshift({
    info: {
      id,
      sessionID,
      role: "user",
      time: { created: now },
      agent: "build",
      model: { providerID: "", modelID: "" },
    },
    parts: [
      {
        id: `${id}-part`,
        sessionID,
        messageID: id,
        type: "text",
        text,
        synthetic: true,
      },
    ],
  })
}

type ToolInput = { tool: string; sessionID: string; callID: string; args?: unknown }
type MessageInfo = { sessionID?: string; role?: string; modelID?: string; [k: string]: unknown }
type MessageEntry = { info: MessageInfo; parts: unknown[] }
type TextPartLike = { type?: string; text?: string }

export const SvMemoryPlugin: Plugin = async ({ $ }) => {
  // Track which sessions have already had their first-read redirect.
  const redirected = new Set<string>()

  return {
    tool: {
      sv_memory_context: tool({
        description:
          "Get a compact context pack (structural graph role + linked memories) for a file, package, or symbol. Use it before reading source files to understand a module and recall past decisions with minimal tokens.",
        args: {
          path: tool.schema
            .string()
            .describe("File path, package name, or symbol to inspect"),
          maxMemories: tool.schema
            .number()
            .optional()
            .describe("Maximum linked memories to include (default 5)"),
        },
        async execute(args, context) {
          const maxMemories = args.maxMemories ?? 5
          try {
            const out = await $`sv-memory context ${args.path} --max-memories ${maxMemories}`
              .cwd(context.directory)
              .quiet()
              .nothrow()
            if (out.exitCode !== 0) {
              return `sv-memory context unavailable (exit ${out.exitCode}): ${out.stderr
                .toString()
                .trim() || "binary not installed or project not initialized"}`
            }
            return out.stdout.toString()
          } catch (err) {
            return `sv-memory context unavailable: ${(err as Error).message}`
          }
        },
      }),
    },

    hooks: {
      // 1) Graph-first: redirect the first Read of the session to a compact
      //    sv-memory context pack instead of the raw file. Fail-open.
      "tool.execute.before": async (input: ToolInput, output: { args: any }) => {
        if (process.env.SV_MEMORY_STRICT_DISABLE) return

        if (redirected.has(input.sessionID)) return

        if (input.tool !== "read") return

        const filePath = output.args?.filePath as string | undefined
        if (!filePath || filePath.startsWith(".sv-memory/")) return

        redirected.add(input.sessionID)

        try {
          const out = await execFileAsync(SV, ["context", filePath, "--max-memories", "5"], {
            timeout: 5000,
          })
          if (out.stdout && out.stdout.trim().length > 0) {
            const tmpDir = await mkdtemp(join(tmpdir(), "sv-nudge-"))
            const nudgePath = join(tmpDir, "context.md")
            await writeFile(nudgePath, out.stdout, "utf-8")
            output.args.filePath = nudgePath
          }
        } catch {
          // Fail-open: sv-memory unavailable or timed out — leave args untouched.
        }
      },

      // 2) Deterministic lifecycle + prompt capture: on every user message,
      //    persist the user's intent (sv_mem_capture_prompt) and remember the
      //    first message as the session goal. Runs for any model.
      "chat.message": async (
        input: { sessionID: string; model?: { providerID: string; modelID: string } },
        output: { parts: TextPartLike[] },
      ) => {
        const s = stateFor(input.sessionID)
        const text = extractText(output.parts)
        if (!s.goal && text) s.goal = text.slice(0, 120)
        if (text) await runCLI(["capture", "prompt", text], 5000)
      },

      // 3) Auto-Boot injection, model-switch re-orientation, and corrective
      //    spec nudge. Runs on every LLM request; each behavior is gated so it
      //    fires once and adds no recurring token cost.
      "experimental.chat.messages.transform": async (
        _input: {},
        output: { messages: MessageEntry[] },
      ) => {
        const sessionID = output.messages?.[0]?.info?.sessionID || ""
        if (!sessionID) return

        const s = stateFor(sessionID)
        const hasUserTurn = (output.messages || []).some(
          (m: MessageEntry) => m.info?.role === "user",
        )
        const lastModel =
          (output.messages || [])
            .filter((m: MessageEntry) => m.info?.role === "assistant")
            .map((m) => m.info?.modelID)
            .filter(Boolean)
            .pop() || ""

        // Auto-Boot: inject once, only into the first request that carries a
        // real user turn (skips title/summary/internal requests).
        if (!s.booted) {
          if (!hasUserTurn) return
          s.booted = true
          const bundle = await ensureSession(s)
          const header =
            `[sv-memory] Session ${s.sessionId || "(active)"} is auto-managed by the sv-memory plugin — ` +
            `do not call sv_mem_session_start. Use sv_mem_session_end when done.\n\n` +
            bundle
          prependNote(output.messages, sessionID, header)
          return
        }

        // Corrective spec nudge: the model edited code without consulting the
        // spec flow (sv_spec_list / sv_propose_spec). Emitted once per session.
        if (s.pendingNudge && !s.nudged && hasUserTurn) {
          s.nudged = true
          s.pendingNudge = false
          prependNote(
            output.messages,
            sessionID,
            "[sv-memory] You edited code without consulting the spec flow. Per AGENTS.md, run 'sv_spec_list' (and 'sv_propose_spec' for behavior/architecture changes) before continuing.",
          )
          return
        }

        // Model switch: re-orient the new model with a compact header (session
        // + active specs), so behavior does not degrade when the user changes
        // models mid-session.
        if (lastModel && s.model && lastModel !== s.model) {
          s.model = lastModel
          const specs = (await runCLI(["specs", "list"], 5000)).slice(0, 500)
          prependNote(
            output.messages,
            sessionID,
            `[sv-memory] Model switched to ${lastModel}. Session ${s.sessionId || "active"}.\nActive spec changes:\n${specs || "none"}`,
          )
        }
      },

      // 4) Pre-compaction summary reminder (parity with Claude Code PreCompact):
      //    persist the session summary before opencode compacts the context.
      "experimental.session.compacting": async (
        _input: { sessionID: string },
        output: { context: string[]; prompt?: string },
      ) => {
        output.context.push(
          "Before compacting, persist the session summary: run `sv-memory session summary <session-id> --accomplished \"...\"` (or sv_mem_session_summary). It is recoverable later via sv_mem_context.",
        )
      },

      // 5) Track edits/spec-tool usage to arm the corrective spec nudge.
      "tool.execute.after": async (input: ToolInput) => {
        const s = stateFor(input.sessionID)
        if (input.tool === "edit" || input.tool === "write" || input.tool === "patch") {
          if (!s.specSeen && !s.pendingNudge) s.pendingNudge = true
        }
        if (
          input.tool === "sv-memory_sv_spec_list" ||
          input.tool === "sv-memory_sv_propose_spec" ||
          input.tool === "sv-memory_sv_spec_get"
        ) {
          s.specSeen = true
          s.pendingNudge = false
        }
      },
    },
  }
}

export default SvMemoryPlugin