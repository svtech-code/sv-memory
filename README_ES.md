# sv-memory 🧠

**sv-memory** es una herramienta CLI de alto rendimiento en un solo binario y un servidor del Protocolo de Contexto de Modelos (Model Context Protocol - MCP) escrito en **Go**. Su propósito es eliminar la amnesia de contexto de los agentes de IA combinando memorias locales persistentes sobre decisiones y estándares de desarrollo con un grafo estructural de dependencias del código.

Desarrollado bajo el ecosistema de **SVTech** como una herramienta libre y de código abierto para la comunidad de desarrolladores.

---

## 🚀 Características Clave

1. **Memoria Persistente de Decisiones:** Captura correcciones de bugs complejos, decisiones arquitectónicas y estándares de codificación utilizando SQLite + FTS5 con **clasificación BM25** y **filtrado por subdirectorio (path-scoping)** para búsquedas ultrarrápidas y precisas.
2. **Paquete de Arranque Auto-Boot & Compactación:** `sv_mem_session_start` entrega automáticamente las decisiones arquitectónicas recientes y metas de sesión en 1 sola llamada. `sv_mem_compact` consolida revisiones de topic keys para mantener la base de datos liviana.
3. **Sincronización en Equipo (Git Sync):** Sincroniza automáticamente las memorias locales en archivos individuales (`.sv-memory/chunks/{id}.json`) dentro del repositorio — un archivo JSON por memoria — evitando conflictos de fusión cuando diferentes ramas guardan en paralelo.
4. **Grafo de Código Estructural & Caché LRU Sub-milisegundo:** Analiza proyectos en más de 15 lenguajes, detecta imports y dependencias, calcula **centralidad betweenness**, detecta **nodos god**, **nodos puente** y **conexiones sorprendentes**. Usa el algoritmo **Leiden** para comunidades. El caché LRU en RAM con invalidador `mtime` responde a consultas de grafo en **<1ms**.
5. **Diagnósticos de Salud & Extractor Semántico de Markdown:** Herramienta `DiagnoseGraph` para detectar enlaces rotos y nodos huérfanos. `MDSemanticExtractor` parsea secciones, tablas Markdown, bloques de código y viñetas de decisión a nodos del grafo.
6. **Exportadores de Grafo y Memoria (Obsidian y Cypher):** Exporta **visualizaciones HTML interactivas** (vis.js), **Bóvedas de Obsidian** (notas Markdown vinculadas), **scripts Cypher para Neo4j/FalkorDB**, **wikis por comunidad** y **fusiones** de instantáneas.
7. **Interfaz Interactiva de Terminal (`sv-memory tui`):** TUI interactiva en consola para explorar memorias, buscar con BM25, inspeccionar detalles de decisiones, ejecutar diagnósticos de grafo y exportar Obsidian/Cypher.
8. **Orquestación de Agentes (Reglas de Protocolo):** Inyecta automáticamente directrices en los archivos `AGENTS.md`, `.cursorrules` o `.windsurfrules` en la raíz del repositorio para guiar a los agentes de IA.
9. **Portabilidad sin Dependencias:** Compilado en Go puro sin requerir CGO gracias a `modernc.org/sqlite`. El binario compilado funciona directamente en macOS, Linux y Windows.
10. **Hooks PreToolUse y Skills:** Instala hooks PreToolUse para Claude Code y Antigravity CLI (agy), y un Skill para OpenCode.

---

## 🛠️ Arquitectura

```text
       ┌────────────────────────────────────────────────────────┐
       │    Agente de IA (Antigravity CLI, Claude, Cursor, etc) │
       └───────────────────────────┬────────────────────────────┘
                                   │  Protocolo MCP via Stdio
       ┌───────────────────────────▼────────────────────────────┐
       │                   Binario sv-memory                    │
       │                                                        │
       │  ┌──────────────────┐ ┌──────────────┐ ┌────────────┐  │
       │  │ Motor de Memoria │ │ Motor de     │ │ Config/Env │  │
       │  │                  │ │ Grafo        │ │            │  │
       │  └────────┬─────────┘ └──────┬───────┘ └─────┬──────┘  │
       └───────────┼──────────────────┼───────────────┼─────────┘
                   │                  │               │
       ┌───────────▼──────────────────▼───────────────▼─────────┐
       │      SQLite Global (+ Disparadores FTS5 Sincronizados) │
       │           (~/.config/sv-memory/storage.db)             │
       └───────────────────────────┬────────────────────────────┘
                                   │  Importación / Exportación Sync
       ┌───────────────────────────▼────────────────────────────┐
       │             Repositorio Git (JSON Versionado)          │
       │             (.sv-memory/chunks/*.json)                 │
       └────────────────────────────────────────────────────────┘
```

---

## 📋 Requerimientos Mínimos

### Para Usuarios Finales (Uso del Servidor MCP / CLI)

Si solo deseas utilizar `sv-memory` en tus proyectos de desarrollo:

- **Dependencias:** **Ninguna.** El binario es totalmente autocontenido. No requiere que tengas instalado Go, Node.js ni Python en tu máquina.
- **Compatibilidad:** macOS (Intel/Apple Silicon), Linux o Windows.
- **Clientes de IA:** Cualquier editor o cliente que soporte el protocolo MCP (como **Cursor**, **Windsurf** o **Claude Desktop**).

### Para Desarrolladores (Compilación desde Código Fuente)

Si deseas modificar el código o compilar el binario tú mismo:

- **Lenguaje:** **Go 1.26+** instalado en tu sistema.
- **Control de Versiones:** **Git** instalado.

---

## 📦 Instalación y Uso

> 📖 **Guía Completa de Inicio:** Para un recorrido detallado paso a paso sobre la instalación, configuración de editores, inicialización de proyectos y el flujo diario con IA, consulta la [Guía de Inicio Rápido](documentation/getting_started_guide.md).

Para empezar a utilizar `sv-memory`, debes completar **dos fases** obligatorias:

1. **Fase Global:** Instalar el binario en tu sistema y registrar el servidor MCP en tu editor o CLI de IA.
2. **Fase Local:** Inicializar la memoria en cada proyecto de desarrollo en el que desees trabajar.

---

### Paso 1: Obtener e Instalar el Binario (Fase Global)

#### Opción A: Instalación Rápida Global (Próximamente — Requiere Releases en GitHub)
Una vez que la primera versión estable sea publicada en GitHub Releases, podrás descargar e instalar la herramienta globalmente en tu sistema (macOS/Linux) con un solo comando:
```bash
curl -fsSL https://raw.githubusercontent.com/svtech/sv-memory/main/install.sh | bash
```
_Este script detectará tu sistema operativo y arquitectura, descargará el binario adecuado desde GitHub Releases y lo instalará de forma automática en `/usr/local/bin/sv-memory`._

#### Opción B: Compilación desde Código Fuente (Desarrolladores)
Si prefieres clonar el repositorio y generar el binario manualmente:
1. Clona el repositorio y entra al directorio:
   ```bash
   git clone https://github.com/svtech/sv-memory.git
   cd sv-memory
   ```
2. Compila el ejecutable optimizado:
   ```bash
   go build -o sv-memory ./cmd/sv-memory
   ```
3. Copia el ejecutable resultante a tu carpeta global para que esté disponible en cualquier terminal:
   ```bash
   sudo cp sv-memory /usr/local/bin/
   ```

---

### Paso 2: Registrar el Servidor MCP en tu Editor / CLI (Fase Global)
> [!IMPORTANT]
> **Este paso es obligatorio.** Inicializar el proyecto con `init` no es suficiente. Si no registras el servidor en tu cliente de IA (como Cursor o Claude Desktop), el agente de IA no sabrá dónde encontrar `sv-memory` ni cómo comunicarse con él.

Dirígete a la sección de **[Configuración en Clientes de IA](#%EF%B8%8F-configuraci%C3%B3n-en-clientes-de-ia)** al final de este documento y añade la configuración correspondiente. Esto le enseñará a tu editor o CLI de IA a arrancar el servidor `sv-memory mcp` en segundo plano de manera automática.

---

### Paso 3: Inicializar tu Proyecto (Fase Local)
> [!IMPORTANT]
> **Este paso es obligatorio para cada proyecto.** Debes indicarle a `sv-memory` qué carpeta indexar y dónde inyectar el protocolo de instrucciones para el agente de IA.

1. Abre tu terminal y navega a la raíz del proyecto de desarrollo en el que quieres trabajar (ej: `cd ~/mi-proyecto-web`).
2. Ejecuta el comando de inicialización:
   ```bash
   sv-memory init
   ```
   *(Este comando creará el archivo `AGENTS.md` local en la raíz de ese proyecto y guardará el grafo de dependencias inicial en la base de datos SQLite global de tu usuario).*

Una vez completados estos pasos, ¡tu agente de IA ya tendrá memoria persistente y conocimiento del grafo de código activo de forma completamente automática!

---

## 💻 Comandos del CLI

### 1. `sv-memory init`

Inicializa `sv-memory` en el proyecto actual:

- Calcula un ID de proyecto único basado en la raíz del repositorio Git.
- Registra el proyecto en la base de datos global SQLite.
- Escanea los archivos y construye el grafo de dependencias inicial.
- Importa memorias compartidas desde `.sv-memory/chunks/{id}.json` (o el archivo heredado `memories.json`) si ya existen.
- Inyecta las reglas de protocolo del agente en `AGENTS.md`, `.cursorrules` o `.windsurfrules`.

```bash
sv-memory init
```

### 2. `sv-memory mcp`

Inicia el servidor Model Context Protocol (MCP) a través de la entrada/salida estándar (`stdio`). Este es el comando que utilizan los clientes de IA para interactuar con la herramienta.

```bash
sv-memory mcp
```

### 3. `sv-memory tui`

Inicia una Interfaz Interactiva de Usuario en Terminal (TUI) para navegar memorias recientes, buscar con FTS5 BM25, inspeccionar detalles de decisiones, ejecutar diagnósticos de salud del grafo y exportar archivos Obsidian/Cypher.

```bash
sv-memory tui
```

### 3. `sv-memory sync`

Ejecuta la sincronización de manera manual. Importa nuevas memorias de los archivos fragmentados de Git (`chunks/*.json` o el archivo heredado `memories.json`) hacia SQLite, y exporta todas las memorias locales de la base de datos a archivos individuales en `.sv-memory/chunks/`.

```bash
sv-memory sync
```

### 4. `sv-memory configure`

Inicia un asistente interactivo por fases en la terminal para configurar tus entornos de desarrollo.
* **Fase 1 (Editores):** Te permite seleccionar qué editores configurar (`Cursor`, `VS Code`, `Zed`, `Windsurf`).
* **Fase 2 (CLIs):** Te permite seleccionar qué herramientas de terminal configurar (`Claude Code`, `OpenCode`, `Codex`, `Antigravity CLI (agy)`).
* **Fase 3 (Aplicación):** Genera un resumen, solicita confirmación y realiza de manera segura la inyección de configuraciones automáticas (para herramientas como Zed, OpenCode, Antigravity y Claude Code) y muestra instrucciones de copiado paso a paso para el resto.

```bash
sv-memory configure
```

### 5. `sv-memory diagnose`

Ejecuta verificaciones de salud: conexión a base de datos, esquemas, permisos de escritura y configuración activa.

```bash
sv-memory diagnose
```

### 6. `sv-memory stats`

Muestra estadísticas del proyecto: memorias totales, eliminadas, guardadas en las últimas 24h, sesiones y relaciones.

```bash
sv-memory stats
```

### 7. `sv-memory graph rebuild`

Fuerza un re-escaneo completo del árbol de directorios y reconstruye los nodos y aristas del grafo.

```bash
sv-memory graph rebuild
```

### 8. `sv-memory graph path <source> <target>`

Encuentra la ruta de dependencia más corta entre dos nodos del grafo (hasta 10 saltos).

```bash
sv-memory graph path utils/helpers.ts services/api.ts
```

### 9. `sv-memory graph explain <node>`

Muestra información detallada de un nodo: tipo, etiqueta, ruta, metadatos y métricas fan-in/fan-out.

```bash
sv-memory graph explain internal/db/db.go
```

### 10. `sv-memory graph communities`

Detecta y lista clústeres comunitarios usando el algoritmo Leiden, mostrando miembros, centralidad y nodos god.

```bash
sv-memory graph communities
```

### 11. `sv-memory graph wiki [--output dir]`

Exporta páginas wiki en Markdown por cada comunidad, con archivos miembros, centralidad y dependencias entre comunidades.

```bash
sv-memory graph wiki --output graph-wiki
```

### 12. `sv-memory graph viz [--output file]`

Genera una visualización HTML interactiva del grafo usando vis.js con colores por comunidad, simulación física y filtros.

```bash
sv-memory graph viz --output graph.html
```

### 13. `sv-memory graph merge <json-file>`

Fusiona una instantánea JSON del grafo en el proyecto actual, combinando nodos y aristas.

```bash
sv-memory graph merge backup.json
```

### 14. `sv-memory export [output-file]`

Exporta todas las memorias no eliminadas del proyecto a un archivo JSON portátil.

```bash
sv-memory export memories-backup.json
```

### 15. `sv-memory import <input-file>`

Importa memorias desde un archivo JSON usando upsert por ID.

```bash
sv-memory import memories-backup.json
```

### 16. `sv-memory obsidian-export [-o output-dir]`

Exporta todas las memorias del proyecto como archivos Markdown estructurados como un vault de Obsidian.

```bash
sv-memory obsidian-export -o my-obsidian-vault
```

### 17. `sv-memory sync`

Ejecuta la sincronización de manera manual. Importa nuevas memorias de los archivos fragmentados de Git (`chunks/*.json` o el archivo heredado `memories.json`) hacia SQLite, y exporta todas las memorias locales de la base de datos a archivos individuales en `.sv-memory/chunks/`.

```bash
sv-memory sync
```

### 18. `sv-memory delete session <session-id>`

Elimina una sesión vacía (falla si la sesión tiene memorias asociadas).

```bash
sv-memory delete session abc12345
```

### 19. `sv-memory delete project <project-id> [--hard]`

Elimina en cascada todos los datos de un proyecto. Soft-delete por defecto; `--hard` elimina permanentemente.

```bash
sv-memory delete project proj1234 --hard
```

### 20. `sv-memory projects list`

Lista todos los proyectos registrados con su ID, nombre, ruta, conteo de memorias y sesiones.

```bash
sv-memory projects list
```

### 21. `sv-memory conflicts`

Muestra memorias conflictivas y superposiciones semánticas detectadas en el proyecto.

```bash
sv-memory conflicts
```

### 22. `sv-memory hooks install [--platform P] [--strict]`

Instala hooks PreToolUse (o Skills) para asistentes de IA en el proyecto actual. Cuando un agente intenta leer un archivo, el hook lo intercepta y sugiere consultar el grafo y la memoria de sv-memory primero.

```bash
# Instalar para todas las plataformas soportadas (modo soft)
sv-memory hooks install

# Modo strict: bloquea la primera lectura de archivo por sesión
sv-memory hooks install --strict

# Instalar para una plataforma específica
sv-memory hooks install --platform claude-code
sv-memory hooks install --platform antigravity
sv-memory hooks install --platform opencode
```

**Plataformas soportadas:**

| Plataforma | Tipo | Mecanismo |
|-----------|------|-----------|
| **Claude Code** | Hook PreToolUse | Script shell en `.claude/hooks/pre_tool_use/`, configurado via `.claude/settings.json` |
| **Antigravity CLI (agy)** | Hook PreToolUse | Script shell + configuración JSON en `.agents/hooks.json` |
| **Codex** | No-op (AGENTS.md) | Hook placeholder (Codex Desktop rechaza scripts PreToolUse); el nudge es via AGENTS.md |
| **OpenCode** | Skill + AGENTS.md | Skill en `.opencode/skills/sv-memory/SKILL.md` + inyección de reglas de protocolo en AGENTS.md |

**Modos:**

| Modo | Comportamiento |
|------|---------------|
| **Soft** (default) | Siempre permite la herramienta; el nudge se transmite como contexto adicional (Claude Code) o via AGENTS.md (agy, Codex, OpenCode) |
| **Strict** (`--strict`) | Bloquea la primera llamada de lectura de archivo por sesión con un mensaje dirigiendo al agente a usar sv-memory primero. Llamadas subsiguientes son permitidas. |

### 23. `sv-memory hooks status`

Muestra el estado de instalación de hooks para cada plataforma:

```bash
sv-memory hooks status
```

### 24. `sv-memory hooks uninstall [--platform P]`

Elimina hooks / skills instalados por `sv-memory hooks install`:

```bash
sv-memory hooks uninstall
sv-memory hooks uninstall --platform claude-code
```

---

## 🧩 Herramientas del Model Context Protocol (MCP)

Una vez conectado, `sv-memory` expone **26 herramientas MCP** para los agentes de IA:

### Herramientas de Memoria
1. **`sv_mem_save`**: Guarda decisiones arquitectónicas, corrección de fallos o guías de desarrollo con sincronización Git automática.
2. **`sv_mem_search`**: Búsqueda de texto completo (FTS5) con **clasificación BM25**, filtros por categoría y **filtrado por subdirectorio (path-scoping)**.
3. **`sv_mem_get`**: Recupera el contenido completo de una memoria específica con truncamiento opcional.
4. **`sv_mem_timeline`**: Contexto cronológico alrededor de una memoria (Capa 2 de divulgación progresiva).
5. **`sv_mem_suggest_topic_key`**: Genera un topic_key estable en formato category/kebab-case para upsert.
6. **`sv_mem_judge`**: Crea relaciones entre memorias (supersedes, conflicts_with, relates_to).
7. **`sv_mem_compare`**: Comparación lado a lado de dos memorias.
8. **`sv_mem_review`**: Encuentra memorias que necesitan mantenimiento (obsoletas, duplicadas, candidatas a consolidación).
9. **`sv_mem_stats`**: Estadísticas agregadas de memorias y desglose por categoría.
10. **`sv_mem_current_project`**: Recupera el ID, nombre y ruta del proyecto activo.
11. **`sv_mem_delete`**: Soft-delete (o hard-delete) de una memoria.
12. **`sv_mem_capture_passive`**: Registra entradas de diario ligeras automáticamente.
13. **`sv_mem_conflicts`**: Detecta y muestra memorias conflictivas con análisis de superposición semántica.
14. **`sv_mem_compact`**: Consolida revisiones históricas de topic keys en registros de síntesis unificados.

### Herramientas de Sesión
15. **`sv_mem_session_start`**: Registra una nueva sesión de codificación y entrega automáticamente el **Paquete de Arranque Auto-Boot** (resumen anterior + decisiones arquitectónicas principales).
16. **`sv_mem_session_end`**: Cierra una sesión activa con resumen.
17. **`sv_mem_session_summary`**: Actualiza objetivo, descubrimientos y siguientes pasos.
18. **`sv_mem_context`**: Recupera contexto de la última sesión completada (recuperación post-compactación).

### Herramientas de Grafo
19. **`sv_graph_query`**: Consulta BFS de dependencias con caché LRU sub-milisegundo. Devuelve diagrama Mermaid.
20. **`sv_graph_path`**: Ruta de dependencia más corta entre dos nodos.
21. **`sv_graph_sync`**: Sincronización incremental del grafo desde cambios de archivos.
22. **`sv_graph_explain`**: Información detallada de un nodo con métricas fan-in/fan-out.
23. **`sv_graph_god_nodes`**: Identifica nodos altamente conectados (análisis de centralidad).
24. **`sv_graph_surprising_connections`**: Encuentra dependencias inesperadas o no obvias.
25. **`sv_graph_viz`**: Genera visualización HTML interactiva con colores por comunidad.
26. **`sv_graph_merge`**: Fusiona una instantánea JSON del grafo en el grafo actual.

---

## ⚙️ Configuración en Clientes de IA

Para utilizar `sv-memory` como servidor MCP, debes configurarlo en tu cliente de IA preferido utilizando la ruta absoluta a tu binario compilado (o simplemente `sv-memory` si lo instalaste globalmente en el PATH).

### 1. Claude Desktop
Añade la siguiente configuración en tu archivo `claude_desktop_config.json` (Ruta en Mac: `~/Library/Application Support/Claude/claude_desktop_config.json` o en Linux: `~/.config/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "sv-memory": {
      "command": "/ruta/a/tu/binario/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

### 2. Claude Code (CLI de Terminal)
Para agregar el servidor de forma global a la herramienta CLI de Anthropic:
```bash
claude mcp add sv-memory -- /ruta/a/tu/binario/sv-memory mcp
```

### 3. Cursor / Windsurf
Ve a los Ajustes de Cursor -> Features -> MCP, añade un nuevo servidor:
* **Name:** `sv-memory`
* **Type:** `command`
* **Command:** `/ruta/a/tu/binario/sv-memory mcp`

### 4. Zed Editor
Añade lo siguiente en tu archivo de configuración `~/.config/zed/settings.json`:
```json
{
  "mcp_servers": {
    "sv-memory": {
      "command": "/ruta/a/tu/binario/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

### 5. Antigravity CLI / OpenCode / Codex
Si usas un entorno de desarrollo compatible con MCP, agrega el servidor en el archivo de configuración global `mcp_config.json` o configúralo localmente:
```json
{
  "mcpServers": {
    "sv-memory": {
      "command": "/ruta/a/tu/binario/sv-memory",
      "args": ["mcp"]
    }
  }
}
```

---

## 💡 Ejemplo de Interacción y Flujo de Trabajo

Una vez configurado, el flujo de trabajo autónomo con tu agente de IA (como Claude, Gemini o Cursor) es el siguiente:

1. **Al iniciar en el proyecto:**
   El agente lee el archivo `AGENTS.md` y de inmediato realiza una búsqueda automática:
   > *Agente ejecuta internamente:* `sv_mem_search(query="bugfix")` o `sv_mem_search(query="architecture")`
   
2. **Al proponer cambios de código:**
   Antes de refactorizar o borrar código, el agente verifica dependencias:
   > *Agente ejecuta internamente:* `sv_graph_query(path_or_node="internal/db/db.go", depth=1)`
   > *Resultado:* El agente recibe las relaciones del archivo y visualiza un diagrama Mermaid de las dependencias, evitando romper módulos externos.

3. **Al finalizar una tarea (ej: solucionar un bug complejo):**
   El agente registra la decisión:
   > *Agente ejecuta internamente:*
   > `sv_mem_save(category="bugfix", what="Uso de modernc.org/sqlite", why="Evitar depender de CGO para permitir compilación cruzada limpia", learned="Utilizar siempre modernc.org/sqlite para bases de datos SQLite portables en Go", where_path="internal/db/db.go")`
   > *Resultado:* La decisión se guarda en SQLite y se sincroniza automáticamente en `.sv-memory/chunks/{id}.json` (un archivo por memoria, evitando conflictos de fusión) para que todo tu equipo la obtenga al hacer `git pull`.

---

## 📄 Licencia

Este proyecto está bajo la Licencia MIT. Consulta el archivo LICENSE para obtener más detalles.
