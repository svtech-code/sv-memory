package schema

// Node types understood by the graph. 'file' | 'package' | 'function' | 'class'
// | 'section' | 'table' | 'view' | 'index' | 'type' | 'rationale' are produced
// by the code scanner; 'document' nodes are created for saved memories (see
// graph.EnsureMemoryRationaleEdge). The spec-driven decision engine extends the
// vocabulary with 'spec', 'decision', and 'rule' nodes so proposals and their
// governing invariants become first-class citizens of the graph.
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
	NodeTypeDecision  = "decision"
	NodeTypeRule      = "rule"
)

// Edge relation types understood by the graph. The code scanner produces
// 'imports' | 'calls' | 'depends_on' | 'contains' | 'references', and saved
// memories are linked to their code nodes with 'rationale_for'. The spec-driven
// decision engine adds 'affects' (a change/proposal touches code entities),
// 'constrains' (a rule bounds a decision), and 'implements' (a decision or
// entity fulfills a spec requirement).
const (
	EdgeImports      = "imports"
	EdgeCalls        = "calls"
	EdgeDependsOn    = "depends_on"
	EdgeContains     = "contains"
	EdgeReferences   = "references"
	EdgeRationaleFor = "rationale_for"
	EdgeAffects      = "affects"
	EdgeConstrains   = "constrains"
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
