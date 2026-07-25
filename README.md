# sv-memory 🧠

**sv-memory** es una herramienta CLI de alto rendimiento en un solo binario y un servidor del Protocolo de Contexto de Modelos (Model Context Protocol - MCP) escrito en **Go**. Su propósito es eliminar la amnesia de contexto de los agentes de IA combinando memorias locales persistentes sobre decisiones y estándares de desarrollo con un grafo estructural de dependencias del código.

Desarrollado bajo el ecosistema de **SVTech** como una herramienta libre y de código abierto para la comunidad de desarrolladores.

---

## 🚀 Características Clave

1. **Memoria de Decisiones Persistente:** Captura correcciones de bugs complejos, decisiones arquitectónicas y estándares de codificación utilizando SQLite + FTS5 (Full-Text Search) para búsquedas de texto completo ultra rápidas por parte del agente.
2. **Sincronización en Equipo (Git Sync):** Sincroniza automáticamente las memorias locales en el archivo `.sv-memory/memories.json` dentro del repositorio. Los miembros del equipo que clonen o actualicen el repositorio integrarán automáticamente estas memorias en sus bases de datos SQLite locales al inicializar el proyecto.
3. **Grafo de Código en Go Puro:** Analiza el árbol de directorios del proyecto, detecta archivos, extrae imports/dependencias (Go, Python, TypeScript, JavaScript, Astro, PHP, HTML, CSS, Bash, Lua), resuelve rutas relativas y construye un grafo de dependencias interno guardado en SQLite.
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
       │             (.sv-memory/memories.json)                 │
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

- **Lenguaje:** **Go 1.22+** instalado en tu sistema.
- **Control de Versiones:** **Git** instalado.

---

## 📦 Instalación y Uso

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
- Importa memorias compartidas desde `.sv-memory/memories.json` si ya existen.
- Inyecta las reglas de protocolo del agente en `AGENTS.md`, `.cursorrules` o `.windsurfrules`.

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

### 5. `sv-memory configure`

Inicia un asistente interactivo por fases en la terminal para configurar tus entornos de desarrollo.
* **Fase 1 (Editores):** Te permite seleccionar qué editores configurar (`Cursor`, `VS Code`, `Zed`, `Windsurf`).
* **Fase 2 (CLIs):** Te permite seleccionar qué herramientas de terminal configurar (`Claude Code`, `OpenCode`, `Codex`, `Antigravity CLI (agy)`).
* **Fase 3 (Aplicación):** Genera un resumen, solicita confirmación y realiza de manera segura la inyección de configuraciones automáticas (para herramientas como Zed, OpenCode, Antigravity y Claude Code) y muestra instrucciones de copiado paso a paso para el resto.

```bash
sv-memory configure
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
   > *Resultado:* La decisión se guarda en SQLite y se sincroniza automáticamente en `.sv-memory/memories.json` para que todo tu equipo la obtenga al hacer `git pull`.

---

## 📄 Licencia

Este proyecto está bajo la Licencia MIT. Consulta el archivo LICENSE para obtener más detalles.
