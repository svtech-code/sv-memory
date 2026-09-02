package graph

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/graph/extractor"
)

// GraphDiffReport contains the structural code and dependency differences
// between a Git base reference and the working tree.
type GraphDiffReport struct {
	ProjectID           string           `json:"project_id"`
	BaseRef             string           `json:"base_ref"`
	ChangedFilesCount   int              `json:"changed_files_count"`
	ChangedFiles        []FileDiffStatus `json:"changed_files"`
	AddedSymbols        []SymbolDiffItem `json:"added_symbols"`
	RemovedSymbols      []SymbolDiffItem `json:"removed_symbols"`
	ModifiedSymbols     []SymbolDiffItem `json:"modified_symbols"`
	AddedDependencies   []DependencyDiff `json:"added_dependencies"`
	RemovedDependencies []DependencyDiff `json:"removed_dependencies"`
	HighImpactNodes     []ImpactRiskNode `json:"high_impact_nodes,omitempty"`
}

// FileDiffStatus reports a single changed file with its Git status.
type FileDiffStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "A", "M", "D", "R"
}

// SymbolDiffItem reports an added, removed, or modified code symbol.
type SymbolDiffItem struct {
	File     string `json:"file"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// DependencyDiff reports an added or removed import/call relationship.
type DependencyDiff struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Type       string `json:"type"` // "imports" | "calls"
	Confidence string `json:"confidence"`
}

// ImpactRiskNode reports a changed entity with its blast radius impact in the graph.
type ImpactRiskNode struct {
	NodeID           string `json:"node_id"`
	Label            string `json:"label"`
	Path             string `json:"path"`
	BlastRadiusCount int    `json:"blast_radius_count"`
	RiskLevel        string `json:"risk_level"` // "HIGH" | "MEDIUM" | "LOW"
}

const gitTimeout = 10 * time.Second

func runGitCommand(projPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projPath
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// ResolveDefaultBaseRef determines the fallback base reference when none is provided.
func ResolveDefaultBaseRef(projPath string) string {
	// Try origin/HEAD first
	if out, err := runGitCommand(projPath, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" && !strings.Contains(trimmed, "fatal") {
			return trimmed
		}
	}
	// Try main
	if _, err := runGitCommand(projPath, "rev-parse", "--verify", "main"); err == nil {
		return "main"
	}
	// Try master
	if _, err := runGitCommand(projPath, "rev-parse", "--verify", "master"); err == nil {
		return "master"
	}
	return "HEAD~1"
}

// ComputeGraphDiff compares structural code elements (symbols, calls, imports,
// blast radius) between baseRef and the current working directory.
func ComputeGraphDiff(db *sql.DB, projectID, projPath, baseRef string, includeBlastRadius bool) (*GraphDiffReport, error) {
	if baseRef == "" {
		baseRef = ResolveDefaultBaseRef(projPath)
	}

	// 1. Verify git repository and baseRef exist
	if _, err := runGitCommand(projPath, "rev-parse", "--verify", baseRef); err != nil {
		return nil, fmt.Errorf("git revision %q not found: %w", baseRef, err)
	}

	// 2. Query changed files
	codeChangedFiles, allChangedFiles, err := collectGitChangedFiles(projPath, baseRef)
	if err != nil {
		return nil, err
	}

	report := &GraphDiffReport{
		ProjectID:         projectID,
		BaseRef:           baseRef,
		ChangedFiles:      codeChangedFiles,
		ChangedFilesCount: len(codeChangedFiles),
	}

	// 3. For each changed file, parse symbols and dependencies
	for _, f := range codeChangedFiles {
		ext := filepath.Ext(f.Path)
		status := f.Status
		if len(status) > 1 {
			status = status[:1]
		}

		switch status {
		case "A":
			diffAddedFile(projPath, f, ext, report)
		case "D":
			diffDeletedFile(projPath, baseRef, f, ext, report)
		case "M":
			diffModifiedFile(projPath, baseRef, f, ext, report)
		}
	}

	// 4. Calculate blast radius impact for affected files/symbols if requested
	if includeBlastRadius && db != nil && projectID != "" {
		calculateBlastRadiusForDiff(db, projectID, allChangedFiles, report)
	}

	return report, nil
}

func collectGitChangedFiles(projPath, baseRef string) (codeChanged []FileDiffStatus, allChanged []FileDiffStatus, err error) {
	out, err := runGitCommand(projPath, "diff", "--name-status", baseRef)
	if err != nil {
		return nil, nil, fmt.Errorf("failed running git diff: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	seenFiles := make(map[string]bool)

	for _, l := range lines {
		parts := strings.Fields(l)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		if len(parts) >= 3 && (status[0] == 'R' || status[0] == 'C') {
			path = parts[2]
		}
		allChanged = append(allChanged, FileDiffStatus{Path: path, Status: status})
		seenFiles[path] = true
	}

	if untrackedOut, uErr := runGitCommand(projPath, "ls-files", "--others", "--exclude-standard"); uErr == nil {
		untrackedLines := strings.Split(strings.TrimSpace(string(untrackedOut)), "\n")
		for _, uPath := range untrackedLines {
			uPath = strings.TrimSpace(uPath)
			if uPath != "" && !seenFiles[uPath] {
				allChanged = append(allChanged, FileDiffStatus{Path: uPath, Status: "A"})
				seenFiles[uPath] = true
			}
		}
	}

	for _, f := range allChanged {
		ext := filepath.Ext(f.Path)
		if _, supported := languageFromExt[ext]; supported {
			codeChanged = append(codeChanged, f)
		}
	}
	return codeChanged, allChanged, nil
}

func diffAddedFile(projPath string, f FileDiffStatus, ext string, report *GraphDiffReport) {
	content, err := os.ReadFile(filepath.Join(projPath, f.Path))
	if err != nil {
		return
	}
	syms, imps, _ := currentExtractor.Extract(content, f.Path, ext)
	for _, s := range syms {
		if isSignificantSymbol(s) {
			report.AddedSymbols = append(report.AddedSymbols, SymbolDiffItem{
				File:     f.Path,
				Name:     s.Name,
				Type:     s.Type,
				Line:     s.Line,
				Exported: s.Exported,
			})
		}
	}
	for _, imp := range imps {
		report.AddedDependencies = append(report.AddedDependencies, DependencyDiff{
			Source:     f.Path,
			Target:     imp,
			Type:       "imports",
			Confidence: "EXTRACTED",
		})
	}
}

func diffDeletedFile(projPath, baseRef string, f FileDiffStatus, ext string, report *GraphDiffReport) {
	oldData, err := runGitCommand(projPath, "show", fmt.Sprintf("%s:%s", baseRef, f.Path))
	if err != nil {
		return
	}
	syms, imps, _ := currentExtractor.Extract(oldData, f.Path, ext)
	for _, s := range syms {
		if isSignificantSymbol(s) {
			report.RemovedSymbols = append(report.RemovedSymbols, SymbolDiffItem{
				File:     f.Path,
				Name:     s.Name,
				Type:     s.Type,
				Line:     s.Line,
				Exported: s.Exported,
			})
		}
	}
	for _, imp := range imps {
		report.RemovedDependencies = append(report.RemovedDependencies, DependencyDiff{
			Source:     f.Path,
			Target:     imp,
			Type:       "imports",
			Confidence: "EXTRACTED",
		})
	}
}

func diffModifiedFile(projPath, baseRef string, f FileDiffStatus, ext string, report *GraphDiffReport) {
	currContent, _ := os.ReadFile(filepath.Join(projPath, f.Path))
	oldData, _ := runGitCommand(projPath, "show", fmt.Sprintf("%s:%s", baseRef, f.Path))

	currSyms, currImps, _ := currentExtractor.Extract(currContent, f.Path, ext)
	oldSyms, oldImps, _ := currentExtractor.Extract(oldData, f.Path, ext)

	currMap := make(map[string]extractor.Symbol)
	for _, s := range currSyms {
		if isSignificantSymbol(s) {
			currMap[s.Name] = s
		}
	}

	oldMap := make(map[string]extractor.Symbol)
	for _, s := range oldSyms {
		if isSignificantSymbol(s) {
			oldMap[s.Name] = s
		}
	}

	for name, cs := range currMap {
		if os, exists := oldMap[name]; !exists {
			report.AddedSymbols = append(report.AddedSymbols, SymbolDiffItem{
				File:     f.Path,
				Name:     cs.Name,
				Type:     cs.Type,
				Line:     cs.Line,
				Exported: cs.Exported,
			})
		} else if cs.Line != os.Line || cs.Exported != os.Exported {
			report.ModifiedSymbols = append(report.ModifiedSymbols, SymbolDiffItem{
				File:     f.Path,
				Name:     cs.Name,
				Type:     cs.Type,
				Line:     cs.Line,
				Exported: cs.Exported,
			})
		}
	}

	for name, os := range oldMap {
		if _, exists := currMap[name]; !exists {
			report.RemovedSymbols = append(report.RemovedSymbols, SymbolDiffItem{
				File:     f.Path,
				Name:     os.Name,
				Type:     os.Type,
				Line:     os.Line,
				Exported: os.Exported,
			})
		}
	}

	currImpMap := make(map[string]bool)
	for _, imp := range currImps {
		currImpMap[imp] = true
	}
	oldImpMap := make(map[string]bool)
	for _, imp := range oldImps {
		oldImpMap[imp] = true
	}

	for imp := range currImpMap {
		if !oldImpMap[imp] {
			report.AddedDependencies = append(report.AddedDependencies, DependencyDiff{
				Source:     f.Path,
				Target:     imp,
				Type:       "imports",
				Confidence: "EXTRACTED",
			})
		}
	}
	for imp := range oldImpMap {
		if !currImpMap[imp] {
			report.RemovedDependencies = append(report.RemovedDependencies, DependencyDiff{
				Source:     f.Path,
				Target:     imp,
				Type:       "imports",
				Confidence: "EXTRACTED",
			})
		}
	}
}

func calculateBlastRadiusForDiff(db *sql.DB, projectID string, files []FileDiffStatus, report *GraphDiffReport) {
	seenNodes := make(map[string]bool)
	for _, f := range files {
		if seenNodes[f.Path] {
			continue
		}
		seenNodes[f.Path] = true
		nodes, bErr := CalculateBlastRadius(db, projectID, f.Path, 3, 10)
		if bErr == nil && len(nodes) > 0 {
			risk := "LOW"
			if len(nodes) >= 5 {
				risk = "HIGH"
			} else if len(nodes) >= 2 {
				risk = "MEDIUM"
			}
			report.HighImpactNodes = append(report.HighImpactNodes, ImpactRiskNode{
				NodeID:           f.Path,
				Label:            filepath.Base(f.Path),
				Path:             f.Path,
				BlastRadiusCount: len(nodes),
				RiskLevel:        risk,
			})
		}
	}
	sort.Slice(report.HighImpactNodes, func(i, j int) bool {
		return report.HighImpactNodes[i].BlastRadiusCount > report.HighImpactNodes[j].BlastRadiusCount
	})
}

func isSignificantSymbol(s extractor.Symbol) bool {
	if s.Name == "" {
		return false
	}
	if s.Type == "section" || s.Type == "code_block" || s.Type == "diagram" {
		return false
	}
	return true
}

// RenderGraphDiffReport formats a GraphDiffReport as clean Markdown.
func RenderGraphDiffReport(r *GraphDiffReport) string {
	if r == nil {
		return "No graph diff report available."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Structural Graph Diff vs `%s`\n\n", r.BaseRef)
	fmt.Fprintf(&sb, "**Changed Files:** %d | **Added Symbols:** %d | **Removed Symbols:** %d | **Modified Symbols:** %d\n",
		r.ChangedFilesCount, len(r.AddedSymbols), len(r.RemovedSymbols), len(r.ModifiedSymbols))

	if r.ChangedFilesCount == 0 {
		sb.WriteString("\n*No code or structural differences detected between working tree and base reference.*\n")
		return sb.String()
	}

	if len(r.AddedSymbols) > 0 {
		sb.WriteString("\n### Added Symbols\n\n")
		sb.WriteString("| File | Symbol | Type | Line |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, s := range r.AddedSymbols {
			exp := ""
			if s.Exported {
				exp = " *(exported)*"
			}
			fmt.Fprintf(&sb, "| `%s` | `%s`%s | `%s` | %d |\n", s.File, s.Name, exp, s.Type, s.Line)
		}
	}

	if len(r.RemovedSymbols) > 0 {
		sb.WriteString("\n### Removed Symbols\n\n")
		sb.WriteString("| File | Symbol | Type | Line |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, s := range r.RemovedSymbols {
			fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` | %d |\n", s.File, s.Name, s.Type, s.Line)
		}
	}

	if len(r.ModifiedSymbols) > 0 {
		sb.WriteString("\n### Modified Symbols\n\n")
		sb.WriteString("| File | Symbol | Type | Line |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, s := range r.ModifiedSymbols {
			fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` | %d |\n", s.File, s.Name, s.Type, s.Line)
		}
	}

	if len(r.AddedDependencies) > 0 {
		sb.WriteString("\n### Added Dependencies\n\n")
		sb.WriteString("| Source | Target | Type |\n")
		sb.WriteString("| :--- | :--- | :--- |\n")
		for _, d := range r.AddedDependencies {
			fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` |\n", d.Source, d.Target, d.Type)
		}
	}

	if len(r.RemovedDependencies) > 0 {
		sb.WriteString("\n### Removed Dependencies\n\n")
		sb.WriteString("| Source | Target | Type |\n")
		sb.WriteString("| :--- | :--- | :--- |\n")
		for _, d := range r.RemovedDependencies {
			fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` |\n", d.Source, d.Target, d.Type)
		}
	}

	if len(r.HighImpactNodes) > 0 {
		sb.WriteString("\n### Blast Radius & Architectural Impact\n\n")
		sb.WriteString("| File / Node | Upstream Consumers | Risk Level |\n")
		sb.WriteString("| :--- | :---: | :---: |\n")
		for _, n := range r.HighImpactNodes {
			fmt.Fprintf(&sb, "| `%s` | %d | **%s** |\n", n.Path, n.BlastRadiusCount, n.RiskLevel)
		}
	}

	return sb.String()
}
