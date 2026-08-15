import { tool, type Plugin } from "@opencode-ai/plugin"

/**
 * sv-memory native OpenCode plugin.
 *
 * Bridges sv-memory into OpenCode without requiring the agent to discover the
 * MCP tools manually:
 *
 *  - `sv_memory_context` — fetch a compact context pack (structural graph role
 *    + linked memories) for a file/package/symbol in one call, shelling out to
 *    the `sv-memory context <path>` CLI. Mirrors the sv_mem_context_pack MCP
 *    tool; bounded output saves tokens before reading source files.
 *
 * The session lifecycle (sv_mem_session_start / sv_mem_session_end) and the
 * full 29-tool surface are provided by the sv-memory MCP server, configured by
 * `sv-memory setup opencode`. Fail-open: the tool returns errors as output so
 * a missing binary or uninitialized project never crashes the agent.
 */
export const SvMemoryPlugin: Plugin = async ({ $ }) => {
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
  }
}

export default SvMemoryPlugin
