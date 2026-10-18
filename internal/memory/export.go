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

	if err := atomicWriteFile(filePath, data); err != nil {
		return 0, err
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

	// Strict import: an insert failure aborts instead of silently skipping.
	if _, err := importMemoriesIntoDB(db, projectID, memories, false, true); err != nil {
		return 0, err
	}

	return len(memories), nil
}

// obsidianFooter returns the standard footer appended to every exported
// Obsidian file so readers know when and with what tool the vault was generated.
func obsidianFooter() string {
	return fmt.Sprintf("---\n*Exported from sv-memory on %s*\n", time.Now().Format("2006-01-02 15:04:05"))
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
// format, along with codebase structural graph nodes and edges. vaultDir is the
// target directory, already validated to stay inside the project, and is
// created if it does not exist.
// obsidianGraphData is the graph subset needed to render the Obsidian vault.
type obsidianGraphData struct {
	fileNodes         []exportObsidianNode
	packageNodes      []exportObsidianNode
	symbolNodesByFile map[string][]exportObsidianNode
	nodesMap          map[string]exportObsidianNode
	edgesBySource     map[string][]exportObsidianEdge
	edgesByTarget     map[string][]exportObsidianEdge
	allRels           map[string][]obsidianRelPair
}

func ExportObsidian(db *sql.DB, projectID, vaultDir string) error {
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return err
	}

	if mkErr := os.MkdirAll(vaultDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create vault directory: %w", mkErr)
	}

	data := loadObsidianGraphData(db, projectID)

	for _, mem := range memories {
		if err := writeObsidianMemory(vaultDir, mem, data.allRels, data.edgesBySource, data.nodesMap); err != nil {
			return err
		}
	}
	for _, fn := range data.fileNodes {
		if err := writeObsidianCodeFile(vaultDir, fn, data.symbolNodesByFile, data.edgesBySource, data.edgesByTarget, data.nodesMap); err != nil {
			return err
		}
	}
	for _, pn := range data.packageNodes {
		if err := writeObsidianPackageFile(vaultDir, pn, data.edgesByTarget, data.nodesMap); err != nil {
			return err
		}
	}
	return nil
}

// loadObsidianGraphData loads nodes, edges, and memory relations for the vault.
func loadObsidianGraphData(db *sql.DB, projectID string) *obsidianGraphData {
	data := &obsidianGraphData{
		symbolNodesByFile: make(map[string][]exportObsidianNode),
		nodesMap:          make(map[string]exportObsidianNode),
		edgesBySource:     make(map[string][]exportObsidianEdge),
		edgesByTarget:     make(map[string][]exportObsidianEdge),
		allRels:           make(map[string][]obsidianRelPair),
	}
	loadObsidianNodes(db, projectID, data)
	loadObsidianEdges(db, projectID, data)
	loadObsidianRelations(db, projectID, data)
	return data
}

func loadObsidianNodes(db *sql.DB, projectID string, data *obsidianGraphData) {
	nodeRows, err := db.Query("SELECT id, node_type, label, path, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return
	}
	defer nodeRows.Close()
	for nodeRows.Next() {
		var n exportObsidianNode
		var pathVal, metaVal sql.NullString
		if errScan := nodeRows.Scan(&n.id, &n.nodeType, &n.label, &pathVal, &metaVal); errScan != nil {
			continue
		}
		if pathVal.Valid {
			n.path = pathVal.String
		}
		if metaVal.Valid {
			n.metadata = metaVal.String
		}
		data.nodesMap[n.id] = n
		switch n.nodeType {
		case "file", "document":
			data.fileNodes = append(data.fileNodes, n)
		case "package":
			data.packageNodes = append(data.packageNodes, n)
		case "function", "class":
			data.symbolNodesByFile[n.path] = append(data.symbolNodesByFile[n.path], n)
		}
	}
}

func loadObsidianEdges(db *sql.DB, projectID string, data *obsidianGraphData) {
	edgeRows, err := db.Query("SELECT source_id, target_id, relation_type, confidence, source_location FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e exportObsidianEdge
		if errScan := edgeRows.Scan(&e.sourceID, &e.targetID, &e.relType, &e.confidence, &e.sourceLoc); errScan != nil {
			continue
		}
		data.edgesBySource[e.sourceID] = append(data.edgesBySource[e.sourceID], e)
		data.edgesByTarget[e.targetID] = append(data.edgesByTarget[e.targetID], e)
	}
}

func loadObsidianRelations(db *sql.DB, projectID string, data *obsidianGraphData) {
	relRows, err := db.Query("SELECT source_id, target_id FROM memory_relations WHERE project_id = ?", projectID)
	if err != nil {
		return
	}
	defer relRows.Close()
	for relRows.Next() {
		var s, t string
		if relRows.Scan(&s, &t) == nil {
			data.allRels[s] = append(data.allRels[s], obsidianRelPair{s, t})
			data.allRels[t] = append(data.allRels[t], obsidianRelPair{s, t})
		}
	}
}

// writeObsidianMemory writes a single memory as an Obsidian markdown file.
func writeObsidianMemory(vaultDir string, mem *Memory, allRels map[string][]obsidianRelPair, edgesBySource map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) error {
	fm := memoryFrontmatter(mem)
	body := memoryBody(mem)
	body += relatedMemoriesSection(mem.ID, allRels)
	body += linksToCodebaseSection(mem.ID, edgesBySource, nodesMap)
	body += obsidianFooter()

	writePath := filepath.Join(vaultDir, mem.ID+".md")
	return os.WriteFile(writePath, []byte(fm+body), 0644)
}

// memoryFrontmatter renders the YAML frontmatter block for a memory file.
func memoryFrontmatter(mem *Memory) string {
	fm := fmt.Sprintf(`---
id: "%s"
category: "%s"
title: "%s"
created: "%s"
tags: [memory/%s]
`,
		mem.ID, mem.Category, mem.What, mem.CreatedAt.Format("2006-01-02"), mem.Category)

	if mem.TopicKey != "" {
		fm += fmt.Sprintf("topic_key: \"%s\"\nrevision: %d\n", mem.TopicKey, mem.RevisionCount)
	}
	if mem.WherePath != "" {
		fm += fmt.Sprintf("path: \"%s\"\n", mem.WherePath)
	}
	return fm + "---\n\n"
}

// memoryBody renders the markdown body sections of a memory file.
func memoryBody(mem *Memory) string {
	body := fmt.Sprintf("# %s\n\n**Category:** `%s`\n\n", mem.What, mem.Category)
	body += optionalSection("Why", mem.Why)
	body += optionalSection("Learned", mem.Learned)
	if mem.WherePath != "" {
		body += fmt.Sprintf("**Path:** [[code/%s|%s]]\n\n", mem.WherePath, mem.WherePath)
	}
	body += optionalSection("Impact", mem.Impact)
	body += optionalSection("Errors Faced", mem.ErrorsFaced)
	body += optionalSection("Next Steps", mem.NextSteps)
	return body
}

// optionalSection renders a "## <title>\n<content>\n\n" block when content is
// non-empty.
func optionalSection(title, content string) string {
	if content == "" {
		return ""
	}
	return fmt.Sprintf("## %s\n%s\n\n", title, content)
}

// relatedMemoriesSection renders wikilinks to related memories.
func relatedMemoriesSection(memID string, allRels map[string][]obsidianRelPair) string {
	rels := allRels[memID]
	if len(rels) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Related Memories\n")
	for _, r := range rels {
		otherID := r.target
		if r.target == memID {
			otherID = r.source
		}
		fmt.Fprintf(&sb, "- [[%s]]\n", otherID)
	}
	sb.WriteString("\n")
	return sb.String()
}

// linksToCodebaseSection renders wikilinks from a memory to the code nodes it
// is bound to via rationale_for edges.
func linksToCodebaseSection(memID string, edgesBySource map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) string {
	rationaleEdges := edgesBySource[memID]
	if len(rationaleEdges) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Links to Codebase\n")
	for _, edge := range rationaleEdges {
		if edge.relType != "rationale_for" {
			continue
		}
		targetNode, exists := nodesMap[edge.targetID]
		if !exists {
			continue
		}
		if targetNode.nodeType == "function" || targetNode.nodeType == "class" {
			fmt.Fprintf(&sb, "- [[code/%s#%s|%s (%s)]]\n", targetNode.path, targetNode.label, targetNode.label, targetNode.nodeType)
		} else {
			fmt.Fprintf(&sb, "- [[code/%s|%s (%s)]]\n", targetNode.id, targetNode.id, targetNode.nodeType)
		}
	}
	sb.WriteString("\n")
	return sb.String()
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
func writeObsidianCodeFile(vaultDir string, fn exportObsidianNode, symbolNodesByFile map[string][]exportObsidianNode, edgesBySource, edgesByTarget map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) error {
	filePath := filepath.Join(vaultDir, "code", fn.id+".md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create code subfolders for %s: %w", fn.id, err)
	}

	fm := codeFileFrontmatter(fn)

	body := fmt.Sprintf("# %s\n\n**Type:** `%s`\n\n", fn.label, fn.nodeType)
	body += definedSymbolsSection(fn, symbolNodesByFile, edgesBySource, edgesByTarget, nodesMap)
	body += edgeLinkSection(fn.id, edgesBySource[fn.id], nodesMap, "## Dependencies", false, true)
	body += edgeLinkSection(fn.id, edgesByTarget[fn.id], nodesMap, "## Dependents", true, false)
	body += associatedMemoriesSection(fn, symbolNodesByFile, edgesByTarget)
	body += obsidianFooter()
	return os.WriteFile(filePath, []byte(fm+body), 0644)
}

// codeFileFrontmatter renders the YAML frontmatter for a code-file note.
func codeFileFrontmatter(fn exportObsidianNode) string {
	fm := fmt.Sprintf(`---
id: "%s"
type: "%s"
label: "%s"
path: "%s"
`, fn.id, fn.nodeType, fn.label, fn.path)

	if fn.metadata != "" {
		var metaMap map[string]interface{}
		_ = json.Unmarshal([]byte(fn.metadata), &metaMap)
		for k, v := range metaMap {
			if k != "line" && k != "exported" {
				fm += fmt.Sprintf("%s: %v\n", k, v)
			}
		}
	}
	return fm + "---\n\n"
}

// definedSymbolsSection renders the "Defined Symbols" block of a code-file note.
func definedSymbolsSection(fn exportObsidianNode, symbolNodesByFile map[string][]exportObsidianNode, edgesBySource, edgesByTarget map[string][]exportObsidianEdge, nodesMap map[string]exportObsidianNode) string {
	symbols := symbolNodesByFile[fn.id]
	if len(symbols) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Defined Symbols\n\n")
	for _, sym := range symbols {
		writeSymbolBlock(&sb, fn.id, sym, edgesBySource[sym.id], edgesByTarget[sym.id], nodesMap)
	}
	return sb.String()
}

// writeSymbolBlock renders one symbol's heading, metadata, calls, and callers.
func writeSymbolBlock(sb *strings.Builder, fileID string, sym exportObsidianNode, srcEdges, tgtEdges []exportObsidianEdge, nodesMap map[string]exportObsidianNode) {
	fmt.Fprintf(sb, "### %s\n", sym.label)
	fmt.Fprintf(sb, "- **Type:** `%s`\n", sym.nodeType)
	if lineVal, ok := symbolMetaValue(sym.metadata, "line"); ok {
		fmt.Fprintf(sb, "- **Declared on:** Line %v\n", lineVal)
	}
	writeCallSection(sb, fileID, srcEdges, nodesMap, "- **Calls:**\n", false)
	writeCallSection(sb, fileID, tgtEdges, nodesMap, "- **Called by:**\n", true)
}

// writeCallSection renders a "calls" (useSource=false) or "called by"
// (useSource=true) list for one symbol.
func writeCallSection(sb *strings.Builder, fileID string, edges []exportObsidianEdge, nodesMap map[string]exportObsidianNode, header string, useSource bool) {
	found := false
	for _, edge := range edges {
		if edge.relType != "calls" {
			continue
		}
		if !found {
			sb.WriteString(header)
			found = true
		}
		otherID := edge.targetID
		if useSource {
			otherID = edge.sourceID
		}
		loc := ""
		if edge.sourceLoc.Valid {
			loc = fmt.Sprintf(" (at %s)", edge.sourceLoc.String)
		}
		if node, exists := nodesMap[otherID]; exists {
			fmt.Fprintf(sb, "  - %s%s\n", makeRelativeLink(fileID, node.path, node.label, node.label, "calls"), loc)
		} else {
			fmt.Fprintf(sb, "  - `%s` (unresolved)%s\n", otherID, loc)
		}
	}
}

// symbolMetaValue extracts a single key from a node's JSON metadata.
func symbolMetaValue(metadata, key string) (interface{}, bool) {
	if metadata == "" {
		return nil, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// edgeLinkSection renders an import/depends_on/references list from one side of
// a node's edges. useSource selects the neighbour side; withRelTypePrefix
// prefixes each line with the relation type.
func edgeLinkSection(fnID string, edges []exportObsidianEdge, nodesMap map[string]exportObsidianNode, header string, useSource, withRelTypePrefix bool) string {
	var sb strings.Builder
	found := false
	for _, edge := range edges {
		if !isDependencyEdge(edge.relType) {
			continue
		}
		otherID := edge.targetID
		if useSource {
			otherID = edge.sourceID
		}
		if !found {
			sb.WriteString(header + "\n")
			found = true
		}
		if node, exists := nodesMap[otherID]; exists {
			link := makeRelativeLink(fnID, node.id, "", node.label, edge.relType)
			if withRelTypePrefix {
				fmt.Fprintf(&sb, "- **%s**: %s\n", edge.relType, link)
			} else {
				fmt.Fprintf(&sb, "- %s\n", link)
			}
			continue
		}
		if withRelTypePrefix {
			fmt.Fprintf(&sb, "- **%s**: `%s` (unresolved)\n", edge.relType, otherID)
		} else {
			fmt.Fprintf(&sb, "- `%s` (unresolved)\n", otherID)
		}
	}
	if found {
		sb.WriteString("\n")
	}
	return sb.String()
}

func isDependencyEdge(relType string) bool {
	return relType == "imports" || relType == "depends_on" || relType == "references"
}

// associatedMemoriesSection renders the rationale_for memories bound to the file
// or to any of its symbols.
func associatedMemoriesSection(fn exportObsidianNode, symbolNodesByFile map[string][]exportObsidianNode, edgesByTarget map[string][]exportObsidianEdge) string {
	rationales := collectReferencingRationales(fn, symbolNodesByFile, edgesByTarget)
	if len(rationales) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Associated Memories / Decisions\n")
	for _, re := range rationales {
		fmt.Fprintf(&sb, "- %s\n", makeRelativeLink(fn.id, re.sourceID, "", re.sourceID, "rationale_for"))
	}
	sb.WriteString("\n")
	return sb.String()
}

// collectReferencingRationales gathers rationale_for edges targeting the file and
// each of its symbols.
func collectReferencingRationales(fn exportObsidianNode, symbolNodesByFile map[string][]exportObsidianNode, edgesByTarget map[string][]exportObsidianEdge) []exportObsidianEdge {
	var out []exportObsidianEdge
	appendRationaleEdges := func(id string) {
		for _, re := range edgesByTarget[id] {
			if re.relType == "rationale_for" {
				out = append(out, re)
			}
		}
	}
	appendRationaleEdges(fn.id)
	for _, sym := range symbolNodesByFile[fn.id] {
		appendRationaleEdges(sym.id)
	}
	return out
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

	body += obsidianFooter()
	return os.WriteFile(filePath, []byte(fm+body), 0644)
}
