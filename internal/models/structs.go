package models

import (
	"encoding/json"
	"strings"
)

// ContextPackage is the output of the scope phase, written to context_package.json.
// Schema must match ScopePrompt in internal/templates/prompts.go.
type ContextPackage struct {
	Summary             string    `json:"summary"`
	EstimatedComplexity string    `json:"estimated_complexity"` // low, medium, high
	FilesToModify       []FileRef `json:"files_to_modify"`
	FilesToReference    []FileRef `json:"files_to_reference"`
	SQLFiles            []FileRef `json:"sql_files"`
	NewFiles            []NewFile `json:"new_files"`
	Dependencies        []string  `json:"dependencies"`
	BuildInstructions   string    `json:"build_instructions"`
}

// FileWithContent pairs a file path with its inline source content.
type FileWithContent struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// FileRef is a file path with a reason explaining why it is relevant.
type FileRef struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type NewFile struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// VerificationReport is the output of the verify phase.
// Schema must match VerifyPrompt in internal/templates/prompts.go.
type VerificationReport struct {
	Status             string             `json:"status"` // PASS, WARN, FAIL
	Summary            string             `json:"summary"`
	ScopeDrift         ScopeDrift         `json:"scope_drift"`
	Completeness       Completeness       `json:"completeness"`
	PatternConsistency PatternConsistency `json:"pattern_consistency"`
	Concerns           []string           `json:"concerns"`
}

type ScopeDrift struct {
	Detected      bool           `json:"detected"`
	UnscopedFiles []UnscopedFile `json:"unscoped_files"`
}

type UnscopedFile struct {
	Path             string `json:"path"`
	ReasonConcerning string `json:"reason_concerning"`
}

type Completeness struct {
	AllCriteriaMet  bool              `json:"all_criteria_met"`
	CriteriaResults []CriterionResult `json:"criteria_results"`
}

type CriterionResult struct {
	CriterionID      string `json:"-"`
	Criterion        string `json:"-"`
	Required         *bool  `json:"-"`
	Result           string `json:"-"`
	VerificationKind string `json:"-"`
	Notes            string `json:"-"`
}

const (
	CriterionResultMet          = "met"
	CriterionResultUnmet        = "unmet"
	CriterionResultUnassessable = "unassessable"
)

func (c CriterionResult) MarshalJSON() ([]byte, error) {
	type payload struct {
		CriterionID      string `json:"criterion_id,omitempty"`
		Description      string `json:"description,omitempty"`
		Required         *bool  `json:"required,omitempty"`
		Result           string `json:"result,omitempty"`
		VerificationKind string `json:"verification_kind,omitempty"`
		Notes            string `json:"notes,omitempty"`
	}
	return json.Marshal(payload{
		CriterionID:      c.CriterionID,
		Description:      c.Criterion,
		Required:         c.Required,
		Result:           c.Result,
		VerificationKind: c.VerificationKind,
		Notes:            c.Notes,
	})
}

func (c *CriterionResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		CriterionID      string `json:"criterion_id"`
		Description      string `json:"description"`
		Required         *bool  `json:"required"`
		Result           string `json:"result"`
		VerificationKind string `json:"verification_kind"`
		Notes            string `json:"notes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.CriterionID = raw.CriterionID
	c.Criterion = strings.TrimSpace(raw.Description)
	c.Required = raw.Required
	c.Result = normalizeCriterionResult(raw.Result)
	c.VerificationKind = raw.VerificationKind
	c.Notes = raw.Notes
	return nil
}

func (c CriterionResult) NormalizedResult() string {
	if normalized := normalizeCriterionResult(c.Result); normalized != "" {
		return normalized
	}
	return CriterionResultUnassessable
}

func normalizeCriterionResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case CriterionResultMet:
		return CriterionResultMet
	case CriterionResultUnmet:
		return CriterionResultUnmet
	case CriterionResultUnassessable:
		return CriterionResultUnassessable
	default:
		return ""
	}
}

type PatternConsistency struct {
	FollowsConventions bool     `json:"follows_conventions"`
	Issues             []string `json:"issues"`
}

// FullContextPackage is the structured JSON context handed to the build agent.
// It combines work order data, scope LLM output, and orchestrator directives.
type FullContextPackage struct {
	WorkOrder  WorkOrderContext `json:"work_order"`
	Scope      ScopeContext     `json:"scope"`
	Directives Directives       `json:"directives"`
}

// WorkOrderContext is the work order fields relevant to the build agent.
type WorkOrderContext struct {
	SchemaVersion           int                        `json:"schema_version,omitempty"`
	Title                   string                     `json:"title"`
	Type                    string                     `json:"type"`
	TargetModule            string                     `json:"target_module"`
	ReferenceModule         string                     `json:"reference_module,omitempty"`
	Requirements            []WorkOrderRequirement     `json:"requirements,omitempty"`
	AcceptanceCriteria      []string                   `json:"acceptance_criteria"`
	TypedAcceptanceCriteria []TypedAcceptanceCriterion `json:"typed_acceptance_criteria,omitempty"`
	Constraints             []string                   `json:"constraints"`
	KnownFiles              []string                   `json:"known_files"`
}

// ScopeContext holds the scope LLM's analysis enriched with RAG results.
type ScopeContext struct {
	FilesToModify       []FileRef         `json:"files_to_modify"`
	FilesToReference    []FileRef         `json:"files_to_reference"`
	FileContents        []FileWithContent `json:"file_contents,omitempty"`
	RelevantCode        []RelevantCode    `json:"relevant_code"`
	Summary             string            `json:"summary"`
	EstimatedComplexity string            `json:"estimated_complexity"`
	BuildInstructions   string            `json:"build_instructions,omitempty"`
	NewFiles            []NewFile         `json:"new_files,omitempty"`
	SQLFiles            []FileRef         `json:"sql_files,omitempty"`
	Dependencies        []string          `json:"dependencies,omitempty"`
}

// CodeRef identifies a function or method in the codebase.
// JSON-facing equivalent of rag.FuncRef (lives in models to avoid import cycles).
type CodeRef struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	File    string `json:"file,omitempty"`
}

// RelevantCode is a RAG search result mapped to structured data.
type RelevantCode struct {
	Function        string    `json:"function"`
	File            string    `json:"file"`
	Description     string    `json:"description"`
	Body            string    `json:"body,omitempty"`
	Signature       string    `json:"signature,omitempty"`
	Calls           []CodeRef `json:"calls,omitempty"`
	CalledBy        []CodeRef `json:"called_by,omitempty"`
	IsDependencyHop bool      `json:"is_dependency_hop,omitempty"`
	QueryHitCount   int       `json:"query_hit_count,omitempty"`
}

// Directives are orchestrator-provided instructions for the build agent.
type Directives struct {
	BranchName          string `json:"branch_name"`
	ReferenceModuleNote string `json:"reference_module_note,omitempty"`
}
