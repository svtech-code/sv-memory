# Cómo Contribuir a SV-Memory

> **Idioma:** [English](CONTRIBUTING.md) | **Español**

¡Gracias por tu interés en contribuir a **SV-Memory**! Esta guía describe el flujo de trabajo y los estándares para ayudarte a comenzar.

> 💡 **¿Solo quieres instalar sv-memory?** Usa el instalador de una línea:
> `curl -fsSL https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.sh | bash`
> (Windows: `iwr -useb https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.ps1 | iex`)

---

## 1. Estrategia de Ramas y Flujo de Trabajo

Seguimos un flujo de trabajo simple con ramas de funcionalidad:

1.  **Nombres de Rama:** Crea una rama desde `main` usando prefijos descriptivos:
    *   `feat/nombre-funcionalidad` para nuevas funcionalidades.
    *   `fix/nombre-bug` para correcciones de bugs.
    *   `docs/nombre-doc` para actualizaciones de documentación.
    *   `refactor/nombre-refactor` para limpieza de código.
2.  **Pull Requests:** Una vez que tus cambios estén listos y probados:
    *   Empuja tu rama a GitHub.
    *   Abre una Pull Request (PR) contra `main`.
    *   Asegúrate de que todos los tests pasen antes de solicitar revisión.

> **Nota para contribuidores externos:** `main` está protegida — los push directos a ella están
> bloqueados. Todos los cambios deben llegar mediante una pull request. La Integración Continua (CI)
> ejecuta `go vet`, `go test -race` y una verificación de compilación en Linux y macOS, y **debe
> pasar en verde** antes de que tu PR pueda fusionarse. No necesitas acceso de escritura para
> contribuir: haz un fork del repositorio, crea una rama en tu fork y abre una PR desde allí.

---

## 2. Estándares de Mensajes de Commit

Este repositorio aplica estrictamente la especificación **Conventional Commits**. Además, **todos los mensajes de commit deben estar escritos en inglés**.

Formato: `<type>(<scope>): <description>`

*   **Tipos Permitidos:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `build`, `ci`, `chore`.
*   **Scope:** Opcional pero muy recomendado (p. ej., `mcp`, `graph`, `db`, `config`).
*   **Ejemplos:**
    *   `feat(mcp): add support for active session tracking`
    *   `fix(graph): resolve typescript interface import parsing panic`
    *   `docs(spect): update specification to v3`

---

## 3. Entorno de Desarrollo y Targets del Makefile

Usamos un `Makefile` estándar para gestionar los flujos de trabajo de desarrollo. Asegúrate de tener Go (v1.26+) instalado.

### Targets Clave:
*   **Compilar el binario:**
    ```bash
    make build
    ```
*   **Ejecutar tests unitarios:**
    ```bash
    make test
    ```
*   **Ejecutar tests con detección de race (muy recomendado antes de commits):**
    ```bash
    make test-race
    ```
*   **Dar formato al código:**
    ```bash
    make fmt
    ```
*   **Ejecutar go vet:**
    ```bash
    make vet
    ```

---

## 4. Cómo Añadir un Nuevo Lenguaje al Motor de Grafo

El motor de grafo analiza dependencias del código fuente. Para añadir soporte a un nuevo lenguaje de programación:

1.  **Mapear Extensiones:** Abre [graph.go](internal/graph/graph.go) y añade las extensiones de archivo al mapa `languageFromExt`:
    ```go
    var languageFromExt = map[string]string{
        ".newext": "newlanguage",
    }
    ```
2.  **Habilitar el Escaneo de Símbolos:** Si el lenguaje soporta símbolos (como funciones/clases), añade su extensión a `symbolScanExts` en [scanner.go](internal/graph/scanner.go):
    ```go
    var symbolScanExts = map[string]bool{
        ".newext": true,
    }
    ```
3.  **Implementar el Extractor de Parseo:** En las rutinas `parseSymbols` o `parseResult`, implementa el extractor o el pattern matching para capturar imports y declaraciones (p. ej., usando regex o parsers AST).
4.  **Escribir Tests:** Añade casos de test a [graph_test.go](internal/graph/graph_test.go) que verifiquen que el escaneo de archivos registra correctamente nodos y aristas de import para el nuevo lenguaje.

---

## 5. Cómo Añadir una Nueva Herramienta MCP

Para registrar una nueva herramienta de Model Context Protocol (MCP):

1.  **Definir y Registrar la Herramienta:** Abre [mcp.go](internal/mcp/mcp.go). Dentro de la función `NewServer`, define tu nueva herramienta usando `mcp.NewTool` y registra su handler con el servidor MCP (`ms := server.NewMCPServer(...)`):
    ```go
    newTool := mcp.NewTool("sv_tool_name",
        mcp.WithDescription("Tool description"),
        mcp.WithString("param1", mcp.Required(), mcp.Description("Param description")),
    )

    ms.AddTool(newTool, s.handleToolName)
    ```
    Añade el método handler `handleToolName` (junto a los demás handlers en el struct `*Server`) con la firma `func (s *Server) handleToolName(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`:
    ```go
    func (s *Server) handleToolName(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        paramVal, err := req.RequireString("param1")
        if err != nil {
            return mcp.NewToolResultError("missing required field: param1"), nil
        }
        // Tool logic goes here
        return mcp.NewToolResultText("Success"), nil
    }
    ```
2.  **Documentar la Herramienta:** Actualiza la lista de herramientas MCP en [spect.md](documentation/spect.md) (o su versión en [español](documentation/spect_ES.md)) con los detalles de los parámetros y los formatos de retorno.
3.  **Escribir Tests de Integración:** Abre [mcp_test.go](internal/mcp/mcp_test.go) y añade un caso de test que recupere tu herramienta y verifique su respuesta para parámetros válidos e inválidos.
