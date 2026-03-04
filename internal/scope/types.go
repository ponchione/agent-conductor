package scope

// InvestigationTarget is a package/directory identified by the decompose step
// for deeper analysis.
type InvestigationTarget struct {
	Path              string `json:"path"`
	Rationale         string `json:"rationale"`
	InvestigationType string `json:"investigation_type"` // primary_modification, supporting_modification, reference_only
}

// AreaAnalysis is the per-target output of the analyze step.
type AreaAnalysis struct {
	Target               string    `json:"target"`
	FilesToModify        []FileRef `json:"files_to_modify"`
	FilesToReference     []FileRef `json:"files_to_reference"`
	NewFiles             []NewFile `json:"new_files"`
	InterfacesToPreserve []string  `json:"interfaces_to_preserve"`
	Concerns             []string  `json:"concerns"`
	AnalysisFailed       bool      `json:"analysis_failed"`
	FailureReason        string    `json:"failure_reason,omitempty"`
}

// FileRef is a file path with a reason (scope-local to avoid importing models).
type FileRef struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// NewFile is a file to be created.
type NewFile struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// CrossCutAnalysis is the output of the crosscut step, identifying
// shared types and cross-target dependencies.
type CrossCutAnalysis struct {
	SharedTypes      []SharedType `json:"shared_types"`
	Dependencies     []Dependency `json:"dependencies"`
	IntegrationRisks []string     `json:"integration_risks"`
	SuggestedOrder   []string     `json:"suggested_order"`
}

// SharedType is a type used across multiple investigation targets.
type SharedType struct {
	Name    string   `json:"name"`
	Package string   `json:"package"`
	UsedBy  []string `json:"used_by"`
}

// Dependency represents a cross-target dependency.
type Dependency struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // calls, imports, implements
}

// SubCallRecord tracks metrics for a single LLM sub-call within the
// recursive scope pipeline.
type SubCallRecord struct {
	Phase            string  `json:"phase"`
	Step             string  `json:"step"`
	TargetPath       string  `json:"target_path,omitempty"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	LatencyMs        int64   `json:"latency_ms"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	Success          bool    `json:"success"`
	ErrorMessage     string  `json:"error_message,omitempty"`
}
