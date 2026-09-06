# Cap reasoning a 200 chars en judge/compare para ahorrar tokens

- **ID:** `5a5c5b6b63f745fb`
- **Slug:** `cap-reasoning-200-chars`
- **Status:** `proposed`
- **Where:** `internal/memory/semantic.go, internal/memory/memory_relations.go`
- **Capability:** `cap-reasoning-200-chars`
- **Created:** 2026-09-06T14:28:37-03:00

## Proposal

El campo Reason en MemoryRelation y SemanticVerdict no tiene límite. Cuando se muestra al agente en CompareMemories o se persiste en ApplySemanticVerdict, textos largos inflan tokens innecesariamente. Añadir cap de 200 chars.

## Goal

Reducir consumo de tokens al mostrar relaciones de memoria al agente

## Design

1. Añadir constante maxReasonChars=200 en semantic.go. 2. Truncar v.Reason en ApplySemanticVerdict antes de SanitizeText. 3. Truncar r.Reason en CompareMemories antes de mostrar.

## Tasks

- [x] Añadir constante maxReasonChars = 200 en semantic.go\n- [x] Truncar Reason en ApplySemanticVerdict\n- [x] Truncar Reason en CompareMemories\n- [x] Añadir test en semantic_test.go\n- [x] Añadir test en memory_test.go\n- [x] go build ./...\n- [x] go vet ./...\n- [x] go test -race ./internal/memory/...\n- [x] golangci-lint run ./...