import { tool, type Plugin } from "@opencode-ai/plugin"
import { mkdtemp, writeFile, readFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { execFile } from "node:child_process"
import { promisify } from "node:util"

const execFileAsync = promisify(execFile)

/**
 * sv-memory native OpenCode plugin — graph-first mode (strict).
 *
 * Two capabilities:
 *
 * 1. `sv_memory_context` — compact context pack for a path (same as soft).
 *
 * 2. `tool.execute.before` hook — on the FIRST Read/Grep/Bash(grep) call
 *    per session, intercepts the call and redirects the agent to the
 *    sv-memory graph context instead of reading the raw file.
 *
 *    Mechanism: runs `sv-memory context <file>`, writes the output to a
 *    temp file, and rewrites `output.args.filePath` to point at it — so
 *    the agent reads graph context first. Subsequent calls proceed normally.
 *
 *    Fail-open: if the sv-memory binary is missing, the project is
 *    uninitialized, or context generation fails, the original filePath
 *    is left untouched and the read proceeds normally.
 *
 *    Opt-out: set SV_MEMORY_STRICT_DISABLE=1 to disable the redirect entirely.
 */
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
      "tool.execute.before": async (input, output) => {
        // Opt-out: SV_MEMORY_STRICT_DISABLE disables redirect entirely.
        if (process.env.SV_MEMORY_STRICT_DISABLE) return

        // Only intercept the first read/search call per session.
        if (redirected.has(input.sessionID)) return

        const toolName = input.tool
        const isReadTool =
          toolName === "read" || toolName === "grep" || toolName === "bash"

        if (!isReadTool) return

        // For read tool, extract the file path from args.
        // For grep/bash, skip the redirect (too complex to rewrite).
        if (toolName !== "read") return

        const filePath = output.args?.filePath as string | undefined
        if (!filePath || filePath.startsWith(".sv-memory/")) return

        redirected.add(input.sessionID)

        try {
          const out = await execFileAsync(
            "sv-memory",
            ["context", filePath, "--max-memories", "5"],
            { timeout: 5000 },
          )
          if (out.stdout && out.stdout.trim().length > 0) {
            // Write context to a temp file and redirect the read.
            const tmpDir = await mkdtemp(join(tmpdir(), "sv-nudge-"))
            const nudgePath = join(tmpDir, "context.md")
            await writeFile(nudgePath, out.stdout, "utf-8")
            output.args.filePath = nudgePath
          }
        } catch {
          // Fail-open: sv-memory unavailable or timed out — leave args untouched.
        }
      },
    },
  }
}

export default SvMemoryPlugin
