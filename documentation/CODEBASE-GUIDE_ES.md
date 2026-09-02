# Guía del Código (Codebase Guide)

Un recorrido por el código de sv-memory: cómo encajan las piezas, los flujos de
datos clave (guardar una memoria, consultar el grafo, sincronizar, detección de
conflictos) y dónde mirar al extender el proyecto. Complementa la especificación
([spect_ES.md](spect_ES.md)) enfocándose en **flujos**, no en interfaces.

## Mapa de paquetes

| Paquete | Responsabilidad | Puntos de entrada clave |
| :--- | :--- | :--- |
| `cmd/sv-memory/` | CLI Cobra: registro del comando raíz, `init`, `mcp`, `setup`, `configure`, `hooks`, `permissions`, `graph`, `conflicts`, `projects` | `main.go`, `cmd_init.go`, `cmd_setup.go` |
| `internal/config/` | Rutas, config YAML viper, asistente `configure` + escritores de config MCP (cursor/windsurf/claude) | `config.go`, `configure.go` |
| `internal/db/` | Apertura/ajuste de SQLite, migraciones, pool WAL lector/escritor | `db.go`, `pool.go`, `migrations.go` |
| `internal/graph/` | Escáner, construcción del grafo de dependencias (incremental + completo), BFS, comunidades Leiden, betweenness, god nodes, aristas AST de llamadas | `graph.go`, `incremental.go`, `relations.go`, `communities.go`, `leiden.go`, `memory.go` |
| `internal/graph/extractor/` | Extractor tree-sitter (símbolos, imports, refs AST de llamadas), fallback regex | `tree_sitter.go`, `regex.go`, `extractor.go` |
| `internal/mcp/` | Servidor MCP stdio + 34 handlers de herramientas | `mcp.go` (núcleo + registro de tools), `server_sync.go`, `graph_load.go`, `respond.go`, `tools_*.go` |
| `internal/memory/` | CRUD de memorias, sesiones, dedup, conflictos, compactación, git sync, context pack, stats | `memory.go`, `save.go`, `memory_session.go`, `conflicts.go`, `contextpack.go`, `sync.go`, `prompts.go` |
| `internal/hook/` | Scripts/skills de hooks PreToolUse + ciclo de vida (Claude Code, OpenCode, Antigravity, Codex) | `hook.go`, `templates.go`, `scripts/` |
| `internal/protocol/` | Inyección de protocolo en AGENTS.md / `.cursorrules` / `.windsurfrules` | `protocol.go` |
| `internal/perm/` | Gestión de allow-lists de herramientas MCP (Antigravity, Claude Code) | `perm.go` |
| `internal/security/` | Sanitizador regex de secretos (API keys, JWTs, PEM, credenciales) | `security.go` |
| `internal/tui/` | Interfaz terminal interactiva (charmbracelet/huh + bubbletea) | `tui.go` |

## Flujo 1 — Guardar una memoria (`sv_mem_save`)

El flujo de escritura más común. Todo parte del handler MCP y termina en
SQLite, y luego se sincroniza con debounce a un chunk del workspace Git.

```text
El agente llama sv_mem_save
   │
   ▼
internal/mcp/tools_save.go  handleSave
   │  requiere category / what / why / learned
   │  auto-asocia session_id vía GetActiveSession (pool lector)
   │  cachea metadata de git (branch/commit/author, TTL 30s)
   ▼
internal/memory/memory.go  SaveMemory (orquestador)
   │  calcula normalized_hash (what+why+learned+where_path)
   │  delega en helpers de save.go sobre una única transacción de escritor:
   │    upsertByTopicKey → upsert por topic_key (revision_count++)
   │    bumpDuplicate   → chequeo de dedup en ventana móvil (24h)
   │    insertMemory    → insert nuevo vía helper compartido memoryInsertArgs
   ▼
SQLite (escritor)  ──  memories + memories_fts (triggers mantienen FTS5 en sync)
   │
   ├─▶ scheduleSync() — git sync con debounce de 500ms
   │      └─ internal/memory/sync.go  SyncToGit → .sv-memory/chunks/{id}.json
   │
   └─▶ similarMemoriesHint() — FindSimilarMemories acotado en tiempo (200ms)
            └─ expone candidatos para sv_mem_judge
```

**Dónde extender:** un campo nuevo en el guardado toca `memoryInsertArgs` en
`memory_util.go`, la migración de la tabla `memories` en `migrations.go` y el handler
MCP en `tools_save.go`. Los caminos de guardado viven en `save.go`.

## Flujo 2 — Consultar el grafo de dependencias (`sv_graph_query`)

Las lecturas del grafo usan carga diferida + caché LRU para que una consulta
nunca re-escanee el proyecto.

```text
El agente llama sv_graph_query(path_or_node, depth, direction, relation_type)
   │
   ▼
internal/mcp/tools_graph.go  handleGraphQuery
   │
   ├─▶ getOrLoadGraph()
   │      ├─ SyncGraphIfStale() — chequeo de mtime/size (internal/graph/stale.go)
   │      └─ GlobalGraphCache (LRU, por proyecto) — miss → LoadFullGraph
   │
   ▼
internal/graph/memory.go  consulta BFS (sub-ms en acierto de caché)
   │
   ▼
Se renderiza y devuelve el diagrama Mermaid (truncado por token-budget vía respond)
```

**Dónde extender:** la lógica BFS y de rutas vive en `internal/graph/memory.go`;
el metadata de nodos (comunidades, centralidad) lo añade
`UpdateCommunitiesAndCentrality` en `communities.go`.

## Flujo 3 — Construir / refrescar el grafo (`sv_graph_sync`)

El grafo se construye incrementalmente; un rebuild completo solo ocurre en la
primera ejecución o cuando cambió >30% de los archivos rastreados.

```text
sv_graph_sync / sv-memory graph rebuild
   │
   ▼
internal/graph/incremental.go
   │
   ├─▶ trySyncGraphIncrementalFiltered
   │      scanFilesFiltered → clasificar por mtime/size
   │      │   sin cambios → skip
   │      │   nuevo/cambiado → toParse
   │      │   faltante      → deleted
   │      churn > 30% de rastreados → caer a rebuild completo
   │      tx: borrar nodos/aristas obsoletos → parseFiles (imports/references)
   │          → parseManifests (depends_on) → extractCallEdges (calls)
   │          → extractContainsEdges (contains) → updateFileMeta
   │
   └─▶ syncGraphFull (fallback)
          DELETE todo → re-escanear → bulkInsertNodes/Edges → mismas pasadas
```

**Precisión de aristas `calls` (AST vs heurístico):** `extractCallEdges` en
`relations.go` ahora prefiere `ExtractCallRefs` de tree-sitter por archivo — las
aristas llevan confianza `EXTRACTED` con ubicación `L<línea>:<columna>`. Los
archivos sin cobertura AST de llamadas (Go, Lua, Markdown, shell, Vue/Svelte/Astro)
mantienen el heurístico de tokenize (`INFERRED`). Ver [spect_ES.md §14](spect_ES.md).

## Flujo 4 — Ciclo de vida de sesión

Las sesiones agrupan memorias y habilitan la recuperación de contexto tras la
compactación.

```text
sv_mem_session_start ──▶ internal/memory/memory_session.go  StartSession
   │                      (status='active', reinicia el ledger de tokens)
   │                      GetAutoBootBundle: resumen previo + decisiones
   │                      + estándares + bugfixes + postmortem + Q&A + hubs del grafo
   │
   ├── sv_mem_save(session_id=...) ── memorias vinculadas a la sesión
   │
   ├── sv_mem_session_summary ── SaveSessionSummary (goal/discoveries/...)
   │
   └── sv_mem_session_end ── EndSession (status='completed')

Tras la compactación:
   sv_mem_context ──▶ GetSessionContext: goal/resumen de la última sesión completada
                      + memorias + prompts de usuario recientes (sv_mem_capture_prompt)
```

## Flujo 5 — Detección de conflictos y juicio

`sv_mem_conflicts` escanea memorias similares y registra relaciones.

```text
sv_mem_conflicts action=scan [semantic=true]
   │
   ▼
internal/memory/conflicts.go  ScanConflicts
   │  similitud Jaccard sobre what+why+learned (umbral configurable, 0.45)
   │  solo memorias nuevas desde last_conflict_scan_at (incremental)
   │  --apply persiste relaciones pendientes; --max-insert limita (100)
   │
   ├─▶ solo léxico → devuelve lista de candidatos
   │
   └─▶ semantic=true → internal/memory/semantic.go  JudgeConflictCandidates
          │  invoca el CLI del agente configurado (claude/opencode)
          │  veredictos: supersedes / conflicts_with / relates_to / none
          ▼
   sv_mem_judge ──▶ internal/memory/memory_relations.go  SaveJudgment
                    (reason limitado a 200 caracteres por disciplina de tokens)
```

## Flujo 6 — Context pack (`sv_mem_context_pack`)

El puente grafo→memoria en una sola llamada acotada. Pasando `include_changes="true"` además se exponen los spec changes activos cuyo `where_path` coincide con la ruta; los nodos de capability enlazados vía aristas `implements` añaden la sección "Capabilities implemented here" (acotada: máx 10 caps, 5 nombres de requirement cada una).

```text
sv_mem_context_pack(path, [include_changes])
   │
   ▼
internal/memory/contextpack.go  GetContextPack
   │  resuelve la ruta a un nodo del grafo (ResolveContextNode)
   │  rol del nodo: tipo, fan-in/fan-out, comunidad, flag de hub
   │  memorias vinculadas vía where_path ∪ aristas rationale_for
   │  cada una renderizada como título + why truncado a bundle_why_chars
   │  (include_changes) changes activos de la ruta (changesForPath)
   │  capabilities alcanzables vía aristas 'implements' (capabilitiesForNode)
   ▼
pack compacto devuelto (memorias limitadas por context_pack_max_memories)
```

## Flujo 7 — Git sync (por chunks)

Las memorias se comparten entre clones como chunks JSON por memoria, evitando
conflictos de merge entre ediciones no relacionadas.

```text
sv-memory sync / scheduleSync con debounce
   │
   ▼
internal/memory/sync.go  SyncToGit / SyncFromGit
   │  exportar: cada memoria → .sv-memory/chunks/{id}.json (tmp+rename atómico)
   │  importar: leer chunks → upsert por ID
   │  marcadores de conflicto o JSON inválido → omitir chunk con warning
   │  edición local más nueva/divergida vs chunk traído → warning last-writer-wins
   ▼
git commit del directorio .sv-memory/ (el usuario corre git add/commit)
```

## Flujo 8 — Decisiones spec-driven con delta requirements

El ciclo propose → validate → commit lleva delta requirements estilo OpenSpec
que se fusionan en un estado durable por capability y se conectan al grafo.

```text
sv_propose_spec(slug, title, what, where_path, requirements, tasks, capability_path)
   │  internal/mcp/tools_spec.go  handleProposeSpec
   │  CreateChange (changes) + opcional SetChangeCapabilityPath (default=slug)
   │  ParseSpecDeltas → ReplaceChangeRequirements (spec_requirements)
   │  PreflightCheck (FTS5+Jaccard vs standard/decision/architecture)
   │  graph.EnsureSpecCapabilityEdges (nodo spec:<cap> + arista implements)
   ▼
sv_update_spec(change_id, tasks, design, what, goal, requirements)
   │  internal/mcp/tools_spec.go  handleUpdateSpec
   │  UpdateChange (changes) + métricas FormatTaskProgress + sync de espejo
   ▼
sv_validate_decision(change_id)   ValidateChangeRequirements
   │  warn de presencia RFC 2119 · warn de escenario eliminado en MODIFIED vs spec_capabilities
   ▼
sv_commit_spec(change_id)
   │  MergeChangeDeltas → MergeDeltas (spec_capabilities)
   │    RENAMED primero, ADDED estricto, MODIFIED reemplaza bloque, REMOVED leniente
   │  SaveMemory (decision/<slug>) → LinkDecisionToCapability (implements)
   │  WriteSpecMirror: changes/<slug>.md (con deltas) + capabilities/<cap>/spec.md
   ▼
cambio aplicado; estado de capability + grafo + mirror consistentes
```

Parser: `internal/memory/requirements.go` (`ParseSpecDeltas` / `DeltasToMarkdown`
/ `ExtractRFC2119`); persistencia/merge: `internal/memory/spec_requirements.go`;
wiring al grafo: `internal/graph/spec_link.go`.

## Dónde añadir una nueva herramienta MCP

1. Registra el handler en `internal/mcp/tools_*.go` (un método de `Server`).
2. Registra la herramienta en `NewServer` (`internal/mcp/mcp.go`) con `mcp.NewTool`.
3. Añade una entrada correspondiente en `AllTools` (mismo archivo) — el test
   guardián `TestAllToolsMatchesRegisteredTools` exige el emparejamiento.
4. Añade la función de store en `internal/memory/` (o `internal/graph/`).
5. Actualiza el protocolo (`internal/protocol/protocol.go`), el skill de OpenCode
   (`internal/hook/scripts/opencode-skill.md`) y la documentación (README, spect,
   getting-started, AGENT-SETUP) — EN y ES — además del CHANGELOG.

## Dónde añadir un nuevo lenguaje al grafo

1. Añade el mapeo extensión→lenguaje en `languageFromExt` (`graph.go`) y
   `getLanguageGroup` (`relations.go`).
2. Añade la gramática tree-sitter a `GetLanguage` (`extractor/tree_sitter.go`)
   y un extractor de símbolos `parseX`; implementa `ExtractCallRefs` para llamadas AST.
3. Añade la extensión al conjunto soportado del escáner (consciente de
   `.sv-memoryignore`) y a la documentación: `spect_ES.md §10` (tabla de lenguajes).
4. Añade un fixture de test con un archivo de muestra que ejercite extracción de
   símbolos + imports + llamadas.

## Convenciones y salvaguardas

- **Fuente única de verdad:** la superficie de herramientas MCP es `mcp.AllTools`;
  las formas de nodo/arista del grafo viven en `internal/graph/schema/`.
- **Disciplina de tokens:** las herramientas de lectura masiva aceptan
  `token_budget` y pasan por el truncamiento de `Server.respond`; añade lo mismo
  a cualquier lector masivo nuevo.
- **Secretos:** todo campo de texto libre que se persista debe pasar por
  `security.SanitizeText` (texto de memorias, resúmenes de sesión, texto rationale
  del grafo, prompts).
- **Concurrencia SQLite:** las escrituras usan el pool `Writer`; las lecturas usan
  `Reader` (WAL). Mantén indexadas las búsquedas calientes — añade una migración
  para índices nuevos.
- **Lint antes de push:** `golangci-lint run ./...` (el analizador govet shadow
  solo está habilitado ahí, no en `go vet`). En los tests, reutiliza el `err`
  externo (`if err = X(); err != nil`) cuando exista uno en el ámbito.
