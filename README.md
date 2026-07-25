# sv-memory 🧠

**sv-memory** es una herramienta CLI de alto rendimiento en un solo binario y un servidor del Protocolo de Contexto de Modelos (Model Context Protocol - MCP) escrito en **Go**. Su propósito es eliminar la amnesia de contexto de los agentes de IA combinando memorias locales persistentes sobre decisiones y estándares de desarrollo con un grafo estructural de dependencias del código.

Desarrollado bajo el ecosistema de **SVTech** como una herramienta libre y de código abierto para la comunidad de desarrolladores.

---

## 🚀 Características Clave

1. **Memoria de Decisiones Persistente:** Captura correcciones de bugs complejos, decisiones arquitectónicas y estándares de codificación utilizando SQLite + FTS5 (Full-Text Search) para búsquedas de texto completo ultra rápidas por parte del agente.
2. **Sincronización en Equipo (Git Sync):** Sincroniza automáticamente las memorias locales en el archivo `.sv-memory/memories.json` dentro del repositorio. Los miembros del equipo que clonen o actualicen el repositorio integrarán automáticamente estas memorias en sus bases de datos SQLite locales al inicializar el proyecto.
3. **Grafo de Código en Go Puro:** Analiza el árbol de directorios del proyecto, detecta archivos, extrae imports (Go, Python, TypeScript, JavaScript, PHP, HTML, CSS), resuelve rutas relativas y construye un grafo de dependencias interno guardado en SQLite.
4. **Orquestación de Agentes (Reglas de Protocolo):** Inyecta automáticamente directrices en los archivos `AGENTS.md`, `.cursorrules` o `.windsurfrules` en la raíz del repositorio para guiar a los agentes de IA a consultar y escribir en la memoria de manera proactiva.
5. **Portabilidad sin Dependencias:** Compilado en Go puro sin requerir CGO gracias al uso de `modernc.org/sqlite`. El binario compilado funciona directamente en macOS, Linux y Windows.

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
       │  └────────┬_________┘ └──────┬───────┘ └─────┬──────┘  │
       └───────────┼──────────────────┼───────────────┼─────────┘
                    │                  │               │
       ┌───────────▼──────────────────▼───────────────▼─────────┐
       │      SQLite Global (+ Disparadores FTS5 Sincronizados) │
       │           (~/.config/sv-memory/storage.db)             │
       └───────────────────────────┬────────────────────────────┘
                                   │  Importación / Exportación Sync
       ┌───────────────────────────▼────────────────────────────┐
       │             Repositorio Git (JSON Versionado)          │
       │             (.sv-memory/memories.json)                 │
       └────────────────────────────────────────────────────────┘
```

---

## 📦 Configuración y Compilación

Asegúrate de tener instalado **Go 1.22+** en tu sistema.

```bash
# Clonar el repositorio
git clone https://github.com/svtech/sv-memory.git
cd sv-memory

# Descargar dependencias y compilar el binario
go build -o sv-memory ./cmd/sv-memory
```

Puedes mover el binario compilado `sv-memory` a una ruta de tu PATH de sistema (ej. `/usr/local/bin/sv-memory`) para tener acceso global.

---

## 💻 Comandos del CLI

### 1. `sv-memory init`
Inicializa `sv-memory` en el proyecto actual:
* Calcula un ID de proyecto único basado en la raíz del repositorio Git.
* Registra el proyecto en la base de datos global SQLite.
* Escanea los archivos y construye el grafo de dependencias inicial.
* Importa memorias compartidas desde `.sv-memory/memories.json` si ya existen.
* Inyecta las reglas de protocolo del agente en `AGENTS.md`, `.cursorrules` o `.windsurfrules`.

```bash
sv-memory init
```

### 2. `sv-memory mcp`
Inicia el servidor Model Context Protocol (MCP) a través de la entrada/salida estándar (`stdio`). Este es el comando que utilizan los clientes de IA para interactuar con la herramienta.

```bash
sv-memory mcp
```

### 3. `sv-memory graph rebuild`
Fuerza un re-escaneo del árbol de archivos del proyecto y actualiza los nodos del grafo de código junto con sus aristas de relación.

```bash
sv-memory graph rebuild
```

### 4. `sv-memory sync`
Ejecuta la sincronización de manera manual. Importa nuevas memorias del archivo JSON de Git hacia SQLite y exporta todas las memorias locales de la base de datos al archivo `.sv-memory/memories.json`.

```bash
sv-memory sync
```

---

## 🧩 Herramientas del Model Context Protocol (MCP)

Una vez conectado, `sv-memory` expone 4 herramientas para los agentes de IA:

1. **`sv_mem_save`**: Guarda decisiones arquitectónicas, corrección de fallos o guías de desarrollo. Automatiza la exportación inmediata a `.sv-memory/memories.json`.
2. **`sv_mem_search`**: Realiza búsquedas de texto completo (FTS) sobre las memorias guardadas. Permite filtrar por categorías.
3. **`sv_graph_query`**: Consulta el subgrafo de dependencias de un archivo, módulo o paquete con un nivel de profundidad configurable. Retorna los nodos conectados y genera un diagrama en formato **Mermaid** de Markdown.
4. **`sv_graph_sync`**: Actualiza y vuelve a sincronizar el grafo de dependencias estructurales en SQLite.

---

## ⚙️ Configuración en Clientes de IA

### Claude Desktop
Añade la siguiente configuración en tu archivo `claude_desktop_config.json` (Ruta típica en Mac: `~/Library/Application Support/Claude/claude_desktop_config.json` o en Unix: `~/.config/Claude/claude_desktop_config.json`):

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

### Cursor / Windsurf
Añade un nuevo servidor MCP en los ajustes de configuración de tu IDE:
* **Name:** `sv-memory`
* **Type:** `command`
* **Command:** `/ruta/a/tu/binario/sv-memory mcp`

---

## 📄 Licencia

Este proyecto está bajo la Licencia MIT. Consulta el archivo LICENSE para obtener más detalles.
