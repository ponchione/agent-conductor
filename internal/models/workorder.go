package models

import "fmt"

type WorkOrder struct {
	Title              string   `yaml:"title"`
	TargetModule       string   `yaml:"target_module"`
	ReferenceModule    string   `yaml:"reference_module"`
	Type               string   `yaml:"type"`
	KnownFiles         []string `yaml:"known_files"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
	Constraints        []string `yaml:"constraints"`
}

var validWorkOrderTypes = map[string]bool{
	"new_feature":   true,
	"bug_fix":       true,
	"refactor":      true,
	"schema_change": true,
	"docs":          true,
	"bootstrap":     true,
}

func (wo *WorkOrder) Validate() error {
	if wo.Title == "" {
		return fmt.Errorf("work order title is required")
	}
	if wo.TargetModule == "" {
		return fmt.Errorf("work order target_module is required")
	}
	if !validWorkOrderTypes[wo.Type] {
		return fmt.Errorf("invalid work order type %q: must be one of new_feature, bug_fix, refactor, schema_change, docs, bootstrap", wo.Type)
	}
	return nil
}
