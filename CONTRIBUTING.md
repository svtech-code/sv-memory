# Contributing to SV-Memory

Thank you for your interest in contributing to **SV-Memory**! This guide outlines the workflow and standards to help you get started.

> 💡 **Just want to install sv-memory?** Use the one-line installer:
> `curl -fsSL https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.sh | bash`
> (Windows: `iwr -useb https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.ps1 | iex`)

---

## 1. Branching Strategy & Workflow

We follow a simple feature-branch workflow:

1.  **Branch Naming:** Create a branch from `main` using descriptive prefixes:
    *   `feat/feature-name` for new features.
    *   `fix/bug-name` for bug fixes.
    *   `docs/doc-name` for documentation updates.
    *   `refactor/refactor-name` for code cleanup.
2.  **Pull Requests:** Once your changes are ready and tested:
    *   Push your branch to GitHub.
    *   Open a Pull Request (PR) against `main`.
    *   Ensure all tests pass before requesting review.

> **Note for external contributors:** `main` is protected — direct pushes to it are
> blocked. All changes must come through a pull request. Continuous Integration (CI)
> runs `go vet`, `go test -race`, and a build check on Linux and macOS, and **must pass
> green** before your PR can be merged. You do not need write access to contribute:
> fork the repository, create a branch on your fork, and open a PR from there.

---

## 2. Commit Message Standards

This repository strictly enforces the **Conventional Commits** specification. Furthermore, **all commit messages must be written in English**.

Format: `<type>(<scope>): <description>`

*   **Allowed Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `build`, `ci`, `chore`.
*   **Scope:** Optional but highly recommended (e.g., `mcp`, `graph`, `db`, `config`).
*   **Examples:**
    *   `feat(mcp): add support for active session tracking`
    *   `fix(graph): resolve typescript interface import parsing panic`
    *   `docs(spect): update specification to v3`

---

## 3. Development Environment & Makefile Targets

We use a standard `Makefile` to manage development workflows. Ensure Go (v1.26+) is installed.

### Key Targets:
*   **Build the binary:**
    ```bash
    make build
    ```
*   **Run unit tests:**
    ```bash
    make test
    ```
*   **Run tests with race detection (highly recommended before commits):**
    ```bash
    make test-race
    ```
*   **Format codebase:**
    ```bash
    make fmt
    ```
*   **Run go vet:**
    ```bash
    make vet
    ```

---

## 4. How to Add a New Language to the Graph Engine

The graph engine parses source code dependencies. To add support for a new programming language:

1.  **Map Extensions:** Open [graph.go](internal/graph/graph.go) and add the file extensions to the `languageFromExt` map:
    ```go
    var languageFromExt = map[string]string{
        ".newext": "newlanguage",
    }
    ```
2.  **Enable Symbol Scanning:** If the language supports symbols (like functions/classes), add its extension to `symbolScanExts` in [scanner.go](internal/graph/scanner.go):
    ```go
    var symbolScanExts = map[string]bool{
        ".newext": true,
    }
    ```
3.  **Implement Parse Extractor:** In `parseSymbols` or `parseResult` routines, implement the extractor or pattern matching to capture imports and declarations (e.g. using regex or AST parsers).
4.  **Write Tests:** Add test cases to [graph_test.go](internal/graph/graph_test.go) verifying that file scanning correctly registers nodes and import edges for the new language.

---

## 5. How to Add a New MCP Tool

To register a new Model Context Protocol (MCP) tool:

1.  **Define and Register the Tool:** Open [mcp.go](internal/mcp/mcp.go). Inside the `NewServer` function, define your new tool using `mcp.NewTool` and register its handler with the MCP server (`ms := server.NewMCPServer(...)`):
    ```go
    newTool := mcp.NewTool("sv_tool_name",
        mcp.WithDescription("Tool description"),
        mcp.WithString("param1", mcp.Required(), mcp.Description("Param description")),
    )

    ms.AddTool(newTool, s.handleToolName)
    ```
    Add the handler method `handleToolName` (alongside the other handlers on the `*Server` struct) with the signature `func (s *Server) handleToolName(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`:
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
2.  **Document the Tool:** Update the MCP tools list in [spect.md](documentation/spect.md) with details on parameters and return formats.
3.  **Write Integration Tests:** Open [mcp_test.go](internal/mcp/mcp_test.go) and add a test case retrieving your tool and verifying its response for valid and invalid parameters.
