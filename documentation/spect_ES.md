# SPEC.md Especificación SV-Memory v3

> **Idioma:** [English](spect.md) | **Español**

## 1. Visión y Objetivo Principal

`sv-memory` es una herramienta CLI de alto rendimiento de binario único y un servidor de Model Context Protocol (MCP) escrito en **Go**. Su propósito es eliminar la amnesia de contexto de los agentes de IA combinando:

1. **Memoria persistente de decisiones:** Captura correcciones no obvias, decisiones arquitectónicas, estándares de codificación, diarios de progreso, discusiones, Q&A e ideas mediante SQLite + búsqueda de texto completo FTS5.
2. **Grafo de conocimiento estructural:** Mapea entidades de código (archivos, componentes, imports, dependencias) para proporcionar contexto estructural a los agentes LLM mediante grafos dirigidos de dependencias.
3. **Orquestación autónoma de agentes:** Inyecta reglas de protocolo en `AGENTS.md`, `.cursorrules` o `.windsurfrules` para que los agentes consulten, registren y mantengan el contexto automáticamente durante las sesiones de codificación.
4. **Colaboración en equipo:** Sincronización bidireccional con Git mediante JSON (`.sv-memory/chunks/*.json`, un archivo por memoria) para que todo el equipo comparta el contexto entre clones.

Desarrollado bajo el ecosistema de **SVTech** como una herramienta gratuita y de código abierto para la comunidad de desarrolladores.

---

## 2. Arquitectura y Flujo del Sistema

```text
       ┌────────────────────────────────────────────────────────┐
       │       AI Agent (Cursor / Windsurf / Claude Code /       │
       │        OpenCode / Codex / Antigravity / Zed / VS Code)  │
       └───────────────────────────┬────────────────────────────┘
                                   │  MCP Protocol via Stdio
       ┌───────────────────────────▼────────────────────────────┐
       │                    sv-memory Binary                      │
       │                                                         │
       │  ┌──────────────────┐ ┌──────────────┐ ┌────────────┐   │
       │  │ Memory Engine    │ │ Graph Engine │ │ Config/Env │   │
       │  │ + Sessions       │ │ + Cache      │ │ + Security │   │
       │  └────────┬─────────┘ └──────┬───────┘ └─────┬──────┘   │
       └───────────┼──────────────────┼───────────────┼──────────┘
                   │                  │               │
       ┌───────────▼──────────────────▼───────────────▼─────────┐
       │           Global SQLite Storage (+FTS5)                 │
       │         (~/.config/sv-memory/storage.db)                │
       └────────────────────────────────────────────────────────┘
                             │
        ┌─────────────────────────▼─────────┐
        │ .sv-memory/chunks/                │
        │  <memory-id>.json                 │  ← Git-committed team sync
        └───────────────────────────────────┘
```

### Principios de Diseño Clave

- **Divulgación Progresiva (eficiente en tokens):** Patrón de recuperación de 3 capas (búsqueda compacta → línea de tiempo → contenido completo) que minimiza el consumo de tokens del LLM.
- **Ciclo de Vida de Sesión:** Las sesiones agrupan memorias relacionadas, permiten la recuperación de contexto tras la compactación y rastrean objetivos/descubrimientos/próximos pasos.
- **Rendimiento:** El caché de grafo en memoria elimina las consultas N+1 a SQL; pool de conexiones dividido (escritor + lector) en modo WAL; actualizaciones incrementales del grafo basadas en mtime; sincronización Git con coalescencia de escrituras con debounce.
- **Seguridad:** La sanitización de secretos redacta claves API, tokens y contraseñas antes del almacenamiento; no se ejecutan operaciones Git autónomas.

---

## 3. Stack Tecnológico y Librerías Clave

- **Lenguaje:** Go 1.26+ (1.26.3 requerido por `go.mod`)
- **Motor de almacenamiento:** SQLite mediante `modernc.org/sqlite` (Go puro, sin CGO, totalmente portable) con búsqueda de texto completo **FTS5**.
- **Servidor de protocolo:** MCP Go SDK (`github.com/mark3labs/mcp-go`, v0.57.0).
- **Framework CLI:** `github.com/spf13/cobra` para el manejo de comandos.
- **UIs interactivas:** `charmbracelet/huh` (asistente de configuración, formularios TUI), `charmbracelet/bubbles` + `lipgloss` (TUI), `charmbracelet/bubbletea` (runtime TUI).
- **Configuración:** `github.com/spf13/viper` (config YAML global/local con precedencia de flags/env).
- **Parseo de grafos:** `github.com/odvcencio/gotreesitter` (bindings tree-sitter en Go puro) con respaldo por regex para lenguajes heredados o casos límite.
- **Caché de grafos:** `github.com/hashicorp/golang-lru/v2` (caché LRU de grafo en memoria).
- **Generación de UUIDs:** `github.com/google/uuid` (IDs hex con 64 bits de entropía mediante `newID()`).
- **Seguridad:** Redacción basada en regex para claves OpenAI, claves Anthropic, claves Gemini, tokens JWT, claves privadas RSA/EC, cadenas de conexión a BD y patrones genéricos de secretos.

---

## 4. Comandos CLI y Flujo de Trabajo

`sv-memory` proporciona comandos CLI organizados bajo la raíz de Cobra y sus subcomandos:

### Comandos Principales

#### 1. `sv-memory init`

- Calcula un `project_id` hex determinista de 16 caracteres (SHA256 de la ruta raíz del repositorio Git).
- Registra la entrada del proyecto en la base de datos central SQLite.
- Verifica la existencia de `AGENTS.md`, `.cursorrules` o `.windsurfrules`:
  - Si existe alguno: Inyecta o actualiza el bloque `<!-- SV-MEMORY:START -->...<!-- SV-MEMORY:END -->`.
  - Si no existe ninguno: Crea `AGENTS.md` con la plantilla completa del protocolo.
- Escanea el directorio del proyecto y realiza una construcción inicial del grafo de conocimiento.
- Sincroniza memorias desde `.sv-memory/chunks/` (contexto compartido por el equipo).

#### 2. `sv-memory mcp`

- Inicia el servidor MCP JSON-RPC sobre `stdio` para el consumo por parte de agentes.
- Registra las 31 herramientas MCP.
- Mantiene un caché de grafo en memoria para recorridos BFS sin SQL.
- Aplica debounce a las escrituras de Git sync (coalescencia de 500ms).

#### 3. `sv-memory version`

- Imprime la versión de compilación, el commit y el runtime de Go (`go`, `GOOS`/`GOARCH`).
- La versión se inyecta en tiempo de compilación mediante `-ldflags`, por lo que los binarios de release reportan el tag con el que fueron construidos.

#### 4. `sv-memory update`

- Verifica en GitHub Releases si hay una versión más nueva, pide confirmación, descarga el binario de la plataforma, verifica su checksum SHA-256 contra `checksums.txt` y reemplaza atómicamente el ejecutable en uso (en Windows imprime un comando `copy` manual ya que el `.exe` en ejecución no se puede sobrescribir).

#### 5. `sv-memory diagnose`

- Ejecuta chequeos de salud que verifican conexiones a la BD, esquemas, carpetas, permisos de escritura y ajustes activos.

#### 6. `sv-memory stats`

- Muestra estadísticas del proyecto: memorias totales, memorias eliminadas, guardados recientes en 24h, número de sesiones, sesiones activas y conteos de relaciones.

#### 7. `sv-memory sync`

- Extrae desde `.sv-memory/chunks/` y empuja los cambios locales de SQLite de vuelta a dicha carpeta (JSON por memoria en chunks). Los IDs de memoria distintos nunca entran en conflicto; una edición concurrente del _mismo_ ID deja marcadores de conflicto en `{id}.json`, que la importación **omite con un warning** en vez de abortar todo el sync. Importar un chunk que sobreescribiría una edición local más nueva (mayor `revision_count`) o divergida en la misma revisión registra un **warning de last-writer-wins**: gana el chunk de git, pero la edición local perdida queda en superficie. Resuelve un chunk en conflicto quitando los marcadores y volviendo a ejecutar `sv-memory sync`.

#### 8. `sv-memory tui`

- Inicia una interfaz de terminal interactiva (`charmbracelet/huh`/bubbletea) para inspección de memorias, búsqueda BM25, diagnósticos de grafo, exportación a bóveda Obsidian y exportación Cypher para Neo4j/FalkorDB.

#### 9. `sv-memory configure`

- Asistente interactivo para configuraciones automáticas/manuales de editores (Cursor, VS Code, Zed, Windsurf, OpenCode) y CLIs (Claude Code, Codex, Antigravity).
- **Fase 4 (Permisos MCP):** Lista las 31 herramientas MCP de sv-memory con descripciones y otorga las entradas de allow-list seleccionadas a las plataformas con allow-list elegidas previamente (Antigravity CLI, Claude Code).
- **Subcomandos** para leer/escribir configuración (YAML, global `~/.sv-memory/config.yaml` o local `.sv-memory/config.yaml`):
  - `sv-memory configure get <key>`: imprime un único valor de configuración.
  - `sv-memory configure set <key> <value> [--local]`: escribe un valor de forma global (por defecto) o local al proyecto.
  - `sv-memory configure list`: imprime todos los valores de configuración activos (`default_db_path`, `git_sync_enabled`, `conflict_threshold`, `default_review_limit`, `auto_compaction_enabled`, `compaction_interval_minutes`, `max_response_tokens`, `max_field_chars`, `search_expand_chars`, `timeline_why_chars`, `bundle_why_chars`, `context_pack_max_memories`, `graph_boost`).

#### 10. `sv-memory permissions`

- `list`: muestra las 31 herramientas MCP de sv-memory con descripciones legibles.
- `grant --platform <p> [--all | --tool a,b] [--dry-run]`: escribe entradas de allow-list (`mcp(sv-memory/<tool>)` para Antigravity, `mcp__sv-memory__<tool>` para Claude Code), conservando entradas no relacionadas.
- `revoke --platform <p> [--dry-run]`: elimina las entradas de sv-memory de la allow-list.
- `status [--platform <p>]`: reporta herramientas otorgadas vs faltantes por plataforma.
- OpenCode y Codex usan aprobación interactiva y se omiten (sin allow-list estática).

#### 11. `sv-memory setup [agente]`

- Integración de un solo comando que replica `engram setup <agent>`: configura el servidor MCP, hooks/skills/plugins, inyección de protocolo (`AGENTS.md` / `.cursorrules` / `.windsurfrules`) y permisos de herramientas MCP para un agente.
- Agentes soportados: `claude-code`, `opencode`, `cursor`, `windsurf`, `antigravity`, `codex`.
- `sv-memory setup` (sin argumentos): solo lectura — imprime el estado de instalación por agente.
- `sv-memory setup <agente>`: instala el agente de extremo a extremo (idempotente).
- `--all`: instala todos los agentes soportados.
- `--strict`: instala hooks estrictos (bloquea la primera lectura cruda en Antigravity; solo nudge en Claude Code).
- **Claude Code:** escribe un `.mcp.json` local del proyecto cuando el CLI `claude` no está, instala hooks `PreToolUse` + ciclo de vida (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`) en `.claude/hooks/` y los registra en `.claude/settings.json`, inyecta el protocolo en `AGENTS.md` y concede el allow-list de 31 herramientas en `~/.claude/settings.json`.
- **OpenCode:** registra el servidor MCP en `opencode.json`, instala `SKILL.md` más el plugin nativo TypeScript `.opencode/plugin/sv-memory.ts` (añade el tool `sv_memory_context`) e inyecta el protocolo en `AGENTS.md`.
- **Cursor:** escribe `.cursor/mcp.json` e inyecta `.cursorrules`.
- **Windsurf:** escribe `.windsurf/mcp_config.json` e inyecta `.windsurfrules`.
- **Antigravity CLI:** registra el servidor MCP, instala los hooks de `.agents/hooks.json`, inyecta `AGENTS.md` y concede el allow-list de 31 herramientas.
- **Codex:** escribe el bloque `[mcp_servers.sv-memory]` en `~/.codex/config.toml`, instala un hook no-op e inyecta `AGENTS.md`.

#### 12. `sv-memory hooks`

- `install [--strict] [--context-injection] [--platform <p>]`: instala hooks PreToolUse (`.agents/hooks.json` + `.agents/hooks/sv-memory.sh`) para que los agentes consulten la memoria antes de leer archivos. En Claude Code también instala los hooks de ciclo de vida (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`) en `.claude/hooks/` y los registra en `.claude/settings.json` en el formato oficial de arrays. `--strict` bloquea la primera lectura raw de cada sesión. Plataforma por defecto: todas (`claude-code`, `codex`, `antigravity`, `opencode`).
- **Inyección silenciosa de contexto (`--context-injection`, opt-in, default off):** cuando se habilita, el hook PreToolUse de Claude Code llama `sv-memory context <file>` en la primera `Read` de cada archivo e inyecta el context pack compacto grafo+memorias (título + `why` truncado, acotado a 3 memorias) como `additionalContext`. La salida se cachea por archivo para la sesión y está acotada en tiempo (2s, timeout portable); el hook siempre sale con `exit 0` (fail-open) de modo que un binario o `.sv-memory` ausente nunca rompe la llamada. Se activa mediante el marcador `.sv-memory/context-injection-enabled` creado por el flag. Antigravity, Codex y OpenCode no soportan inyección por `additionalContext` y mantienen el mecanismo de nudge/skill.
- **Degradación del modo strict (fail-open):** los scripts de hook nunca llaman al servidor sv-memory. El bloqueo strict solo está implementado en Antigravity CLI; en Claude Code el modo strict es solo nudge (siempre `exit 0`). El bloqueo se omite cuando sv-memory no está disponible (sin `.sv-memory/`, binario ausente, o `SV_MEMORY_STRICT_DISABLE=1`), de modo que el agente nunca queda atascado por un sv-memory ausente o mal configurado.
- `uninstall [--context-injection] [--platform <p>]`: elimina los hooks (y el marcador de inyección cuando se pasa `--context-injection`).
- `status`: reporta qué plataformas tienen hooks instalados y si la inyección silenciosa está habilitada.

#### 13. `sv-memory obsidian-export [-o output-dir]`

- Exporta todas las memorias del proyecto a archivos Markdown dentro de la carpeta de destino (por defecto `.obsidian-sv-memory`) estructurados como una bóveda de Obsidian.

#### 14. `sv-memory export [output-file]`

- Exporta todas las memorias no eliminadas de este proyecto a un archivo JSON portátil.

#### 15. `sv-memory import <input-file>`

- Importa memorias desde un archivo JSON usando upsert por ID.

### Comandos de Eliminación de Memoria y Sesión

#### 16. `sv-memory delete session <session-id>`

- Elimina una sesión vacía (falla si la sesión contiene memorias asociadas).

#### 17. `sv-memory delete project <project-id> [--hard]`

- Elimina en cascada todos los datos del proyecto. Por defecto hace soft-delete de las memorias; `--hard` las elimina permanentemente de SQLite.

### Gestión del Registro de Proyectos

#### 18. `sv-memory projects list`

- Lista todos los proyectos registrados con su ID, nombre, ruta, conteos de memorias y conteos de sesiones.

#### 19. `sv-memory projects prune`

- Elimina proyectos vacíos (aquellos con 0 memorias y 0 sesiones) del registro central de SQLite.

#### 20. `sv-memory projects consolidate <source-project-id> <target-project-id>`

- Fusiona todas las memorias y sesiones del proyecto origen en el proyecto destino y luego elimina el proyecto origen.

### Gestión del Grafo de Código

#### 21. `sv-memory graph rebuild`

- Fuerza un escaneo completo del directorio de código, reconstruyendo nodos y aristas del grafo.

#### 22. `sv-memory graph path <source> <target>`

- Calcula e imprime la ruta de dependencia más corta entre dos nodos de código del grafo (hasta 10 saltos).

#### 23. `sv-memory graph explain <node>`

- Imprime información detallada de un nodo específico: tipo, etiqueta, ruta, metadatos JSON y métricas fan-in/fan-out.

#### 24. `sv-memory graph communities`

- Ejecuta detección de comunidades Leiden sobre el grafo. Lista los clústeres de comunidades, sus nodos miembros, puntuaciones de centralidad y nodos god.

#### 25. `sv-memory graph wiki [--output dir]`

- Exporta páginas wiki en Markdown para cada comunidad detectada, listando archivos miembros, puntuaciones de centralidad y dependencias inter-comunidad. Directorio de salida por defecto: `graph-wiki`.

#### 26. `sv-memory graph viz [--output file] [--open]`

- Genera una visualización HTML interactiva usando vis.js con simulación física coloreada por comunidad, filtrado de nodos y tooltips. Salida por defecto: `graph.html`. Se abre en el navegador por defecto (`--open=false` para omitirlo).

#### 27. `sv-memory graph merge <project-id-a> <project-id-b> [-o output-file]`

- Carga dos grafos de proyecto y produce un union-merge por ID de nodo, actualizando nodos y aristas en un snapshot JSON (salida por defecto: `merged-<a>-<b>.json`).

### Gestión de Conflictos

#### 28. `sv-memory conflicts`

- `list [--status pending|judged|ignored] [--project P]`: muestra memorias conflictivas y superposiciones semánticas detectadas.
- `stats`: resume los conteos de relaciones de conflicto por estado.
- `scan [--apply] [--dry-run] [--max-insert N] [--threshold T]`: ejecuta el escaneo incremental de superposición semántica; por defecto solo reporta sin persistir (`--apply` guarda las relaciones `potential_conflict` detectadas).
- `scan --semantic [--agent claude|opencode|CMD] [--max-semantic N] [--concurrency N]`: tras exponer los pares candidatos, los juzga con el CLI del agente configurado. El agente compara el contenido completo de las memorias y devuelve un veredicto (`supersedes`, `conflicts_with`, `relates_to` o `none`); los veredictos se persisten con `judged_by='llm'` al usar `--apply`, y los juicios fallidos quedan pendientes para reintentar. Agente por defecto: `$SV_MEMORY_SEMANTIC_AGENT` o `claude`.
- `ignore <relation-id>`: marca un conflicto detectado como ignorado.

#### 29. `sv-memory context <path>`

- Imprime un **context pack compacto** para un archivo, paquete o símbolo: el rol estructural del nodo (tipo, fan-in/fan-out, comunidad, flag de hub) más las memorias vinculadas a esa ruta (`where_path` o aristas `rationale_for`), cada una como título + `why` truncado. Flags: `--max-memories N` (default 5), `--why-chars N` (default 300). Rápido y acotado — es el punto de entrada que llama el hook opcional de inyección de contexto en la primera lectura de archivo.

---

## 5. Esquema de Base de Datos

La base de datos reside en `~/.config/sv-memory/storage.db`. Todos los esquemas usan `IF NOT EXISTS` para la idempotencia.

```sql
-- Projects Registry
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Persistent Decision Memories
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    category TEXT NOT NULL,  -- 'bugfix' | 'architecture' | 'standard' |
                            -- 'decision' | 'journal' | 'postmortem' |
                            -- 'discussion' | 'idea' | 'qa'
    what TEXT NOT NULL,
    why TEXT NOT NULL,
    where_path TEXT,
    learned TEXT NOT NULL,
    git_branch TEXT,
    git_commit TEXT,
    author TEXT,
    impact TEXT,
    errors_faced TEXT,
    next_steps TEXT,
    session_id TEXT,
    topic_key TEXT,
    revision_count INTEGER DEFAULT 1,
    duplicate_count INTEGER DEFAULT 0,
    last_seen_at DATETIME,
    normalized_hash TEXT,
    deleted_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Full-Text Search (FTS5) Virtual Table
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    what,
    why,
    learned,
    content=memories,
    content_rowid=rowid
);

-- FTS5 sync triggers (auto-sync on INSERT/UPDATE/DELETE)
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, what, why, learned)
    VALUES (new.rowid, new.what, new.why, new.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned)
    VALUES('delete', old.rowid, old.what, old.why, old.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned)
    VALUES('delete', old.rowid, old.what, old.why, old.learned);
    INSERT INTO memories_fts(rowid, what, why, learned)
    VALUES (new.rowid, new.what, new.why, new.learned);
END;

-- Graph Nodes (Files, Symbols, Packages)
CREATE TABLE IF NOT EXISTS graph_nodes (
    project_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL,  -- 'file' | 'document' | 'sql' | 'package' |
                             -- 'function' | 'class' | 'module' | 'component' |
                             -- 'service' | 'concept' | ...
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,            -- JSON payload
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Graph Edges (Directed Relationships)
CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,              -- 'imports' | 'calls' | 'depends_on' | 'references' | 'rationale_for'
    confidence TEXT NOT NULL DEFAULT 'EXTRACTED', -- 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
    source_location TEXT,                     -- Line numbers/ranges
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_id, target_id, relation_type)
);

-- File metadata cache (for incremental graph updates)
CREATE TABLE IF NOT EXISTS graph_files_meta (
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    mtime_ms INTEGER NOT NULL,
    size INTEGER NOT NULL,
    PRIMARY KEY(project_id, path),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Session tracking (coding session lifecycle)
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    goal TEXT,
    directory TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    summary TEXT,
    status TEXT DEFAULT 'active',
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Memory relations (conflict surfacing & supersedes timeline)
CREATE TABLE IF NOT EXISTS memory_relations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL, -- 'supersedes' | 'conflicts_with' | 'relates_to' |
                                -- 'potential_conflict' (candidate found by scan)
    status TEXT DEFAULT 'pending', -- 'pending' | 'judged' | 'ignored'
    score REAL,                 -- Jaccard similarity for potential_conflict
    reason TEXT,
    judged_by TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, source_id) REFERENCES memories(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES memories(project_id, id) ON DELETE CASCADE
);
```

> Nota: en bases de datos anteriores, las columnas `status` y `score` se agregan de forma idempotente mediante la migración en `internal/db/migrations.go`.

### Índices de Rendimiento

```sql
CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);
CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(project_id, topic_key);
CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(project_id, normalized_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_active_started ON sessions(project_id, started_at DESC) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_completed_ended ON sessions(project_id, ended_at DESC) WHERE status = 'completed';
CREATE INDEX IF NOT EXISTS idx_memory_relations_source ON memory_relations(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_memory_relations_target ON memory_relations(project_id, target_id);
```

### Configuración de PRAGMA de SQLite

| PRAGMA         | Valor     | Propósito                                                      |
| -------------- | --------- | -------------------------------------------------------------- |
| `journal_mode` | WAL       | Write-Ahead Logging: lecturas concurrentes mientras se escribe |
| `synchronous`  | NORMAL    | Durabilidad/velocidad equilibradas (crash-safe con WAL)        |
| `temp_store`   | MEMORY    | Tablas temporales en RAM                                       |
| `cache_size`   | -20000    | Caché de páginas de ~20 MB por conexión                        |
| `mmap_size`    | 268435456 | I/O mapeado en memoria de 256 MB (evita syscalls `read()`)     |
| `busy_timeout` | 5000      | Espera 5s ante un lock en lugar de fallar                      |
| `foreign_keys` | ON        | Aplica integridad referencial                                  |

### Pool de Conexiones

- **Escritor:** `MaxOpenConns=1` (escrituras serializadas bajo WAL)
- **Lector:** `MaxOpenConns=16` (lecturas paralelas, `?mode=ro` para concurrencia sin locks; tiempo de vida de conexión ilimitado para mantener lectores WAL activos, poda de inactivas mediante `ConnMaxIdleTime`)
- **Degradación:** Si el lector falla al abrir, `Reader == Writer` (correcto pero más lento)

---

## 6. Definición de Herramientas MCP

`sv-memory` registra **31 herramientas MCP** para agentes de IA:

### 1. `sv_mem_save`

Persiste una decisión arquitectónica clave, una corrección de bug, un diario de progreso o una pauta estándar.

- **Parámetros:**
  - `category` (string, requerido): `bugfix` | `architecture` | `standard` | `decision` | `journal` | `postmortem` | `discussion` | `idea` | `qa`
  - `what` (string, requerido): Descripción concisa.
  - `why` (string, requerido): Razonamiento detallado.
  - `learned` (string, requerido): Regla o lección clave.
  - `where_path` (string, opcional): Archivo o módulo afectado.
  - `impact` (string, opcional): Qué salió bien.
  - `errors_faced` (string, opcional): Errores o bloqueos.
  - `next_steps` (string, opcional): Tareas pendientes.
  - `topic_key` (string, opcional): Clave estable para semántica de upsert (actualiza en el lugar).
  - `session_id` (string, opcional): ID de sesión asociado (se auto-detecta si se omite).

### 2. `sv_mem_update`

Actualiza parcialmente una memoria existente por ID. Solo cambian los campos proporcionados; los campos de identidad (id, category, session, topic_key) se conservan y el contador de revisión avanza.

- **Parámetros:**
  - `id` (string, requerido): El ID de la memoria a actualizar.
  - `what` (string, opcional): Nueva descripción concisa.
  - `why` (string, opcional): Nuevo razonamiento detallado.
  - `learned` (string, opcional): Nueva regla o lección clave.
  - `where_path` (string, opcional): Nueva ruta de archivo/carpeta afectada (cadena vacía la limpia).
  - `impact` (string, opcional): Nuevos logros/lo que salió bien (cadena vacía lo limpia).
  - `errors_faced` (string, opcional): Nuevos errores/bloqueos (cadena vacía los limpia).
  - `next_steps` (string, opcional): Nuevas tareas pendientes (cadena vacía las limpia).

### 3. `sv_mem_suggest_topic_key`

Genera un topic key estable en formato kebab-case antes de guardar.

- **Parámetros:**
  - `category` (string, requerido): Categoría de la memoria.
  - `what` (string, requerido): Título o descripción.
- **Devuelve:** Una clave sugerida como `category/kebab-case-description`.

### 4. `sv_mem_session_start`

Registra una nueva sesión de codificación. Devuelve el Auto-Boot Context Bundle (resumen de la sesión anterior, decisiones clave, estándares, bugfixes recientes, postmortems, Q&A reciente, últimos diarios, hubs del grafo) limitado por el presupuesto de tokens. Cuando se proporciona un `goal`, las decisiones/estándares/bugfixes mostrados se ordenan por relevancia a él en lugar de por mera recencia.

- **Parámetros:**
  - `goal` (string, opcional): Objetivo de la sesión. Al definirlo, el Auto-Boot bundle ordena los candidatos de cada sección por relevancia a él (pinned primero, luego solapamiento de keywords, luego recencia).
  - `directory` (string, opcional): Directorio de trabajo.
  - `semantic` (string, opcional): Cuando es `'true'` y hay un `goal`, reordena los candidatos del bundle con el CLI del agente configurado según relevancia semántica (una llamada en lote; falla de forma segura al ranking de keywords determinista si el agente no está disponible). Por defecto `'false'`.
  - `semantic_agent` (string, opcional): CLI del agente para recall semántico. Por defecto `$SV_MEMORY_SEMANTIC_AGENT`, luego `claude`.
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 5. `sv_mem_session_end`

Cierra una sesión activa.

- **Parámetros:**
  - `session_id` (string, requerido): ID de la sesión.
  - `summary` (string, opcional): Logros.

### 6. `sv_mem_session_summary`

Actualiza objetivos, descubrimientos y próximos pasos de la sesión.

- **Parámetros:**
  - `session_id` (string, requerido): ID de la sesión.
  - `goal` (string, opcional): Objetivo actualizado.
  - `discoveries` (string, opcional): Hallazgos.
  - `accomplished` (string, opcional): Tareas completadas.
  - `next_steps` (string, opcional): Objetivos próximos.
  - `files` (string, opcional): Lista de archivos editados.

### 7. `sv_mem_context`

Recupera el contexto de la última sesión completada.

- **Parámetros:**
  - `limit` (string, opcional): Máximo de memorias a recuperar (por defecto `10`).
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 8. `sv_mem_compact`

Activa la compactación automática de memoria: consolida revisiones históricas de topic keys y duplicados en registros de síntesis limpios.

- **Parámetros:** Ninguno.

### 9. `sv_mem_search` (Capa 1 Divulgación Progresiva)

Búsqueda de memoria basada en FTS5. Devuelve solo IDs, categorías, fechas, títulos y topic keys.

- **Parámetros:**
  - `query` (string, requerido): Términos de búsqueda por palabras clave.
  - `category` (string, opcional): Filtro por categoría.
  - `path` (string, opcional): Filtro de alcance por ruta/directorio para limitar memorias relevantes a un archivo o directorio específico. Con `graph_boost` (default `true`), el recall se expande a toda la comunidad del grafo de esa ruta: los resultados de todo el módulo surgen en una sola llamada, y las filas expandidas por comunidad se anotan con un marcador `[graph]`.
  - `limit` (string, opcional): Máximo de resultados (por defecto `10`).
  - `offset` (string, opcional): Desplazamiento de paginación.
  - `match_mode` (string, opcional): `'all'` (por defecto) exige que cada token coincida; `'any'` devuelve memorias que coinciden con uno o más tokens para una recuperación más amplia.
  - `semantic` (string, opcional): Cuando es `'true'`, reordena los candidatos por palabras clave con el CLI del agente configurado según relevancia semántica (opt-in, una sola llamada en lote; falla de forma segura a resultados por palabras clave si el agente no está disponible). Por defecto `'false'`.
  - `semantic_agent` (string, opcional): CLI del agente para recall semántico. Por defecto `$SV_MEMORY_SEMANTIC_AGENT`, luego `claude`.
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 10. `sv_mem_timeline` (Capa 2 Divulgación Progresiva)

Recupera una lista cronológica de observaciones centradas en una memoria específica.

- **Parámetros:**
  - `observation_id` (string, requerido): ID de la memoria.
  - `before` (string, opcional): Cantidad de memorias precedentes (por defecto `5`).
  - `after` (string, opcional): Cantidad de memorias posteriores (por defecto `5`).
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 11. `sv_mem_get` (Capa 3 Divulgación Progresiva)

Recupera todos los campos de una memoria específica. Los campos de texto se truncan más allá de `max_chars`.

- **Parámetros:**
  - `id` (string, requerido): ID de la memoria.
  - `max_chars` (string, opcional): Máximo de caracteres por campo (por defecto `1000`; `'0'` = ilimitado).
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 12. `sv_mem_judge`

Crea una relación (juicio) entre dos memorias para mantener la continuidad o registrar conflictos.

- **Parámetros:**
  - `source_id` (string, requerido): ID de la memoria más nueva.
  - `target_id` (string, requerido): ID de la memoria más antigua.
  - `relation_type` (string, requerido): `supersedes` | `conflicts_with` | `relates_to`.
  - `reason` (string, opcional): Razonamiento.
  - `judged_by` (string, opcional): Identidad del juez (por defecto `'agent'`).

### 13. `sv_mem_compare`

Compara dos memorias lado a lado en formato Markdown.

- **Parámetros:**
  - `id1` (string, requerido): ID de la primera memoria.
  - `id2` (string, requerido): ID de la segunda memoria.

### 14. `sv_mem_review`

Encuentra memorias que necesitan mantenimiento (p. ej. obsoletas, conteos de duplicados excesivos, candidatas a consolidación) o reinicia el plazo de revisión de política de una memoria.

- **Parámetros:**
  - `action` (string, opcional): `'list'` (por defecto) o `'mark_reviewed'`.
  - `id` (string, opcional): Requerido para `action='mark_reviewed'`: el ID de la memoria a marcar como revisada. Reinicia `review_after` a `now + decay(category)`.

### 15. `sv_mem_stats`

Proporciona métricas agregadas (conteos, desglose por categoría) más el proyecto activo actual (ID, nombre y ruta), y el **ledger de tokens de sesión**: una estimación de los tokens (chars/4) inyectados en el contexto del agente desde el último `sv_mem_session_start` (Auto-Boot bundle + tools de lectura masiva), junto con el presupuesto `max_response_tokens` — para que el agente decida cuándo compactar.

- **Parámetros:**
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 16. `sv_mem_diagnose`

Ejecuta chequeos de salud de solo lectura para el proyecto activo: archivo de base de datos, tablas del esquema, triggers FTS5, registro del proyecto, permisos de escritura, directorio de chunks e integridad estructural del grafo (aristas colgantes, nodos huérfanos, self-loops, archivos faltantes). Combina los checks de `RunDiagnostics` de memoria con `graph.DiagnoseGraph`.

- **Parámetros:**
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 17. `sv_mem_delete`

Elimina una memoria. Por defecto hace soft-delete; establece `hard` en `'true'` para borrarla permanentemente.

- **Parámetros:**
  - `id` (string, requerido): ID de la memoria.
  - `hard` (string, opcional): `'true'` para eliminación permanente.

### 18. `sv_mem_pin`

Fija una memoria local para que aparezca primero en `sv_mem_context` (las decisiones clave permanecen visibles), o la desfija con `action='unpin'`. El estado de fijación es local a este dispositivo.

- **Parámetros:**
  - `id` (string, requerido): ID de la memoria.
  - `action` (string, opcional): `'pin'` (por defecto) o `'unpin'`.

### 19. `sv_mem_capture_passive`

Registra automáticamente una entrada de diario ligera (p. ej., resultados de tests, cambios de archivos).

- **Parámetros:**
  - `what` (string, requerido): Descripción resumida.
  - `why` (string, requerido): Contexto o justificación.

### 19b. `sv_mem_context_pack`

Construye un **context pack compacto y fusionado** para una ruta de código (archivo, paquete o símbolo): el rol estructural del nodo en el grafo de dependencias (tipo, fan-in/fan-out, comunidad, flag de hub) más las memorias vinculadas a esa ruta vía `where_path` o aristas `rationale_for` (decisiones, estándares, bugfixes), cada una renderizada como título + `why` truncado. Una sola llamada acotada reemplaza los round-trips de `sv_graph_explain` + `sv_mem_search path=` + varios `sv_mem_get`, ahorrando tokens. Es el puente propietario grafo→memoria que alimenta el hook opcional de inyección silenciosa de contexto.

- **Parámetros:**
  - `path` (string, requerido): Ruta de archivo, nombre de paquete o símbolo a resolver.
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).
- **Config:** `context_pack_max_memories` (default `5`, máx `20`) limita las memorias vinculadas; cada `why` se trunca a `bundle_why_chars`.

### 19c. `sv_mem_capture_prompt`

Captura el **prompt del usuario** como observación local asociada a una sesión (paridad con `mem_save_prompt` de Engram). Registra qué pidió el usuario para que las futuras sesiones tengan contexto de sus objetivos tras la compactación.

- **Parámetros:**
  - `content` (string, requerido): El texto del prompt del usuario. Los secretos se redactan antes de escribir.
  - `session_id` (string, opcional): Sesión a la que asociar el prompt; por defecto la sesión activa.
- **Almacenamiento:** los prompts viven en la tabla local `user_prompts` de SQLite (indexada por FTS5) y **no** forman parte del payload de git sync en esta fase — son solo locales. Recuperables vía `sv_mem_context` (prompts recientes de la última sesión) y contabilizados por `sv_mem_stats` (`Total user prompts`).

### 19d. `sv_mem_merge_projects`

Fusiona variantes de nombre de proyecto en un único proyecto canónico (paridad con `mem_merge_projects` de Engram, admin). Mueve todas las memorias, sesiones, relaciones y datos del grafo de `from` a `to`, y luego borra el proyecto origen.

- **Parámetros:**
  - `from` (string, requerido): ID del proyecto origen del que mover datos y luego borrar.
  - `to` (string, requerido): ID del proyecto destino que recibe los datos.
- **Notas:** refleja el CLI `sv-memory projects consolidate <origen> <destino>`. Ambos proyectos deben existir y ser distintos.

### 20. `sv_graph_query`

Consulta relaciones estructurales mediante Búsqueda en Anchura (BFS). Por defecto devuelve una lista de edges textual compacta y eficiente en tokens (`source →[rel]→ target`); pasa `mermaid=true` para renderizar un diagrama Mermaid en su lugar.

- **Parámetros:**
  - `path_or_node` (string, requerido): Ruta de archivo o módulo central.
  - `depth` (string, opcional): Distancia de salto (por defecto `1`).
  - `relation_type` (string, opcional): Filtro (p. ej., `'imports'`, `'calls'`, `'depends_on'`).
  - `direction` (string, opcional): Dirección del recorrido: `'in'` | `'out'` | `'all'` (por defecto `'out'`).
  - `mermaid` (string, opcional): Renderiza los edges como diagrama Mermaid en lugar de la lista textual compacta (por defecto `'false'`).
  - `token_budget` (string, opcional): Máximo de tokens para la respuesta; la respuesta se trunca con un aviso al superarse (por defecto desde config `max_response_tokens`, 4000; `'0'` = ilimitado).

### 21. `sv_graph_path`

Encuentra la ruta de dependencia más corta entre dos nodos del grafo.

- **Parámetros:**
  - `source` (string, requerido): ID del nodo origen.
  - `target` (string, requerido): ID del nodo destino.
  - `max_hops` (string, opcional): Límite de saltos (por defecto `10`).

### 22. `sv_graph_sync`

Activa un escaneo incremental de archivos modificados para sincronizar nodos/aristas. Invalida el caché.

- **Parámetros:** Ninguno.

### 23. `sv_mem_conflicts`

Detecta y expone memorias conflictivas con análisis de superposición semántica, y puede juzgar pares candidatos con LLM.

- **Parámetros:**
  - `action` (string, requerido): Acción a realizar: `list`, `scan` o `ignore`.
  - `status` (string, opcional): Filtro de estado para `list` (`pending`, `judged`, `ignored`).
  - `relation_id` (string, opcional): Requerido para `ignore`: el ID de la relación de conflicto a ignorar.
  - `threshold` (string, opcional): Umbral de similitud para `scan` (por defecto `0.45`).
  - `apply` (string, opcional): Para `scan`: `'true'` para guardar los conflictos escaneados / juicios semánticos en la base de datos (por defecto `'false'`).
  - `semantic` (string, opcional): Para `scan`: `'true'` para juzgar los conflictos candidatos con el CLI del agente (por defecto `'false'`).
  - `agent` (string, opcional): CLI del agente para el juicio semántico (`claude`, `opencode` o un comando personalizado; por defecto `$SV_MEMORY_SEMANTIC_AGENT` o `claude`).
  - `max_semantic` (string, opcional): Máximo de pares candidatos a juzgar (por defecto: todos).
  - `concurrency` (string, opcional): Juicios de agente en paralelo (por defecto `3`).

### 24. `sv_graph_explain`

Imprime información detallada de un nodo específico del grafo: tipo, etiqueta, ruta, metadatos y métricas fan-in/fan-out.

- **Parámetros:**
  - `node` (string, requerido): Ruta de archivo o ID del nodo.

### 25. `sv_graph_god_nodes`

Identifica los nodos más conectados del grafo según centralidad de intermediación (betweenness) y grado. Devuelve una lista clasificada de nodos god con métricas.

- **Parámetros:**
  - `top_n` (string, opcional): Máximo de resultados a devolver (por defecto `10`).

### 26. `sv_graph_surprising_connections`

Encuentra rutas de dependencia no obvias o inesperadas en el grafo. Resalta anomalías estructurales que pueden indicar preocupaciones arquitectónicas.

- **Parámetros:**
  - `limit` (string, opcional): Máximo de conexiones a devolver (por defecto `10`).

### 27. `sv_graph_viz`

Genera una visualización HTML interactiva del grafo usando vis.js con colores por comunidad, simulación física, filtrado de nodos y tooltips.

- **Parámetros:**
  - `output` (string, opcional): Ruta del archivo de salida (por defecto `graph.html`).

### 28. `sv_graph_merge`

Fusiona dos grafos de proyecto en uno (union-merge por ID de nodo), actualizando nodos y aristas.

- **Parámetros:**
  - `project_a` (string, requerido): ID del primer proyecto.
  - `project_b` (string, requerido): ID del segundo proyecto.
  - `output` (string, opcional): Ruta del archivo JSON de salida.

---

## 7. Estrategias de Guardado de Memoria (Detalle)

### Topic Key Upsert (Temas Evolutivos)

Cuando se proporciona `topic_key`:

1. Consulta: `SELECT id, revision_count FROM memories WHERE project_id = ? AND topic_key = ?`
2. Si existe: `UPDATE` de la fila existente, `revision_count++`, actualización de todos los campos.
3. Si no existe: Cae a la inserción con `revision_count = 1`.

Caso de uso: funcionalidades de larga duración, patrones arquitectónicos recurrentes, estándares en evolución.

### Deduplicación de Ventana Móvil (Mismo Contenido, Ventana Corta)

Cuando NO se proporciona `topic_key`:

1. Calcula el hash SHA256 de `what + "\x00" + why + "\x00" + learned + "\x00" + where_path`.
2. Consulta: `SELECT id, duplicate_count FROM memories WHERE project_id = ? AND normalized_hash = ? AND category = ? AND created_at > datetime('now', '-24 hours')`
3. Si existe: `UPDATE duplicate_count++`, actualiza `last_seen_at`. Sin nueva fila.
4. Si no existe: Inserta una nueva fila.

Caso de uso: múltiples agentes guardando el mismo hecho dentro de una sesión.

### Sanitización de Seguridad

Antes de cada guardado, 7 patrones regex redactan:

- Claves de API de OpenAI (`sk-...`)
- Claves de Anthropic (`sk-ant-sid...`)
- Claves de Gemini (`AIzaSy...`)
- Tokens JWT
- Bloques de claves privadas RSA/EC
- Cadenas de conexión a bases de datos
- Asignaciones genéricas de `password`/`secret`/`token`/`api_key`

Todo se reemplaza con `[REDACTED_SECRET]`. Se conservan los nombres de las claves en las asignaciones.

**La redacción se aplica de extremo a extremo, no solo en el guardado:**

- **Escrituras:** `SanitizeText` corre en el camino normal de guardado/actualización, en los resúmenes de sesión (`EndSession`, `SaveSessionSummary`), en relaciones/juicios y vía el helper compartido `sanitizeMemoryFields` en todos los caminos de importación (`ImportJSON`, import de chunks git, `memories.json` monolítico).
- **Grafo:** el texto de nodos derivado de contenido (encabezados markdown, comentarios rationale `TODO:`/`WHY:`, defaults/valores de DDL SQL) se redacta al parsearse y de nuevo en `sanitizeNodeForPersist` antes del upsert en `graph_nodes`. Los archivos `.env`/key/PEM/credentials nunca se escanean (no están en las extensiones soportadas), y el `.sv-memoryignore` por defecto excluye además `.env*`, `*.pem`, `*.key`, `*.p12`, `id_rsa*`, `credentials*`, `.ssh/`, `.aws/`, `.gcp/`, `secrets.yaml`.
- **Lecturas/exportaciones:** search/get, el paquete Auto-Boot, el contexto de sesión, los exportadores de bóveda Obsidian, wiki y Cypher re-aplican la redacción para que los valores saneados nunca vuelvan a aparecer crudos. El `graph.html` generado escapa el id/tipo/ruta/metadata de cada nodo, cerrando el sink de XSS almacenado en el panel de detalle.

El almacén SQLite vive fuera del repositorio (`~/.config/sv-memory/storage.db`), de modo que solo los chunks JSON por memoria (ya redactados) se commitean a Git.

---

## 8. Plantilla de Protocolo del Agente

Al inicializarse, `sv-memory` inyecta el siguiente bloque de protocolo en `AGENTS.md`, `.cursorrules` o `.windsurfrules` (fuente de la verdad: `internal/protocol/protocol.go`):

```markdown
<!-- SV-MEMORY:START -->

# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Session Lifecycle (REQUIRED, in this order):

1. **Start:** Call 'sv_mem_session_start' at the beginning of work. It returns an **Auto-Boot Context Bundle** with the previous session summary, key architectural decisions, standards, recent bugfixes, postmortems, recent Q&A, last journals, and top graph hubs read it and use it as your starting context.
2. **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
3. **Capture knowledge as you go:** Save journals, decisions, standards, and bugfixes with 'sv_mem_save' (see the Memory Capture Guidelines below). Use 'sv_mem_capture_passive' for lightweight observations that do not need an explicit save decision.
4. **Summary:** Call 'sv_mem_session_summary' with goal, discoveries, accomplished work, and next steps before closing.
5. **End:** Call 'sv_mem_session_end' to mark the session as completed and enable context recovery in the next session.

After a compaction or context reset, call 'sv_mem_context' to recover the last session state (goal, summary, associated memories).

## Tool Usage in Any Mode:

The sv-memory tools (session, memory, graph, diagnostics) may be called in ANY operational mod plan, build, or review. They persist only to the project memory store ('.sv-memory/'), which is project data, not source code. Do not skip memory capture, context recovery, or the session lifecycle because of the current mode.

## Context Initialization (Search-Before-Work):

Memory must be consulted before proposing or executing changes:

- **Orientation:** On a new project, call 'sv_mem_stats' first it is the cheapest overview of memory distribution (categories, counts, sessions).
- **Targeted search:** Call 'sv_mem_search' with the topic keywords of your task (feature, component, style, module). Filter by category when relevant ('journal', 'postmortem', 'discussion', 'idea', 'qa', 'architecture', 'decision'). Avoid repeating redundant searches the Auto-Boot Bundle already carries the previous session context.
- **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding. Never answer from assumptions alone memory first, code second.

## Progressive Disclosure (Token-Efficient Retrieval):

Use the 3-layer pattern to minimise tokens:

- **Layer 1 Search:** Call 'sv_mem_search' to get a compact list (IDs + titles + topic keys) of relevant memories (~30 tokens/result).
- **Layer 2 Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory (includes the central observation rationale).
- **Layer 3 Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
  Never dump all fields from search drill down on demand. The top search result is already expanded inline, so only drill further when you need deeper detail.

## Topic Keys (Upsert Semantics):

- Use 'sv_mem_suggest_topic_key(category, what)' to generate a stable 'category/kebab-case' key.
- Pass 'topic_key' to 'sv_mem_save' to enable upsert: saves to the same project+topic update in place (revision_count++) instead of creating a new record.
- Use topic keys for evolving topics (architecture decisions, design systems, long-running features, recurring patterns). Skip for one-off bugs or single facts.
- **Convention:** Always kebab-case in English. Examples: 'standard/design-system', 'architecture/component-card', 'decision/use-bun-instead-of-npm', 'standard/workflow-git-commits', 'bugfix/tab-transition-absolute-position'.

## Memory Capture Guidelines (when to save what):

Always persist design knowledge as structured memories with a topic_key, not just session journals:

| Situation                                            | Category       | topic_key example             |
| :--------------------------------------------------- | :------------- | :---------------------------- |
| Visual style / design system / CSS / Tailwind tokens | 'standard'     | standard/design-system        |
| Reusable component or UI pattern                     | 'architecture' | architecture/component-card   |
| Workflow / methodology / build & dev process         | 'standard'     | standard/workflow-dev-process |
| Architectural decision made (and its rationale)      | 'decision'     | decision/...                  |
| Code convention / naming / folder structure          | 'standard'     | standard/code-conventions     |
| Complex or non-obvious bug fixed                     | 'bugfix'       | bugfix/...                    |
| Relevant Q&A with lasting value                      | 'qa'           | qa/...                        |
| Rejected library or framework feature                | 'decision'     | decision/avoid-...            |
| Session progress checkpoint                          | 'journal'      | journal/...                   |

**Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as 'standard' or 'architecture' with a topic_key. A journal is not a substitute journals document progress, 'standard'/'architecture'/'decision' preserve the "how" and the "why" for future sessions.

## Graph Inspection (before modifying code):

- **Orient before touching code:** Call 'sv_graph_god_nodes' to see the most-connected hub nodes these are the architectural hotspots any change may ripple through.
- **Understand a module:** Call 'sv_graph_explain(node=...)' before refactoring, deleting, or restructuring a file/module. It reports the node's role, community, centrality, fan-in/fan-out, neighbors, and suggested questions.
- **Inspect dependencies:** Call 'sv_graph_query(path_or_node=...)' to see a module's dependency sub-graph (imports/calls/depends_on) with depth, direction, and relation-type filters.
- **Trace a connection:** Call 'sv_graph_path(source=..., target=...)' to find the shortest dependency path between two nodes.

## Graph Refresh:

Execute 'sv_graph_sync' after adding major new files, creating new packages, or modifying package structures/imports. The graph is rebuilt incrementally and communities/centrality are computed lazily when queried.

## Memory Maintenance (periodic):

- **Review:** Call 'sv_mem_review' to list stale, duplicate, or consolidation-candidate memories.
- **Conflicts:** Call 'sv_mem_conflicts action=scan' to detect potential duplicate memories; judge them with 'sv_mem_judge' (supersedes / conflicts_with / relates_to) or with 'action=scan semantic=true' to LLM-judge candidates via the agent CLI. Keep relations coherent.
- **Compare:** Call 'sv_mem_compare(id1, id2)' before judging two similar memories.
- **Compact:** Call 'sv_mem_compact' periodically or after many topic-key upserts to consolidate revisions and keep search fast.

## Tool Quick Reference:

- **Session:** sv_mem_session_start, sv_mem_session_summary, sv_mem_session_end, sv_mem_context
- **Memory CRUD:** sv_mem_save, sv_mem_update, sv_mem_get, sv_mem_delete, sv_mem_search, sv_mem_timeline
- **Pin / Priority:** sv_mem_pin (action='unpin' to clear)
- **Knowledge quality:** sv_mem_suggest_topic_key, sv_mem_judge, sv_mem_compare, sv_mem_compact, sv_mem_review, sv_mem_capture_passive, sv_mem_conflicts, sv_mem_stats, sv_mem_diagnose
- **Graph:** sv_graph_query, sv_graph_explain, sv_graph_god_nodes, sv_graph_path, sv_graph_sync, sv_graph_surprising_connections, sv_graph_viz, sv_graph_merge

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): description'). Use the project's configured commit language (default: English), unless the project specifies otherwise.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->
```

---

## 9. Estructura del Proyecto

```text
sv-memory/
├── cmd/
│   └── sv-memory/
│       ├── main.go              # Registro del comando raíz de Cobra, versión y ejecución CLI
│       ├── cmd_init.go          # Subcomandos init, mcp, tui
│       ├── cmd_memory.go        # diagnose, stats, export, import, sync, obsidian-export
│       ├── cmd_projects.go      # delete session/project, projects list/prune/consolidate
│       ├── cmd_graph.go         # graph rebuild/path/explain/communities/wiki/viz/merge
│       ├── cmd_conflicts.go     # conflicts list/stats/scan/ignore
│       ├── cmd_configure.go     # Asistente interactivo de configuración
│       ├── cmd_config.go        # configure get/set/list (viper YAML)
│       ├── cmd_permissions.go   # permissions list/status/grant/revoke
│       ├── cmd_hooks.go         # hooks install/uninstall/status
│       └── cmd_update.go        # Comando de auto-actualización
├── internal/
│   ├── config/                  # Rutas de la app, parseo de ajustes, config viper y asistente de configuración
│   ├── db/                      # Inicialización de BD, migraciones compuestas, pools WAL y PRAGMAs
│   ├── graph/                   # Escáner de código, consulta BFS, comunidades Leiden,
│   │   │                        # centralidad de intermediación, nodos god, viz HTML, wiki,
│   │   │                        # graph merge, conexiones sorprendentes, actualizaciones incrementales
│   │   ├── extractor/           # Extractor tree-sitter, respaldo regex, semántica markdown
│   │   └── schema/              # Estructuras Node/Edge
│   ├── hook/                    # Generación y plantillas de hooks PreToolUse
│   ├── mcp/                     # Servidor MCP + 31 handlers de herramientas; lee del caché LRU de internal/graph
│   ├── memory/                  # CRUD, almacenamiento de sesiones, dedup, conflictos, compactación,
│   │                            # git sync por chunks, exportación Obsidian/Cypher, stats
│   ├── perm/                    # Gestión de allow-lists de herramientas MCP (antigravity/claude-code)
│   ├── protocol/                # Inyección de reglas AGENTS.md / editores
│   ├── security/                # Sanitizador de secretos por regex
│   └── tui/                     # UI de terminal interactiva (charmbracelet/huh + bubbletea)
├── documentation/
│   ├── requirement.md           # Restricciones del producto
│   ├── spect.md                 # Esta especificación
│   ├── CODEBASE-GUIDE.md        # Tour del código: flujos de datos clave, puntos de extensión
│   └── getting_started_guide.md # Guía paso a paso de instalación y onboarding
├── AGENTS.md                    # Bloque de protocolos inyectado (commiteado)
├── CHANGELOG.md                 # Notas de release
├── CONTRIBUTING.md              # Guía de contribución
├── SECURITY.md                  # Política de reporte de vulnerabilidades
├── CODE_OF_CONDUCT.md           # Código de conducta de la comunidad
├── Makefile                     # Targets build/test/lint/vet/install
├── .golangci.yml                # Configuración del linter
├── .github/workflows/           # Pipelines CI + release
├── install.sh                   # Script de instalación para Unix
├── install.ps1                  # Script de instalación para Windows
├── .sv-memory/
│   └── chunks/                  # JSON por memoria compartido por el equipo (commiteado)
├── go.mod
├── go.sum
└── README.md                    # Introducción principal del proyecto
```

---

## 10. Soporte de Lenguajes para el Grafo de Dependencias

El parseo usa **tree-sitter** (`gotreesitter`) para los lenguajes principales, con un respaldo por regex para el resto. El escáner también maneja `Markdown` (encabezados, bloques de código, wikilinks) y `SQL`.

| Lenguaje   | Extensiones   | Mecanismo de Detección de Imports                                    |
| ---------- | ------------- | -------------------------------------------------------------------- |
| Go         | `.go`         | tree-sitter (`import "path"`)                                        |
| Python     | `.py`         | tree-sitter (`import x`, `from x import y`)                          |
| JavaScript | `.js`, `.jsx` | tree-sitter (`import ... from`, `require()`, `import()`)             |
| TypeScript | `.ts`, `.tsx` | tree-sitter (types, generics, type annotations)                      |
| PHP        | `.php`        | tree-sitter (`use Namespace`, `include`, `require`)                  |
| Rust       | `.rs`         | tree-sitter (`use path`, `mod path`, `extern crate`)                 |
| Ruby       | `.rb`         | tree-sitter (`require`, `load`, `require_relative`)                  |
| Java       | `.java`       | tree-sitter (`import package`)                                       |
| HTML       | `.html`       | tree-sitter (script src, link stylesheet)                            |
| CSS        | `.css`        | tree-sitter (`@import 'path'`, `@import url(...)`)                   |
| Bash       | `.sh`         | tree-sitter (`source`, `. script.sh`)                                |
| Astro      | `.astro`      | regex (bloque de imports del frontmatter)                            |
| Vue        | `.vue`        | regex (imports del bloque `<script>`)                                |
| Svelte     | `.svelte`     | regex (imports del bloque `<script>`)                                |
| Lua        | `.lua`        | regex (`require()`, `dofile()`, `loadfile()`)                        |
| Markdown   | `.md`         | regex + parser semántico (encabezados, bloques de código, wikilinks) |
| SQL        | `.sql`        | a nivel de escáner (referencias a tablas/columnas)                   |

---

## 11. Funcionalidades de Optimización de Tokens

| Funcionalidad                                                      | Mecanismo                                                                                                                                   | Ahorro Estimado                                                           |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Divulgación progresiva de 3 capas                                  | La búsqueda devuelve filas compactas (~30 tokens/resultado); el contenido completo a demanda                                                | 60-80% de los tokens de respuesta                                         |
| Compactación de sesión                                             | Conversación completa → entrada de diario estructurada (200-500 tokens)                                                                     | 80-90% frente al historial crudo                                          |
| Truncación de campos (`sv_mem_get`)                                | Límite `max_chars` por campo de texto (por defecto 1000)                                                                                    | Evita el consumo de tokens sin límite                                     |
| Umbrales de truncación configurables                                | Las claves de config `max_field_chars`, `search_expand_chars`, `timeline_why_chars`, `bundle_why_chars` sobreescriben los límites compilados vía YAML | Ajusta el tamaño de las respuestas sin recompilar                   |
| Guarda de tokens en arranque de sesión (`sv_mem_session_start`)     | Auto-Boot Bundle + Graph Hubs limitados por `max_response_tokens` / `token_budget` por llamada                                               | Acota el payload pre-herramienta más grande de cada sesión                |
| Context Pack (`sv_mem_context_pack`)                                | Una llamada acotada fusiona el rol del grafo + memorias vinculadas para una ruta (`where_path`/`rationale_for`), reemplazando los round-trips de explain+search+get | 1 llamada en vez de 3+; solo título + `why` truncado                     |
| Inyección silenciosa de contexto (hooks `--context-injection`)       | La 1ª Read de Claude Code inyecta `sv-memory context <file>` como additionalContext (acotado a 3 memorias, cacheado por archivo)                                      | Contexto relevante en el momento exacto, sin round-trip de búsqueda        |
| Topic key upsert                                                   | Actualización en el lugar en lugar de acumular revisiones                                                                                   | 50% menos resultados de búsqueda redundantes                              |
| Deduplicación de ventana móvil                                     | Suprime guardados idénticos dentro de 24h                                                                                                   | Evita el crecimiento por duplicados                                       |
| SQL de búsqueda compacto                                           | SELECT de solo 7 columnas necesarias en lugar de las 20                                                                                     | ~60% menos I/O por búsqueda                                               |
| Benchmark de presupuesto de tokens (`BenchmarkToolResponseTokens`) | Guardia de regresión que mide bytes por llamada y tokens estimados para `sv_mem_search`, `sv_mem_get`, `sv_mem_timeline` y `sv_mem_context` | Protege contra el crecimiento sin límite de las respuestas entre releases |

---

## 12. Relaciones de Memoria y Exposición de Conflictos

Para mantener la coherencia en interacciones de agentes de largo plazo, `sv-memory` incluye un sistema de detección y resolución de conflictos.

- **La tabla `memory_relations`:** Rastrea cómo se relacionan dinámicamente los nodos de memoria.
- **Tipos de Relación:**
  - `supersedes`: Una decisión más nueva anula una pauta antigua (desactiva o marca la memoria objetivo).
  - `conflicts_with`: Dos decisiones se contradicen explícitamente. Se marcan para revisión manual.
  - `relates_to`: Asociación laxa entre memorias (p. ej., un estándar se relaciona con un bugfix).
  - `potential_conflict`: Un candidato detectado por el escaneo incremental, inicialmente con `status='pending'`.
- **Ciclo de Vida del Conflicto:**
  1. **Detección:** Ejecuta `sv-memory conflicts scan` (CLI) o `sv_mem_conflicts` con `action=scan` (MCP). El escaneo es incremental (O(nuevas memorias × total) en lugar de O(N²)), cachea las tokenizaciones e inserta como máximo `--max-insert N` (por defecto 100) relaciones. Por defecto solo reporta; `--apply` (o `apply='true'`) persiste las relaciones `potential_conflict` detectadas.
  2. **Revisión:** `sv-memory conflicts list` / `sv_mem_conflicts` con `action=list` muestra los conflictos pendientes. Llamar a `sv_mem_review` también resalta los conflictos pendientes.
  3. **Resolución:** Juzga el par con `sv_mem_judge` (promoviéndolo a `supersedes`/`conflicts_with`/`relates_to`), ejecuta un **escaneo semántico LLM** (`sv-memory conflicts scan --semantic` / `sv_mem_conflicts action=scan semantic=true`) que automatiza el mismo juicio con el CLI del agente configurado, o márcalo como revisado/ignorado con `sv-memory conflicts ignore <relation-id>` (o `action=ignore`).

---

## 13. Ciclo de Vida de las Sesiones

Las sesiones aíslan la creación de memoria por tareas y proporcionan un búfer continuo de logros.

```text
  Session Start              Observation Saves             Session Summary            Session End
┌───────────────┐          ┌───────────────────┐          ┌───────────────┐         ┌─────────────┐
│  Start timer  │ ───────> │  sv_mem_save /    │ ───────> │ Compaction &  │ ──────> │  End timer  │
│  Set goal     │          │  capture_passive  │          │ next steps    │         │  Set status │
└───────────────┘          └───────────────────┘          └───────────────┘         └─────────────┘
```

1. **Inicio:** `sv_mem_session_start` inicia un registro en `sessions` con estado `'active'`.
2. **Ejecución:** Todas las memorias guardadas durante este tiempo se vinculan automáticamente al `session_id` para agregar una línea de tiempo.
3. **Resumen:** El agente resume logros, descubrimientos, archivos modificados y próximos pasos mediante `sv_mem_session_summary`.
4. **Fin:** `sv_mem_session_end` actualiza el estado a `'completed'` y bloquea la sesión.

---

## 14. Unificación del Grafo Código-Memoria

Las entidades de código y las observaciones de memoria se mapean sobre un grafo dirigido unificado almacenado en SQLite:

- **Nodos de Entidad:** Los símbolos de código (funciones, clases, archivos) representan dependencias estructurales.
- **Nodos de Memoria:** Las decisiones y estándares se mapean en el mismo espacio topológico.
- **Aristas de Unificación (`rationale_for`):** Conectar una memoria a una entidad de código vincula el _Por qué_ directamente con el _Qué_. Recorrer el grafo de código mediante `sv_graph_query` recupera tanto los imports/calls relacionados como las decisiones asociadas, brindando a desarrolladores y agentes el contexto completo en el punto de interés.

**Conexión nativa memoria→código:** cuando una memoria se guarda con `sv_mem_save` (o se actualiza con `sv_mem_update`) y proporciona un `where_path`, sv-memory crea/actualiza automáticamente un nodo `document` del grafo para esa memoria (id = ID de la memoria) y una arista `rationale_for` hacia el nodo canónico de código en esa ruta (best-effort: no-op si el grafo aún no se construyó o la ruta es desconocida). Esto implica:

- `sv_graph_explain`/`sv_graph_query` sobre un archivo expone las decisiones/estándares asociados bajo los vecinos de rationale **Memory/Decision**.
- La exportación a bóveda Obsidian vincula cada nota de memoria con sus archivos de código mediante las mismas aristas.
- Tras un rebuild completo del grafo (`sv-memory graph rebuild`, `sv_graph_sync`), los enlaces se recrean automáticamente desde todas las memorias activas con `where_path`.

**Extracción de aristas `calls` (precisión AST):** las aristas `calls` se producen por archivo prefiriendo el AST de tree-sitter (nodos `call_expression` / `call` / `method_invocation` / `function_call_expression`) con confianza `EXTRACTED` y una ubicación precisa `L<línea>:<columna>`, resolviendo cada sitio de llamada contra los nodos de función/clase del proyecto (mismo archivo primero, luego coincidencia única cross-file dentro del grupo de lenguaje). Los archivos cuyo lenguaje no tiene cobertura AST de llamadas (Go — workaround del bug de stack overflow del parser upstream, Lua, Markdown, shell, bloques script de Vue/Svelte/Astro) caen al heurístico de tokenize con confianza `INFERRED`. La ruta AST no captura identificadores dentro de strings o comentarios, eliminando una clase de falsos positivos que el heurístico produce. Esto mejora la precisión de `sv_graph_query`, `sv_graph_explain`, god nodes y la detección de comunidades en lenguajes con cobertura AST (Python, JS/TS, Java, PHP, Ruby, Rust, CSS, HTML).

---

_Especificación v3 refleja la implementación completa a agosto de 2026._
