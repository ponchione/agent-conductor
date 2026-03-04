package verify

// DiffSegment is a logical group of related file diffs for analysis.
type DiffSegment struct {
	Files           []string `json:"files"`
	Diff            string   `json:"diff"`
	OriginalContent string   `json:"original_content,omitempty"`
}

// SegmentVerdict is the per-segment output from the analyze step.
type SegmentVerdict struct {
	Files             []string              `json:"files"`
	AlignedWithIntent bool                  `json:"aligned_with_intent"`
	CriteriaAssessed  []CriterionAssessment `json:"criteria_assessed"`
	Bugs              []string              `json:"bugs"`
	StyleIssues       []string              `json:"style_issues"`
	Concerns          []string              `json:"concerns"`
	AnalysisFailed    bool                  `json:"analysis_failed"`
	FailureReason     string                `json:"failure_reason,omitempty"`
}

// CriterionAssessment is a single acceptance criterion evaluation.
type CriterionAssessment struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Notes     string `json:"notes"`
}

// PreCheckResult represents a pre-check outcome passed in from the worker.
// The orchestrator does not run pre-checks — it receives them as input.
type PreCheckResult struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Notes     string `json:"notes"`
}
