# Configuración de Agentes

Conecta sv-memory a tu asistente de IA. El comando unificado `sv-memory setup <agente>`
configura todo en un solo paso — configuración del servidor MCP, hooks/skills/plugins,
inyección de protocolo (`AGENTS.md`) y permisos de herramientas MCP — replicando el estilo
de integración de un solo comando de Engram (`engram setup`).

## Agentes soportados

| Agente          | Comando                      | Qué se instala                                                                                   |
| :-------------- | :--------------------------- | :----------------------------------------------------------------------------------------------- |
| Claude Code     | `sv-memory setup claude-code` | Config MCP, hooks `PreToolUse` + ciclo de vida (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`), protocolo en `AGENTS.md`, allow-list de las 34 herramientas |
| OpenCode        | `sv-memory setup opencode`    | Config MCP, skill `SKILL.md`, plugin nativo TypeScript (tool `sv_memory_context`), protocolo en `AGENTS.md` |
| Cursor          | `sv-memory setup cursor`      | Config MCP `.cursor/mcp.json`, inyección de protocolo en `.cursorrules` |
| Windsurf        | `sv-memory setup windsurf`    | Config MCP `.windsurf/mcp_config.json`, inyección de protocolo en `.windsurfrules` |
| Antigravity CLI | `sv-memory setup antigravity` | Config MCP, skill nativa (`.agents/skills/sv-memory/SKILL.md`), hooks `PreToolUse` (soft/strict), protocolo en `AGENTS.md`, allow-list de las 34 herramientas |
| Codex           | `sv-memory setup codex`       | Config MCP en `~/.codex/config.toml`, hooks, protocolo en `AGENTS.md` |
| Git             | `sv-memory hooks install --platform git` | Hook `.git/hooks/post-commit` para captura pasiva automática de commits |

## Inicio rápido

```bash
cd /ruta/a/tu-proyecto
sv-memory init                # inicialización interactiva (pregunta asistentes, instala hook post-commit de Git)
sv-memory init --agent antigravity # inicializa y configura un agente específico directamente
sv-memory init --agents claude-code,cursor # inicializa y configura múltiples agentes
sv-memory init --all          # inicializa y configura todos los agentes sin preguntar
sv-memory setup claude-code   # configura un agente posteriormente
sv-memory setup               # muestra el estado de instalación por asistente y Git
sv-memory hooks status        # muestra el estado de hooks PreToolUse, skills y MCP
```

`sv-memory setup` sin argumentos es de solo lectura: imprime el estado de instalación de
cada agente soportado y Git. `setup <agente>` es idempotente — repetirlo refresca la config sin
duplicar entradas. Después de instalar, **reinicia tu asistente** para que cargue la config
MCP y los hooks.

### Flags de Init

- `--agent <nombre>`: configura únicamente el asistente especificado durante init (ej. `antigravity`, `claude-code`).
- `--agents <a,b,c>`: lista separada por comas de asistentes a configurar durante init.
- `--all`: configura todos los asistentes soportados sin menú interactivo.
- `--skip-setup`: inicializa base de datos, reglas y hook de Git, omitiendo la configuración de asistentes.
- `--strict`: instala hooks estrictos (bloquea la primera lectura cruda en Antigravity).

## Actualizar sv-memory (post-actualización)

`sv-memory update` reemplaza el binario de forma atómica. Para refrescar skills, hooks, reglas de protocolo y otorgar permisos a nuevas herramientas MCP en tus proyectos existentes, simplemente ejecuta `sv-memory init` dentro de cada proyecto:

```bash
sv-memory update                # 1. reemplaza el binario
cd /ruta/a/tu-proyecto
sv-memory init                  # 2. auto-reconcilia skills, hooks, AGENTS.md y permisos MCP
```

Por qué el paso 2 es recomendado:
- `sv-memory init` re-escanea las herramientas actuales y **concede cualquier nueva herramienta MCP** en las listas de permisos (`~/.claude/settings.json`, `opencode.json`, `.cursor/mcp.json`, `.windsurf/mcp_config.json`, `mcp_config.json` de Antigravity).
- **Re-inyecta el protocolo** (`AGENTS.md` / `.cursorrules` / `.windsurfrules`) y refresca las skills (`.agents/skills/sv-memory/SKILL.md`, `SKILL.md` de OpenCode), asegurando que las instrucciones del agente coincidan con la versión actualizada.
- Es **idempotente y no destructivo** — detecta los asistentes configurados y actualiza solo lo necesario.

Luego reinicia tu asistente para que recargue la configuración MCP y las nuevas herramientas. Verifica con `sv-memory version` y `sv-memory setup` (tabla de estado de solo lectura).

## Detalles por agente

### Claude Code

- **Config MCP:** si el CLI `claude` está en tu PATH, ejecuta el comando impreso
  `claude mcp add sv-memory ...` para registrar el servidor en el ámbito de usuario. En caso
  contrario se escribe un `.mcp.json` local del proyecto que Claude Code detecta solo.
- **Hooks:** `sv-memory setup claude-code` instala cinco scripts bajo `.claude/hooks/` y los
  registra en `.claude/settings.json`:
  - `PreToolUse` (matcher `Read|Glob|Grep`) — sugiere al agente consultar memoria/grafo antes
    de leer archivos. Con la inyección silenciosa de contexto opt-in
    (`sv-memory hooks install --platform claude-code --context-injection`), la primera `Read`
    de cada archivo inyecta además un context pack compacto grafo+memoria como
    `additionalContext`.
  - `SessionStart` — recuerda al agente llamar `sv_mem_session_start`.
  - `SessionEnd` — recuerda cerrar la sesión con `sv_mem_session_end`.
  - `PreCompact` — se dispara justo antes de la compactación y le pide al agente guardar un
    resumen de sesión primero (recuperación de contexto).
  - `SubagentStop` — recuerda persistir hallazgos duraderos de los subagentes.
- **Permisos:** las 34 herramientas sv-memory se añaden al allow-list de
  `~/.claude/settings.json` (`mcp__sv-memory__<tool>`) para que el agente las llame sin
  pedir aprobación.

### OpenCode

- **Config MCP:** se escribe en `~/.config/opencode/opencode.json` (fusionado, preservando
  servidores existentes).
- **Skill:** `SKILL.md` se instala en `.opencode/skills/sv-memory/` para que el agente cargue
  el flujo sv-memory con la herramienta `skill`.
- **Plugin nativo:** `.opencode/plugin/sv-memory.ts` registra el tool `sv_memory_context` —
  una forma de primera clase de obtener un context pack para un archivo/paquete/símbolo
  invocando `sv-memory context <ruta>`, sin necesitar aprobación MCP.
- **Protocolo:** las reglas sv-memory se inyectan en `AGENTS.md`.

### Cursor

- **Config MCP:** se escribe `.cursor/mcp.json` en la raíz del proyecto (fusionado). Cursor
  lo lee automáticamente.
- **Protocolo:** las reglas sv-memory se inyectan en `.cursorrules`.

### Windsurf

- **Config MCP:** se escribe `.windsurf/mcp_config.json` en la raíz del proyecto (fusionado).
- **Protocolo:** las reglas sv-memory se inyectan en `.windsurfrules`.

### Antigravity CLI (agy)

- **Config MCP:** se escribe en el `mcp_config.json` de Antigravity (fusionado).
- **Skill:** skill nativa instalada bajo `.agents/skills/sv-memory/SKILL.md` con frontmatter YAML
  para descubrimiento progresivo bajo demanda por el agente.
- **Hooks:** `.agents/hooks.json` + `.agents/hooks/sv-memory.sh`. El modo soft siempre
  permite la lectura y sugiere vía `AGENTS.md`; `--strict` bloquea la primera lectura cruda
  de archivo de cada sesión. El modo estricto es fail-open: nunca bloquea el agente cuando
  sv-memory falta o `SV_MEMORY_STRICT_DISABLE=1` está definido.
- **Permisos:** las 34 herramientas sv-memory se añaden al allow-list de Antigravity
  (`mcp(sv-memory/<tool>)`).

### Codex

- **Config MCP:** se escribe el bloque `[mcp_servers.sv-memory]` en `~/.codex/config.toml`
  (o se crea). Codex aprueba las llamadas de forma interactiva, por lo que no se escribe
  ningún allow-list estático.
- **Hooks:** `.codex/hooks.json` con un script no-op — `AGENTS.md` es el mecanismo siempre
  activo en esta plataforma (Codex Desktop rechaza `additionalContext` en PreToolUse).

## Sobrevivir a la compactación

El protocolo sv-memory (inyectado en `AGENTS.md` / `.cursorrules` / `.windsurfrules`)
indica al agente que, tras una compactación de contexto o reset:

1. llame `sv_mem_session_summary` con el contenido del resumen compactado,
2. llame `sv_mem_context` para recuperar el estado de la última sesión,
3. solo entonces continúe trabajando.

Este flujo de recuperación es lo que hace que sv-memory sobreviva a la compactación — el
bloque de protocolo en el system prompt (no un hook) garantiza que el agente recuerde
restaurar el contexto aunque se pierda la ventana de trabajo.

## Estado y desinstalación

```bash
sv-memory setup                 # estado de instalación por agente
sv-memory hooks status          # estado de hooks + inyección de contexto
sv-memory hooks uninstall       # elimina hooks/skills/plugins
sv-memory permissions revoke    # elimina el allow-list de herramientas MCP
```

Consulta [spect_ES.md](spect_ES.md) y el [README_ES](../README_ES.md) para la referencia
completa de CLI y herramientas MCP.
