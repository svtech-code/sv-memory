package schema

// Node represents a vertex in the code dependency graph.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // 'file' | 'module' | 'package' | 'component' | 'concept'
	Label    string                 `json:"label"`
	Path     string                 `json:"path"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`             // 'imports' | 'calls' | 'depends_on' | 'potential_conflict'
	Confidence     string `json:"confidence"`                // 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
	SourceLocation string `json:"source_location,omitempty"` // line number or empty
}
