package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type WorkOrderRequirement struct {
	ID     string `yaml:"id" json:"id"`
	Text   string `yaml:"text" json:"text"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type AcceptanceVerification struct {
	Kind              string   `yaml:"kind" json:"kind"`
	Check             string   `yaml:"check,omitempty" json:"check,omitempty"`
	Focus             []string `yaml:"focus,omitempty" json:"focus,omitempty"`
	Subject           string   `yaml:"subject,omitempty" json:"subject,omitempty"`
	Expectation       string   `yaml:"expectation,omitempty" json:"expectation,omitempty"`
	Route             string   `yaml:"route,omitempty" json:"route,omitempty"`
	Method            string   `yaml:"method,omitempty" json:"method,omitempty"`
	Path              string   `yaml:"path,omitempty" json:"path,omitempty"`
	ExpectStatus      int      `yaml:"expect_status,omitempty" json:"expect_status,omitempty"`
	ExpectContentType string   `yaml:"expect_content_type,omitempty" json:"expect_content_type,omitempty"`
}

type TypedAcceptanceCriterion struct {
	ID             string                 `yaml:"id" json:"id"`
	Description    string                 `yaml:"description" json:"description"`
	RequirementIDs []string               `yaml:"requirement_ids" json:"requirement_ids"`
	Required       *bool                  `yaml:"required" json:"required"`
	Verification   AcceptanceVerification `yaml:"verification" json:"verification"`
	Notes          string                 `yaml:"notes,omitempty" json:"notes,omitempty"`
	Criticality    string                 `yaml:"criticality,omitempty" json:"criticality,omitempty"`
}

type WorkOrder struct {
	SchemaVersion           int                        `yaml:"schema_version,omitempty" json:"schema_version,omitempty"`
	Title                   string                     `yaml:"title" json:"title"`
	TargetModule            string                     `yaml:"target_module" json:"target_module"`
	ReferenceModule         string                     `yaml:"reference_module" json:"reference_module"`
	Type                    string                     `yaml:"type" json:"type"`
	KnownFiles              []string                   `yaml:"known_files" json:"known_files"`
	AcceptanceCriteria      []string                   `yaml:"-" json:"acceptance_criteria"`
	TypedAcceptanceCriteria []TypedAcceptanceCriterion `yaml:"-" json:"typed_acceptance_criteria,omitempty"`
	Requirements            []WorkOrderRequirement     `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	Constraints             []string                   `yaml:"constraints" json:"constraints"`
	AuditSource             string                     `yaml:"audit_source,omitempty" json:"audit_source,omitempty"`
}

var validWorkOrderTypes = map[string]bool{
	"new_feature":   true,
	"bug_fix":       true,
	"refactor":      true,
	"schema_change": true,
	"docs":          true,
	"bootstrap":     true,
}

func (wo *WorkOrder) EffectiveSchemaVersion() int {
	if wo.SchemaVersion <= 0 {
		return 2
	}
	return wo.SchemaVersion
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
	for i, knownFile := range wo.KnownFiles {
		if strings.TrimSpace(knownFile) == "" {
			return fmt.Errorf("work order known_files[%d] must not be empty", i)
		}
	}

	if wo.EffectiveSchemaVersion() != 2 {
		return fmt.Errorf("schema_version must be 2")
	}
	if len(wo.Requirements) == 0 {
		return fmt.Errorf("work order requirements must not be empty")
	}
	if len(wo.TypedAcceptanceCriteria) == 0 {
		return fmt.Errorf("work order typed acceptance criteria must not be empty")
	}

	requirements := make(map[string]bool, len(wo.Requirements))
	coverage := make(map[string]int, len(wo.Requirements))
	for i, req := range wo.Requirements {
		if strings.TrimSpace(req.ID) == "" {
			return fmt.Errorf("work order requirements[%d].id must not be empty", i)
		}
		if requirements[req.ID] {
			return fmt.Errorf("duplicate requirement id %q", req.ID)
		}
		requirements[req.ID] = true
	}

	criterionIDs := make(map[string]bool, len(wo.TypedAcceptanceCriteria))
	wo.AcceptanceCriteria = wo.AcceptanceCriteria[:0]
	for i, criterion := range wo.TypedAcceptanceCriteria {
		if strings.TrimSpace(criterion.ID) == "" {
			return fmt.Errorf("typed_acceptance_criteria[%d].id must not be empty", i)
		}
		if criterionIDs[criterion.ID] {
			return fmt.Errorf("duplicate typed acceptance criterion id %q", criterion.ID)
		}
		criterionIDs[criterion.ID] = true

		if strings.TrimSpace(criterion.Description) == "" {
			return fmt.Errorf("typed_acceptance_criteria[%d].description must not be empty", i)
		}
		if criterion.Required == nil {
			return fmt.Errorf("typed_acceptance_criteria[%d].required must be set", i)
		}
		kind := strings.TrimSpace(criterion.Verification.Kind)
		if kind == "" {
			return fmt.Errorf("typed_acceptance_criteria[%d].verification.kind must not be empty", i)
		}
		if len(criterion.RequirementIDs) == 0 {
			return fmt.Errorf("typed_acceptance_criteria[%d].requirement_ids must not be empty", i)
		}
		switch kind {
		case "precheck":
			if strings.TrimSpace(criterion.Verification.Check) == "" {
				return fmt.Errorf("typed_acceptance_criteria[%d].verification.check must not be empty for precheck", i)
			}
		}
		for _, reqID := range criterion.RequirementIDs {
			if !requirements[reqID] {
				return fmt.Errorf("typed_acceptance_criteria[%d] references unknown requirement id %q", i, reqID)
			}
			coverage[reqID]++
		}
		wo.AcceptanceCriteria = append(wo.AcceptanceCriteria, criterion.Description)
	}

	for _, req := range wo.Requirements {
		if coverage[req.ID] == 0 {
			return fmt.Errorf("requirement %q is not covered by any typed acceptance criterion", req.ID)
		}
	}
	return nil
}

func (wo *WorkOrder) UnmarshalYAML(node *yaml.Node) error {
	type commonFields struct {
		SchemaVersion   int                    `yaml:"schema_version"`
		Title           string                 `yaml:"title"`
		TargetModule    string                 `yaml:"target_module"`
		ReferenceModule string                 `yaml:"reference_module"`
		Type            string                 `yaml:"type"`
		KnownFiles      []string               `yaml:"known_files"`
		Requirements    []WorkOrderRequirement `yaml:"requirements"`
		Constraints     []string               `yaml:"constraints"`
		AuditSource     string                 `yaml:"audit_source"`
	}
	var common commonFields
	if err := node.Decode(&common); err != nil {
		return err
	}

	criteriaNode := findYAMLField(node, "acceptance_criteria")
	typedCriteria, err := decodeAcceptanceCriteriaYAML(criteriaNode)
	if err != nil {
		return err
	}
	if common.SchemaVersion != 2 {
		return fmt.Errorf("schema_version must be 2")
	}

	wo.SchemaVersion = common.SchemaVersion
	wo.Title = common.Title
	wo.TargetModule = common.TargetModule
	wo.ReferenceModule = common.ReferenceModule
	wo.Type = common.Type
	wo.KnownFiles = common.KnownFiles
	wo.Requirements = common.Requirements
	wo.Constraints = common.Constraints
	wo.AuditSource = common.AuditSource
	wo.TypedAcceptanceCriteria = typedCriteria
	wo.AcceptanceCriteria = deriveAcceptanceCriteria(typedCriteria)
	return nil
}

func (wo WorkOrder) MarshalYAML() (any, error) {
	type versionTwo struct {
		SchemaVersion      int                        `yaml:"schema_version"`
		Title              string                     `yaml:"title"`
		TargetModule       string                     `yaml:"target_module"`
		ReferenceModule    string                     `yaml:"reference_module,omitempty"`
		Type               string                     `yaml:"type"`
		KnownFiles         []string                   `yaml:"known_files,omitempty"`
		Requirements       []WorkOrderRequirement     `yaml:"requirements,omitempty"`
		AcceptanceCriteria []TypedAcceptanceCriterion `yaml:"acceptance_criteria,omitempty"`
		Constraints        []string                   `yaml:"constraints,omitempty"`
		AuditSource        string                     `yaml:"audit_source,omitempty"`
	}
	return versionTwo{
		SchemaVersion:      2,
		Title:              wo.Title,
		TargetModule:       wo.TargetModule,
		ReferenceModule:    wo.ReferenceModule,
		Type:               wo.Type,
		KnownFiles:         wo.KnownFiles,
		Requirements:       wo.Requirements,
		AcceptanceCriteria: wo.TypedAcceptanceCriteria,
		Constraints:        wo.Constraints,
		AuditSource:        wo.AuditSource,
	}, nil
}

func (wo *WorkOrder) UnmarshalJSON(data []byte) error {
	type commonFields struct {
		SchemaVersion   int                    `json:"schema_version"`
		Title           string                 `json:"title"`
		TargetModule    string                 `json:"target_module"`
		ReferenceModule string                 `json:"reference_module"`
		Type            string                 `json:"type"`
		KnownFiles      []string               `json:"known_files"`
		Requirements    []WorkOrderRequirement `json:"requirements"`
		Constraints     []string               `json:"constraints"`
		AuditSource     string                 `json:"audit_source"`
	}
	var raw struct {
		commonFields
		AcceptanceCriteria json.RawMessage `json:"acceptance_criteria"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	typedCriteria, err := decodeAcceptanceCriteriaJSON(raw.AcceptanceCriteria)
	if err != nil {
		return err
	}
	if raw.SchemaVersion != 2 {
		return fmt.Errorf("schema_version must be 2")
	}

	wo.SchemaVersion = raw.SchemaVersion
	wo.Title = raw.Title
	wo.TargetModule = raw.TargetModule
	wo.ReferenceModule = raw.ReferenceModule
	wo.Type = raw.Type
	wo.KnownFiles = raw.KnownFiles
	wo.Requirements = raw.Requirements
	wo.Constraints = raw.Constraints
	wo.AuditSource = raw.AuditSource
	wo.TypedAcceptanceCriteria = typedCriteria
	wo.AcceptanceCriteria = deriveAcceptanceCriteria(typedCriteria)
	return nil
}

func (wo WorkOrder) MarshalJSON() ([]byte, error) {
	type versionTwo struct {
		SchemaVersion      int                        `json:"schema_version"`
		Title              string                     `json:"title"`
		TargetModule       string                     `json:"target_module"`
		ReferenceModule    string                     `json:"reference_module,omitempty"`
		Type               string                     `json:"type"`
		KnownFiles         []string                   `json:"known_files,omitempty"`
		Requirements       []WorkOrderRequirement     `json:"requirements,omitempty"`
		AcceptanceCriteria []TypedAcceptanceCriterion `json:"acceptance_criteria,omitempty"`
		Constraints        []string                   `json:"constraints,omitempty"`
		AuditSource        string                     `json:"audit_source,omitempty"`
	}
	return json.Marshal(versionTwo{
		SchemaVersion:      2,
		Title:              wo.Title,
		TargetModule:       wo.TargetModule,
		ReferenceModule:    wo.ReferenceModule,
		Type:               wo.Type,
		KnownFiles:         wo.KnownFiles,
		Requirements:       wo.Requirements,
		AcceptanceCriteria: wo.TypedAcceptanceCriteria,
		Constraints:        wo.Constraints,
		AuditSource:        wo.AuditSource,
	})
}

func findYAMLField(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func decodeAcceptanceCriteriaYAML(node *yaml.Node) ([]TypedAcceptanceCriterion, error) {
	if node == nil || node.Kind == 0 || node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("acceptance_criteria must be a sequence")
	}
	if len(node.Content) == 0 {
		return []TypedAcceptanceCriterion{}, nil
	}
	if node.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("acceptance_criteria entries must be mappings")
	}
	var criteria []TypedAcceptanceCriterion
	if err := node.Decode(&criteria); err != nil {
		return nil, err
	}
	return criteria, nil
}

func decodeAcceptanceCriteriaJSON(data json.RawMessage) ([]TypedAcceptanceCriterion, error) {
	if len(bytes.TrimSpace(data)) == 0 || string(bytes.TrimSpace(data)) == "null" {
		return nil, nil
	}

	var typed []TypedAcceptanceCriterion
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, fmt.Errorf("invalid acceptance_criteria payload: %w", err)
	}
	return typed, nil
}

func deriveAcceptanceCriteria(criteria []TypedAcceptanceCriterion) []string {
	descriptions := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		descriptions = append(descriptions, criterion.Description)
	}
	return descriptions
}
