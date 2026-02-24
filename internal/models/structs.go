package models

import "encoding/json"

// ContextPackage is the output of the scope phase, written to context_package.json.
// Schema must match ScopePrompt in internal/templates/prompts.go.
type ContextPackage struct {
	Summary             string       `json:"summary"`
	EstimatedComplexity string       `json:"estimated_complexity"` // low, medium, high
	FilesToModify       []FileRef    `json:"files_to_modify"`
	FilesToReference    []FileRef    `json:"files_to_reference"`
	SQLFiles            []FileRef    `json:"sql_files"`
	NewFiles            []NewFile    `json:"new_files"`
	Dependencies        []string     `json:"dependencies"`
	BuildInstructions   string       `json:"build_instructions"`
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
	Detected       bool      `json:"detected"`
	UnscopedFiles  []UnscopedFile `json:"unscoped_files"`
}

func (s *ScopeDrift) UnmarshalJSON(data []byte) error {
	var raw struct {
		Detected      bool            `json:"detected"`
		UnscopedFiles json.RawMessage `json:"unscoped_files"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Detected = raw.Detected
	if len(raw.UnscopedFiles) == 0 || string(raw.UnscopedFiles) == "null" {
		return nil
	}
	// First attempt: array of objects.
	if err := json.Unmarshal(raw.UnscopedFiles, &s.UnscopedFiles); err == nil {
		return nil
	}
	// Fallback: array of plain strings.
	var paths []string
	if err := json.Unmarshal(raw.UnscopedFiles, &paths); err != nil {
		return err
	}
	s.UnscopedFiles = make([]UnscopedFile, len(paths))
	for i, p := range paths {
		s.UnscopedFiles[i] = UnscopedFile{Path: p, ReasonConcerning: "unclassified (string fallback)"}
	}
	return nil
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
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Notes     string `json:"notes"`
}

type PatternConsistency struct {
	FollowsConventions bool     `json:"follows_conventions"`
	Issues             []string `json:"issues"`
}
