# Reglas de oro del flujo de trabajo

- **ID:** `057a2aa589ab497e`
- **Slug:** `workflow-golden-rules`
- **Status:** `applied`
- **Where:** `AGENTS.md`
- **Created:** 2026-08-15T03:22:44-04:00

## Proposal

Reglas R1-R7 que rigen el flujo de trabajo del proyecto, consolidando standards previos en una única fuente autoritativa: R1) Entregar commit por fase/tarea compleja terminada en formato `type(scope): description` en inglés, scope siempre presente. R2) Tras terminar tarea y entregar el commit, pedir confirmación explícita antes de continuar. R3) NUNCA ejecutar git add/commit/push — el usuario los aplica manualmente. R4) Antes de cada feature/refactor/bug/fix, revisar con sv_mem_search y sv_graph_query si ya existe algo que lo cubra (evitar duplicación). R5) Evaluar valor real, viabilidad y necesidad de cada implementación nueva; no implementar por impulso. R6) El corazón de sv-memory es la memoria persistente + grafo de conocimiento — todo cambio debe preservarlos y usarlos. R7) Al terminar, revisar contra bugfixes previos (fallos vistos en CI como go test -race -cover o golangci-lint shadow) antes de declarar terminado.

## Goal

Una única fuente autoritativa del flujo de trabajo del proyecto que sustituya a standard/commit-message-format, standard/ci-validation-gate y standard/phase-workflow, sin duplicar reglas entre memorias.

## Design

Crear un standard consolidado category=standard con topic_key standard/workflow-golden-rules vía sv_commit_spec. El pre-flight devolverá WARN por overlap con los 3 standards previos (no están pinned, así que no BLOCK). Gestionar el WARN con sv_validate_decision; si el veredicto final es WARN se resuelve declarando supersedes de los 3 antiguos vía sv_mem_judge, quedando ellos como historial y el nuevo como autoritativo.

## Tasks

1. sv_propose_spec workflow-golden-rules (este registro). 2. sv_validate_decision sobre el change_id → verificar veredicto (esperado WARN). 3. sv_commit_spec(change_id, category="standard") → crea standard/workflow-golden-rules. 4. sv_mem_judge supersedes ×3 (sobre efc2ad9dcbd54d74, 37c65584345644c6, 677df4ea29ea46b7). 5. Verificar espejo .sv-memory/specs/changes/workflow-golden-rules.md. 6. Entregar mensaje de commit al usuario para aplicar manualmente. 7. sv_mem_session_end.