package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ValidationEvidence struct {
	Phase       string                    `json:"phase"`
	Commands    []ValidationEvidenceEntry `json:"commands,omitempty"`
	SmokeChecks []ValidationEvidenceEntry `json:"smoke_checks,omitempty"`
}

type ValidationEvidenceEntry struct {
	Name    string   `json:"name"`
	Argv    []string `json:"argv"`
	Workdir string   `json:"workdir,omitempty"`
	Result  string   `json:"result,omitempty"`
	Notes   string   `json:"notes,omitempty"`
}

func WriteValidationEvidence(path string, evidence *ValidationEvidence) error {
	if evidence == nil {
		evidence = &ValidationEvidence{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create evidence dir: %w", err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write evidence: %w", err)
	}
	return nil
}

func LoadValidationEvidence(path string) (*ValidationEvidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var evidence ValidationEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return nil, fmt.Errorf("unmarshal evidence: %w", err)
	}
	return &evidence, nil
}
