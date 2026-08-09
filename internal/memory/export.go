package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportJSON exports all non-deleted project memories to a JSON file.
// Returns the number of memories exported.
func ExportJSON(db *sql.DB, projectID, filePath string) (int, error) {
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return 0, fmt.Errorf("failed to query memories for export: %w", err)
	}

	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal memories JSON: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return 0, fmt.Errorf("failed to write export file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to finalize export file: %w", err)
	}

	return len(memories), nil
}

// ImportJSON imports memories from a JSON file into the database.
// Uses upsert semantics: existing IDs are updated, new IDs are inserted.
// Returns the number of memories imported.
func ImportJSON(db *sql.DB, projectID, filePath string) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read import file: %w", err)
	}

	var memories []*Memory
	if unmarshalErr := json.Unmarshal(data, &memories); unmarshalErr != nil {
		return 0, fmt.Errorf("failed to parse import JSON: %w", unmarshalErr)
	}

	if len(memories) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := memoryInsertConflictQuery()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, mem := range memories {
		mem.ProjectID = projectID
		sanitizeMemoryFields(mem)
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err := stmt.Exec(
			mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
			mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
			nullString(mem.SessionID), nullString(mem.TopicKey),
			mem.RevisionCount, mem.DuplicateCount,
			nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
			nullTime(mem.ReviewAfter), mem.Pinned,
			createdAt)
		if err != nil {
			return 0, fmt.Errorf("failed to import memory %s: %w", mem.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit import: %w", err)
	}

	return len(memories), nil
}

// exportObsidianNode represents a graph node in the export.
type exportObsidianNode struct {
	id       string
	nodeType string
	label    string
	path     string
	metadata string
}

// exportObsidianEdge represents a graph edge in the export.
type exportObsidianEdge struct {
	sourceID   string
	targetID   string
	relType    string
	confidence string
	sourceLoc  sql.NullString
}

// obsidianRelPair is a relation between two nodes in an obsidian export.
type obsidianRelPair struct {
	source, target string
}

// ExportObsidian exports all project memories as Markdown files in Obsidian vault
// format, along with codebase structural graph nodes and edges.
func ExportObsidian(db *sql.DB, projectID, projPath, outputDir string) error {
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return err
	}

	vaultDir := filepath.Join(projPath, outputDir)
	if mkErr := os.MkdirAll(vaultDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create vault directory: %w", mkErr)
	}

	var fileNodes []exportObsidianNode
	var packageNodes []exportObsidianNode
	symbolNodesByFile := make(map[string][]exportObsidianNode)
	nodesMap := make(map[string]exportObsidianNode)

	nodeRows, err := db.Query("SELECT id, node_type, label, path, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err == nil {
		defer nodeRows.Close()
		for nodeRows.Next() {
			var n exportObsidianNode
			var pathVal, metaVal sql.NullString
			if errScan := nodeRows.Scan(&n.id, &n.nodeType, &n.label, &pathVal, &metaVal); errScan == nil {
				if pathVal.Valid {
					n.path = pathVal.String
				}
				if metaVal.Valid {
					n.metadata = metaVal.String
				}
				nodesMap[n.id] = n
				switch n.nodeType {
				case "file", "document":
					fileNodes = append(fileNodes, n)
				case "package":
					packageNodes = append(packageNodes, n)
				case "function", "class":
					symbolNodesByFile[n.path] = append(symbolNodesByFile[n.path], n)
				}
			}
		}
	}

	edgesBySource := make(map[string][]exportObsidianEdge)
	edgesByTarget := make(map[string][]exportObsidianEdge)

	edgeRows, err := db.Query("SELECT source_id, target_id, relation_type, confidence, source_location FROM graph_edges WHERE project_id = ?", projectID)
	if err == nil {
		defer edgeRows.Close()
		for edgeRows.Next() {
			var e exportObsidianEdge
			if errScan := edgeRows.Scan(&e.sourceID, &e.targetID, &e.relType, &e.confidence, &e.sourceLoc); errScan == nil {
				edgesBySource[e.sourceID] = append(edgesBySource[e.sourceID], e)
				edgesByTarget[e.targetID] = append(edgesByTarget[e.targetID], e)
			}
		}
	}

	allRels := make(map[string][]obsidianRelPair)
	relRows, err := db.Query("SELECT source_id, target_id FROM memory_relations WHERE project_id = ?", projectID)
	if err == nil {
		defer relRows.Close()
		for relRows.Next() {
			var s, t string
			if relRows.Scan(&s, &t) == nil {
				allRels[s] = append(allRels[s], obsidianRelPair{s, t})
				allRels[t] = append(allRels[t], obsidianRelPair{s, t})
			}
		}
	}

	for _, mem := range memories {
		if err := writeObsidianMemory(vaultDir, mem, allRels, edgesBySource, nodesMap); err != nil {
			return err
		}
	}

	for _, fn := range fileNodes {
		if err := writeObsidianCodeFile(vaultDir, fn, symbolNodesByFile, edgesBySource, edgesByTarget, nodesMap); err != nil {
			return err
		}
	}

	for _, pn := range packageNodes {
		if err := writeObsidianPackageFile(vaultDir, pn, edgesByTarget, nodesMap); err != nil {
			return err
		}
	}

	return nil
}

// writeObsidianMemory writes a single memory as an Obsidian markdown file.
func writeObsidianMemory(vaultDir string, mem *Memory, allRels map[string][]obsidianRelPair, edgesBySource map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) error {
	fm := fmt.Sprintf(`---
id: "%s"
category: "%s"
title: "%s"
created: "%s"
tags: [memory/%s]
`,
		mem.ID, mem.Category, mem.What, mem.CreatedAt.Format("2006-01-02"), mem.Category)

	if mem.TopicKey != "" {
		fm += fmt.Sprintf(`topic_key: "%s"
revision: %d
`, mem.TopicKey, mem.RevisionCount)
	}
	if mem.WherePath != "" {
		fm += fmt.Sprintf(`path: "%s"
`, mem.WherePath)
	}
	fm += "---\n\n"

	body := fmt.Sprintf("# %s\n\n**Category:** `%s`\n\n", mem.What, mem.Category)
	if mem.Why != "" {
		body += fmt.Sprintf("## Why\n%s\n\n", mem.Why)
	}
	if mem.Learned != "" {
		body += fmt.Sprintf("## Learned\n%s\n\n", mem.Learned)
	}
	if mem.WherePath != "" {
		body += fmt.Sprintf("**Path:** [[code/%s|%s]]\n\n", mem.WherePath, mem.WherePath)
	}
	if mem.Impact != "" {
		body += fmt.Sprintf("## Impact\n%s\n\n", mem.Impact)
	}
	if mem.ErrorsFaced != "" {
		body += fmt.Sprintf("## Errors Faced\n%s\n\n", mem.ErrorsFaced)
	}
	if mem.NextSteps != "" {
		body += fmt.Sprintf("## Next Steps\n%s\n\n", mem.NextSteps)
	}

	if rels, ok := allRels[mem.ID]; ok && len(rels) > 0 {
		body += "## Related Memories\n"
		for _, r := range rels {
			otherID := r.target
			if r.target == mem.ID {
				otherID = r.source
			}
			body += fmt.Sprintf("- [[%s]]\n", otherID)
		}
		body += "\n"
	}

	if rationaleEdges, ok := edgesBySource[mem.ID]; ok && len(rationaleEdges) > 0 {
		body += "## Links to Codebase\n"
		for _, edge := range rationaleEdges {
			if edge.relType == "rationale_for" {
				if targetNode, exists := nodesMap[edge.targetID]; exists {
					if targetNode.nodeType == "function" || targetNode.nodeType == "class" {
						body += fmt.Sprintf("- [[code/%s#%s|%s (%s)]]\n", targetNode.path, targetNode.label, targetNode.label, targetNode.nodeType)
					} else {
						body += fmt.Sprintf("- [[code/%s|%s (%s)]]\n", targetNode.id, targetNode.id, targetNode.nodeType)
					}
				}
			}
		}
		body += "\n"
	}

	body += fmt.Sprintf("---\n*Exported from sv-memory on %s*\n", time.Now().Format("2006-01-02 15:04:05"))

	writePath := filepath.Join(vaultDir, mem.ID+".md")
	return os.WriteFile(writePath, []byte(fm+body), 0644)
}

func relativePrefixToRoot(filePath string) string {
	parts := strings.Split(filePath, "/")
	if len(parts) <= 1 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < len(parts)-1; i++ {
		sb.WriteString("../")
	}
	return sb.String()
}

func makeRelativeLink(sourceNodePath, targetNodePath, targetSymbol, label, relationType string) string {
	sourceFilePath := "code/" + sourceNodePath + ".md"
	prefix := relativePrefixToRoot(sourceFilePath)
	var targetLink string
	if relationType == "rationale_for" {
		targetLink = prefix + targetNodePath
	} else if strings.HasPrefix(targetNodePath, "pkg:") {
		pkgName := strings.TrimPrefix(targetNodePath, "pkg:")
		targetLink = prefix + "code/packages/pkg_" + pkgName
	} else {
		targetLink = prefix + "code/" + targetNodePath
		if targetSymbol != "" {
			targetLink += "#" + targetSymbol
		}
	}
	return fmt.Sprintf("[[%s|%s]]", targetLink, label)
}

// writeObsidianCodeFile writes a file node as an Obsidian markdown file with its symbols and edges.
//
//nolint:gocyclo // many render branches per node kind; refactor later
func writeObsidianCodeFile(vaultDir string, fn exportObsidianNode, symbolNodesByFile map[string][]exportObsidianNode, edgesBySource, edgesByTarget map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) error {
	filePath := filepath.Join(vaultDir, "code", fn.id+".md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create code subfolders for %s: %w", fn.id, err)
	}

	fm := fmt.Sprintf(`---
id: "%s"
type: "%s"
label: "%s"
path: "%s"
`, fn.id, fn.nodeType, fn.label, fn.path)

	var metaMap map[string]interface{}
	if fn.metadata != "" {
		_ = json.Unmarshal([]byte(fn.metadata), &metaMap)
	}
	for k, v := range metaMap {
		if k != "line" && k != "exported" {
			fm += fmt.Sprintf("%s: %v\n", k, v)
		}
	}
	fm += "---\n\n"

	body := fmt.Sprintf("# %s\n\n**Type:** `%s`\n\n", fn.label, fn.nodeType)

	if symbols, ok := symbolNodesByFile[fn.id]; ok && len(symbols) > 0 {
		body += "## Defined Symbols\n\n"
		for _, sym := range symbols {
			body += fmt.Sprintf("### %s\n", sym.label)
			body += fmt.Sprintf("- **Type:** `%s`\n", sym.nodeType)

			var symMeta map[string]interface{}
			if sym.metadata != "" {
				_ = json.Unmarshal([]byte(sym.metadata), &symMeta)
			}
			if lineVal, ok := symMeta["line"]; ok {
				body += fmt.Sprintf("- **Declared on:** Line %v\n", lineVal)
			}

			if symEdges, ok := edgesBySource[sym.id]; ok && len(symEdges) > 0 {
				callsFound := false
				for _, edge := range symEdges {
					if edge.relType == "calls" {
						if !callsFound {
							body += "- **Calls:**\n"
							callsFound = true
						}
						targetNode, exists := nodesMap[edge.targetID]
						var sourceLocText string
						if edge.sourceLoc.Valid {
							sourceLocText = fmt.Sprintf(" (at %s)", edge.sourceLoc.String)
						}
						if exists {
							body += fmt.Sprintf("  - %s%s\n", makeRelativeLink(fn.id, targetNode.path, targetNode.label, targetNode.label, "calls"), sourceLocText)
						} else {
							body += fmt.Sprintf("  - `%s` (unresolved)%s\n", edge.targetID, sourceLocText)
						}
					}
				}
			}

			if symEdges, ok := edgesByTarget[sym.id]; ok && len(symEdges) > 0 {
				callersFound := false
				for _, edge := range symEdges {
					if edge.relType == "calls" {
						if !callersFound {
							body += "- **Called by:**\n"
							callersFound = true
						}
						sourceNode, exists := nodesMap[edge.sourceID]
						var sourceLocText string
						if edge.sourceLoc.Valid {
							sourceLocText = fmt.Sprintf(" (at %s)", edge.sourceLoc.String)
						}
						if exists {
							body += fmt.Sprintf("  - %s%s\n", makeRelativeLink(fn.id, sourceNode.path, sourceNode.label, sourceNode.label, "calls"), sourceLocText)
						} else {
							body += fmt.Sprintf("  - `%s` (unresolved)%s\n", edge.sourceID, sourceLocText)
						}
					}
				}
			}
			body += "\n"
		}
	}

	if fileEdges, ok := edgesBySource[fn.id]; ok && len(fileEdges) > 0 {
		importsFound := false
		for _, edge := range fileEdges {
			if edge.relType == "imports" || edge.relType == "depends_on" || edge.relType == "references" {
				if !importsFound {
					body += "## Dependencies\n"
					importsFound = true
				}
				targetNode, exists := nodesMap[edge.targetID]
				if exists {
					body += fmt.Sprintf("- **%s**: %s\n", edge.relType, makeRelativeLink(fn.id, targetNode.id, "", targetNode.label, edge.relType))
				} else {
					body += fmt.Sprintf("- **%s**: `%s` (unresolved)\n", edge.relType, edge.targetID)
				}
			}
		}
		if importsFound {
			body += "\n"
		}
	}

	if fileEdges, ok := edgesByTarget[fn.id]; ok && len(fileEdges) > 0 {
		dependentsFound := false
		for _, edge := range fileEdges {
			if edge.relType == "imports" || edge.relType == "depends_on" || edge.relType == "references" {
				if !dependentsFound {
					body += "## Dependents\n"
					dependentsFound = true
				}
				sourceNode, exists := nodesMap[edge.sourceID]
				if exists {
					body += fmt.Sprintf("- %s\n", makeRelativeLink(fn.id, sourceNode.id, "", sourceNode.label, edge.relType))
				} else {
					body += fmt.Sprintf("- `%s` (unresolved)\n", edge.sourceID)
				}
			}
		}
		if dependentsFound {
			body += "\n"
		}
	}

	var referencingRationales []exportObsidianEdge
	if rEdges, ok := edgesByTarget[fn.id]; ok {
		for _, re := range rEdges {
			if re.relType == "rationale_for" {
				referencingRationales = append(referencingRationales, re)
			}
		}
	}
	if symbols, ok := symbolNodesByFile[fn.id]; ok {
		for _, sym := range symbols {
			if rEdges, ok := edgesByTarget[sym.id]; ok {
				for _, re := range rEdges {
					if re.relType == "rationale_for" {
						referencingRationales = append(referencingRationales, re)
					}
				}
			}
		}
	}

	if len(referencingRationales) > 0 {
		body += "## Associated Memories / Decisions\n"
		for _, re := range referencingRationales {
			body += fmt.Sprintf("- %s\n", makeRelativeLink(fn.id, re.sourceID, "", re.sourceID, "rationale_for"))
		}
		body += "\n"
	}

	body += fmt.Sprintf("---\n*Exported from sv-memory on %s*\n", time.Now().Format("2006-01-02 15:04:05"))
	return os.WriteFile(filePath, []byte(fm+body), 0644)
}

// writeObsidianPackageFile writes a package node as an Obsidian markdown file.
func writeObsidianPackageFile(vaultDir string, pn exportObsidianNode, edgesByTarget map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) error {
	pkgName := strings.TrimPrefix(pn.id, "pkg:")
	filePath := filepath.Join(vaultDir, "code", "packages", "pkg_"+pkgName+".md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create packages subfolder: %w", err)
	}

	fm := fmt.Sprintf(`---
id: "%s"
type: "package"
label: "%s"
---

`, pn.id, pn.label)

	body := fmt.Sprintf("# Package: %s\n\n", pn.label)

	if fileEdges, ok := edgesByTarget[pn.id]; ok && len(fileEdges) > 0 {
		body += "## Dependents\n"
		for _, edge := range fileEdges {
			sourceNode, exists := nodesMap[edge.sourceID]
			if exists {
				body += fmt.Sprintf("- %s\n", makeRelativeLink("packages/pkg_"+pkgName, sourceNode.id, "", sourceNode.label, edge.relType))
			} else {
				body += fmt.Sprintf("- `%s` (unresolved)\n", edge.sourceID)
			}
		}
		body += "\n"
	}

	body += fmt.Sprintf("---\n*Exported from sv-memory on %s*\n", time.Now().Format("2006-01-02 15:04:05"))
	return os.WriteFile(filePath, []byte(fm+body), 0644)
}
