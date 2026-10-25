# Arquitectura

Cómo funciona sv-memory por dentro: las estructuras de datos clave, estados del ciclo de vida y decisiones de diseño que hacen que la memoria persistente y los grafos de código funcionen para agentes de IA.

## Ciclo de vida de sesión

Las sesiones rastrean el trabajo de codificación a través de interacciones del agente. Cada sesión inicia como `active` y debe ser `completed` para que la recuperación de contexto funcione.

```text
[no creada]
    │
    ▼  sv_mem_session_start / sv-memory session start
    │  o primera llamada a herramienta no-sesión (auto-boot lazy)
 [active] ──────────────────────────────────────────────▶ [completed]
    │                                                       ▲
    │  sv_mem_session_end / sv-memory session end           │
    └───────────────────────────────────────────────────────┘
    │
    └── auto-close (stale > 24h) ──▶ [completed]
```

| Estado | Significado | Cómo se transiciona |
| :--- | :--- | :--- |
| `active` | Sesión en progreso, se están guardando memorias | `StartSession` (explícito o auto-boot lazy) |
| `completed` | Sesión terminada, resumen guardado | `EndSession`, `CloseStaleSessions` |

### Integración por agente

| Agente | Inicio | Fin | Captura de prompts |
| :--- | :--- | :--- | :--- |
| Claude Code | Hook `SessionStart` (auto-start + inyecta bundle) | Hook `SessionEnd` (auto-close, idempotente) | Hook `UserPromptSubmit` |
| OpenCode | `instructions` MCP + `AGENTS.md` + auto-boot lazy del servidor | Protocolo (`AGENTS.md`) | ❌ |
| Antigravity CLI | Hook `PreInvocation` (auto-start + inyección de bundle) | Protocolo (`AGENTS.md`) | ❌ |
| Cursor | Protocolo (`AGENTS.md`) + auto-boot lazy del servidor | Protocolo (`AGENTS.md`) | ❌ |
| Codex | `instructions` MCP + `AGENTS.md` + auto-boot lazy del servidor | Protocolo (`AGENTS.md`) | ❌ |
| Pi | `AGENTS.md` (nativo) + auto-boot lazy del servidor | Protocolo (`AGENTS.md`) | ❌ |

### Auto-boot lazy del lado del servidor

Todo cliente MCP obtiene una sesión aunque olvide llamar
`sv_mem_session_start`. Un middleware de herramientas a nivel de servidor
(`autoBootMiddleware`) ejecuta el chequeo de auto-boot antes de la primera
llamada a una herramienta que no sea de sesión:

```text
primera llamada no-sesión (sv_mem_search, sv_graph_explore, sv_mem_save, ...)
    │
    ▼
CloseStaleSessions(projectID, stale_session_hours)
    │
    ▼
GetActiveSession() ── ¿existe? ──▶ reutilizarla, sin banner
    │ ninguna
    ▼
StartSession(projectID, goal="", directory=ruta del proyecto)
    │
    ▼
anteponer un banner único "[sv-memory Auto-Boot]" a la respuesta
    │  (invita al agente a llamar sv_mem_session_start para el bundle completo)
    ▼
más tarde sv_mem_session_start ──▶ consumeLazySession: adopta la misma fila,
                                    persiste goal/directory explícitos
```

Las herramientas del ciclo de vida (`sv_mem_session_start`,
`sv_mem_session_end`, `sv_mem_session_summary`, `sv_mem_context`) nunca
disparan auto-boot lazy. El banner aparece una sola vez por proceso del
servidor; un `sv_mem_session_start` posterior adopta la fila creada
lazymente en lugar de duplicarla. Los hooks siguen siendo el camino preferido
porque inyectan el Auto-Boot Context Bundle completo antes del primer turno
del modelo; el auto-boot lazy es la red de seguridad independiente del cliente.

**Archivos clave:** `internal/mcp/respond.go` (`autoBootMiddleware`,
`ensureSessionAutoBoot`, `consumeLazySession`), `internal/mcp/mcp_server.go`
(registro del middleware).

### Encadenamiento adaptativo entre herramientas

Las herramientas de descubrimiento cierran su respuesta con un próximo paso
de una línea, consciente de la sesión, para que los agentes encadenen el
protocolo en lugar de detenerse tras una llamada:

| Herramienta | Próximo paso sugerido |
| :--- | :--- |
| `sv_mem_session_start` | `sv_mem_search` + `sv_graph_explore` / `sv_graph_search` |
| `sv_graph_search` | `sv_graph_explore` + `sv_mem_search` |
| `sv_mem_search` | `sv_mem_get` + `sv_graph_explore` (o `sv_propose_spec` si el grafo ya se exploró) |
| `sv_graph_explore` | `sv_mem_search` / `sv_mem_get`, luego `sv_spec_list` → `sv_propose_spec` antes de cambiar comportamiento |

### Auto-cierre de sesiones stale

Cuando se inicia una sesión —explícitamente vía `sv_mem_session_start` o
lazymente en la primera llamada a una herramienta— las sesiones activas
mayores a `stale_session_hours` (default 24h) se cierran automáticamente. Esto
evita que sesiones fugadas se acumulen cuando el usuario sale del agente sin
llamar `sv_mem_session_end`.

```text
sv_mem_session_start / auto-boot lazy
    │
    ▼
CloseStaleSessions(projectID, 24h)
    │  UPDATE sessions SET status='completed'
    │    WHERE status='active' AND started_at < cutoff
    │
    ▼
StartSession() → nueva sesión activa
    │
    ▼
GetAutoBootBundle() → inyectar contexto (solo en inicio explícito; el inicio
                       lazy muestra el banner único y deja que el agente pida
                       el bundle)
```

**Archivo clave:** `internal/memory/memory_session.go` — `CloseStaleSessions`

## Divulgación progresiva

El patrón de 3 capas minimiza tokens retornando resultados compactos primero, permitiendo al agente profundizar bajo demanda:

| Capa | Tool | Retorna | ~Tokens |
| :--- | :--- | :--- | :--- |
| 1 — Búsqueda | `sv_mem_search` | IDs, categorías, fechas, títulos, topic_keys | 30/result |
| 2 — Timeline | `sv_mem_timeline` | Contexto cronológico alrededor de una memoria | 100/result |
| 3 — Get | `sv_mem_get` | Contenido completo (what, why, learned, etc.) | 200/result |

**Ahorro de tokens:** ~148x vs leer archivos raw (12,852 → 87 tokens para graph hubs).

**Archivo clave:** `internal/memory/memory_search.go` — `SearchMemoriesCompact`

## Unificación del grafo

sv-memory unifica tres tipos de nodos en un solo grafo de dependencias:

```text
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Code Nodes │────▶│ Memory Nodes│────▶│  Spec Nodes │
│ (funciones, │     │(decisiones, │     │(capabilities,│
│  clases,    │     │  estándares,│     │   cambios)  │
│  archivos)  │     │  bugfixes)  │     │             │
└─────────────┘     └─────────────┘     └─────────────┘
```

### Tipos de nodos

| Tipo | Fuente | Ejemplo |
| :--- | :--- | :--- |
| `function` | tree-sitter AST | `InitDB`, `SaveMemory` |
| `class` | tree-sitter AST | `TreeSitterExtractor` |
| `file` | scanner | `internal/db/db.go` |
| `memory` | `sv_mem_save` | Decisión: "Usar Postgres" |
| `spec` | `sv_propose_spec` | Capability: `auth-session` |
| `rationale` | Comentarios AST | `NOTE:`, `WHY:`, `HACK:` |

### Tipos de aristas

| Relación | Fuente | Significado |
| :--- | :--- | :--- |
| `imports` | AST | Archivo A importa archivo B |
| `calls` | AST (EXTRACTED) / heurístico (INFERRED) | Función A llama a función B |
| `depends_on` | transitivo | A depende de B (cierre transitivo) |
| `references` | AST | Símbolo A referencia símbolo B |
| `rationale_for` | Comentarios AST | Comentario explica nodo de código |
| `implements` | linking de specs | Código implementa capability |
| `supersedes` | juicio de memoria | Decisión nueva reemplaza vieja |
| `conflicts_with` | juicio de memoria | Dos decisiones se contradicen |

### Detección de comunidades

El algoritmo Leiden detecta comunidades de código que cambian juntas. Cada nodo obtiene un `community_id` en sus metadatos. Las comunidades permiten:

- **graph_boost** en `sv_mem_search`: buscar un archivo expande a toda su comunidad
- **Context Pack**: muestra membresía de comunidad para orientación
- **God Nodes**: nodos más conectados entre comunidades (hotspots arquitectónicos)

**Archivos clave:** `internal/graph/communities.go`, `internal/graph/leiden.go`

## Superficie de conflictos

Los conflictos de memoria surgen cuando dos decisiones se contradicen.

```text
[sin relación]
    │  sv_mem_judge
    ▼
 [pending] ──▶ escaneo semántico (LLM opt-in) ──▶ [judged]
                                                     │
                                       ┌─────────────┴─────────────┐
                                       ▼                           ▼
                                 [applied]                    [ignored]
```

### Tipos de relación

| Tipo | Significado |
| :--- | :--- |
| `supersedes` | Decisión nueva reemplaza vieja |
| `conflicts_with` | Dos decisiones se contradicen |
| `relates_to` | Asociación general |

**Archivos clave:** `internal/memory/conflicts.go`, `internal/memory/semantic.go`

## Economía de tokens

sv-memory minimiza el uso de tokens en cada capa:

| Mecanismo | Ubicación | Efecto |
| :--- | :--- | :--- |
| `token_budget` | Tools MCP | Trunca respuesta a N tokens |
| `bundleWhyChars` | Auto-Boot bundle | Limita rationale por memoria (default 300) |
| `truncateReason` | Judge/Compare | Limita razón de relación a 200 chars |
| `graph_boost` | `sv_mem_search` | Expande búsqueda a comunidad (evita re-queries) |
| `semanticRecallMaxCandidates` | Semantic recall | Limita candidatos enviados al LLM (30) |
| `semanticRecallFieldChars` | Semantic recall | Limita cada campo por candidato (300) |
| `context_pack_max_memories` | Context Pack | Limita memorias vinculadas por pack (default 5) |

## Ciclo de decisión orientado a specs

Antes de modificar comportamiento o arquitectura, los agentes deben seguir el loop de 5 tools:

```text
1. sv_spec_list      → ver qué propuestas existen
2. sv_spec_get       → inspeccionar una propuesta específica
3. sv_propose_spec   → registrar nueva propuesta (pre-flight check)
4. sv_update_spec    → marcar tareas completas durante implementación
5. sv_validate_decision → re-verificar después de edits
6. sv_commit_spec    → promover a memoria de decisión durable
```

### Pre-flight check

`sv_propose_spec` ejecuta un pre-flight check contra estándares/decisiones existentes:

| Veredicto | Significado | Acción |
| :--- | :--- | :--- |
| `BLOCK` | Invariante pinned se superpone | No se puede proceder |
| `WARN` | Superposición ordinaria | Proceder con precaución |
| `PASS` | Sin conflictos | Seguro para proceder |

### Merge de capability state

Cuando un cambio se confirma, sus delta requirements se mergean en el capability state (`.sv-memory/specs/capabilities/<cap>/spec.md`). El estado materializado actual se proyecta al spec mirror para lectura humana.

**Archivos clave:** `internal/memory/specs.go`, `internal/memory/changes.go`

## Sincronización y frescura

### Git sync

Cuando el sync con Git está activado (`git_sync_enabled`, **desactivado por defecto**), las memorias se sincronizan con Git vía archivos JSON chunked (`.sv-memory/chunks/<id>.json`). La sincronización tiene debounce (default 500ms) y coalesce múltiples cambios en un solo commit. Mantén `.sv-memory/` fuera del control de versiones en repositorios públicos.

### Watcher de archivos en background

Un watcher basado en `fsnotify` monitorea el directorio del proyecto por cambios de archivos. Cuando está activo, las queries del grafo saltan el walk O(n) `DetectStaleFiles` — la frescura viene del sync en background, el path de query es O(1).

### Auto-freshness en context packs

`sv_graph_explore` llama `SyncGraphIfHasChanges` antes de resolver nodos, asegurando que los snippets reflejen el estado más reciente del disco sin `sv_graph_sync` manual.

**Archivos clave:** `internal/memory/sync.go`, `internal/graph/watcher.go`, `internal/graph/incremental.go`
