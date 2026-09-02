# Explore unificado: multi-símbolo + source de vecinos + call path en sv_mem_context_pack (alias sv_graph_explore)

- **ID:** `f636017aa0004801`
- **Slug:** `contextpack-unified-explore`
- **Status:** `applied`
- **Where:** `internal/memory/contextpack.go`
- **Capability:** `contextpack-unified-explore`
- **Created:** 2026-09-01T23:21:38-04:00

## Proposal

sv-memory aprovecha mal su grafo: tiene todos los primitivos (query, path, explain, god nodes, communities, blast radius) pero el agente debe encadenarlos a mano. codegraph demostró que 'one strong tool beats a menu of narrower ones'. Esta fase evoluciona sv_mem_context_pack (ya existente y conservando su contrato para compatibilidad) para aceptar varios símbolos en el path (separados por coma), devolver source verbatim quirúrgico de los nodos secundarios/vecinos (no solo del resuelto), y añadir el shortest call path entre dos de los símbolos dados (reuso de InMemoryGraph.ShortestPath). Se registra un alias MCP sv_graph_explore apuntando al mismo handler, con descripción orientada a exploración ('one bounded call to understand how X relates to Y'). No se elimina sv_mem_context_pack; se amplía su renderizado condicionalmente cuando hay varios símbolos o call path.

## Goal

Que el agente entienda código con UNA llamada (estilo codegraph_explore): resolver varios símbolos, devolver su source verbatim quirúrgico, el call path entre ellos, blast radius y memorias vinculadas — en lugar de encadenar sv_graph_query + sv_graph_path + sv_graph_explain manualmente.

## Design

1) Ampliar el struct ContextPack con: ExtraSnippets []ContextSnippet (label, path, startLine, text) y CallPath []string (ordered node IDs of the shortest path). 2) Añadir GetContextPackMulti (o ampliar GetContextPack) que: (a) detecta múltiples símbolos en `query` (split por comas/espacios significativos), (b) resuelve cada uno con ResolveContextNode, (c) para el primer nodo mantiene el Snippet + BlastRadius + memorias existentes, (d) para los demás nodos extrae surgical snippets, (e) calcula ShortestPath entre el primer y segundo símbolo principales usando LoadFullGraph + InMemoryGraph.ShortestPath (con límite de hops), (f) reordena el render para mostrar call path + snippets secundarios tras el bloque principal. 3) handler MCP: nuevo sv_graph_explore con params path (requerido, multi-símbolo separado por coma), include_changes, token_budget; registra el MISMO handler handleContextPack. 4) Tests: unit tests en contextpack_test.go y tools_graph_test.go/mcp_test.go cubriendo multi-símbolo, call path, y alias tool registrado.

## Tasks

1. Añadir ContextSnippet + ExtraSnippets + CallPath al struct ContextPack en contextpack.go. 2. Implementar parseo multi-símbolo y resolución de varios nodos. 3. Implementar cálculo de ShortestPath entre los dos primeros símbolos. 4. Actualizar RenderContextPack con secciones condicionales 'Call path' y 'Related symbols (source)'. 5. Registrar sv_graph_explore en mcp.go (alias al handler handleContextPack). 6. Añadir tests: multi-símbolo, call path, preservación contrato single-symbol, alias MCP registrado.

## ADDED Requirements

### Requirement: Resolve multiple symbols in one explore call
The context pack tool SHALL accept a comma-separated list of symbols/paths and resolve each to a graph node.

#### Scenario: Agent explores two related symbols

### Requirement: Surface the call path between explored symbols
The context pack tool SHALL compute and render the shortest dependency path between the two most significant resolved symbols when they exist.

#### Scenario: Call path between two symbols

### Requirement: Preserve the existing single-symbol contract
The sv_mem_context_pack tool SHALL keep its existing behaviour and output for a single path so existing integrations are not broken.

#### Scenario: Single symbol keeps old shape