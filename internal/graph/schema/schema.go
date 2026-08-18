package schema

// Node types understood by the graph. 'file' | 'package' | 'function' | 'class'
// | 'section' | 'table' | 'view' | 'index' | 'type' | 'rationale' are produced
// by the code scanner; 'document' nodes are created for saved memories (see
// graph.EnsureMemoryRationaleEdge), 'sql' nodes for SQL entities, and the
// spec-driven decision engine adds 'spec' nodes for proposal capabilities.
const (
	NodeTypeFile      = "file"
	NodeTypePackage   = "package"
	NodeTypeFunction  = "function"
	NodeTypeClass     = "class"
	NodeTypeSection   = "section"
	NodeTypeTable     = "table"
	NodeTypeView      = "view"
	NodeTypeIndex     = "index"
	NodeTypeType      = "type"
	NodeTypeRationale = "rationale"
	NodeTypeSQL       = "sql"
	NodeTypeDocument  = "document"
	NodeTypeSpec      = "spec"
)

// Edge relation types understood by the graph. The code scanner produces
// 'imports' | 'calls' | 'depends_on' | 'contains' | 'references', saved
// memories are linked to their code nodes with 'rationale_for', and the spec
// engine links capabilities to their code entities with 'implements'.
const (
	EdgeImports      = "imports"
	EdgeCalls        = "calls"
	EdgeDependsOn    = "depends_on"
	EdgeContains     = "contains"
	EdgeReferences   = "references"
	EdgeRationaleFor = "rationale_for"
	EdgeImplements   = "implements"
)

// Node represents a vertex in the code dependency graph.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Label    string                 `json:"label"`
	Path     string                 `json:"path"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`
	Confidence     string `json:"confidence"`                // 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
	SourceLocation string `json:"source_location,omitempty"` // line number or empty
}
