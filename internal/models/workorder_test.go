package models

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkOrderValidateRequiresAcceptanceCriteria(t *testing.T) {
	wo := &WorkOrder{
		Title:        "Test",
		Type:         "bug_fix",
		TargetModule: "cmd/conductor",
	}

	if err := wo.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want acceptance criteria error")
	}
}

func TestWorkOrderValidateRejectsBlankEntries(t *testing.T) {
	wo := &WorkOrder{
		Title:              "Test",
		Type:               "bug_fix",
		TargetModule:       "cmd/conductor",
		AcceptanceCriteria: []string{" "},
		KnownFiles:         []string{"cmd/conductor/plan.go", "  "},
	}

	if err := wo.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want blank entry validation error")
	}
}

func TestWorkOrderYAMLUnmarshalVersion1DefaultsSchema(t *testing.T) {
	data := []byte(`
title: Test
type: bug_fix
target_module: cmd/conductor
known_files:
  - cmd/conductor/plan.go
acceptance_criteria:
  - make test passes
constraints:
  - Keep behavior stable
`)

	var wo WorkOrder
	if err := yaml.Unmarshal(data, &wo); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if wo.EffectiveSchemaVersion() != 1 {
		t.Fatalf("EffectiveSchemaVersion() = %d, want 1", wo.EffectiveSchemaVersion())
	}
	if len(wo.TypedAcceptanceCriteria) != 0 {
		t.Fatalf("TypedAcceptanceCriteria = %+v, want empty", wo.TypedAcceptanceCriteria)
	}
	if len(wo.AcceptanceCriteria) != 1 || wo.AcceptanceCriteria[0] != "make test passes" {
		t.Fatalf("AcceptanceCriteria = %+v, want legacy scalar criteria", wo.AcceptanceCriteria)
	}
}

func TestWorkOrderYAMLUnmarshalVersion2TypedCriteria(t *testing.T) {
	data := []byte(`
schema_version: 2
title: Preserve observability routes
type: refactor
target_module: internal/api
reference_module: internal/http
known_files:
  - internal/api/server.go
requirements:
  - id: REQ-1
    text: Static assets remain reachable
acceptance_criteria:
  - id: AC-1
    description: GET /assets/observability.css returns 200
    requirement_ids: [REQ-1]
    required: true
    verification:
      kind: http_smoke
      route: observability_assets
constraints:
  - Do not change unrelated routes
`)

	var wo WorkOrder
	if err := yaml.Unmarshal(data, &wo); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if wo.EffectiveSchemaVersion() != 2 {
		t.Fatalf("EffectiveSchemaVersion() = %d, want 2", wo.EffectiveSchemaVersion())
	}
	if len(wo.Requirements) != 1 || wo.Requirements[0].ID != "REQ-1" {
		t.Fatalf("Requirements = %+v, want REQ-1", wo.Requirements)
	}
	if len(wo.TypedAcceptanceCriteria) != 1 {
		t.Fatalf("TypedAcceptanceCriteria = %+v, want 1 entry", wo.TypedAcceptanceCriteria)
	}
	if wo.TypedAcceptanceCriteria[0].Verification.Kind != "http_smoke" {
		t.Fatalf("Verification.Kind = %q, want http_smoke", wo.TypedAcceptanceCriteria[0].Verification.Kind)
	}
	if len(wo.AcceptanceCriteria) != 1 || wo.AcceptanceCriteria[0] != "GET /assets/observability.css returns 200" {
		t.Fatalf("AcceptanceCriteria = %+v, want derived descriptions", wo.AcceptanceCriteria)
	}
	if err := wo.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestWorkOrderValidateVersion2RejectsMissingRequiredFlag(t *testing.T) {
	wo := &WorkOrder{
		SchemaVersion: 2,
		Title:         "Preserve observability routes",
		Type:          "refactor",
		TargetModule:  "internal/api",
		Requirements: []WorkOrderRequirement{
			{ID: "REQ-1", Text: "Static assets remain reachable"},
		},
		TypedAcceptanceCriteria: []TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "GET /assets/observability.css returns 200",
				RequirementIDs: []string{"REQ-1"},
				Verification:   AcceptanceVerification{Kind: "http_smoke", Route: "observability_assets"},
			},
		},
	}

	err := wo.Validate()
	if err == nil || !strings.Contains(err.Error(), ".required must be set") {
		t.Fatalf("Validate() error = %v, want missing required flag error", err)
	}
}

func TestWorkOrderValidateVersion2RejectsMissingRequirementCoverage(t *testing.T) {
	required := true
	wo := &WorkOrder{
		SchemaVersion: 2,
		Title:         "Preserve observability routes",
		Type:          "refactor",
		TargetModule:  "internal/api",
		Requirements: []WorkOrderRequirement{
			{ID: "REQ-1", Text: "Static assets remain reachable"},
			{ID: "REQ-2", Text: "Tests remain green"},
		},
		TypedAcceptanceCriteria: []TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "GET /assets/observability.css returns 200",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification:   AcceptanceVerification{Kind: "http_smoke", Route: "observability_assets"},
			},
		},
	}

	err := wo.Validate()
	if err == nil || !strings.Contains(err.Error(), `requirement "REQ-2" is not covered`) {
		t.Fatalf("Validate() error = %v, want missing requirement coverage error", err)
	}
}

func TestWorkOrderValidateVersion2RejectsPrecheckWithoutCheck(t *testing.T) {
	required := true
	wo := &WorkOrder{
		SchemaVersion: 2,
		Title:         "Run typed prechecks",
		Type:          "refactor",
		TargetModule:  "internal/worker",
		Requirements: []WorkOrderRequirement{
			{ID: "REQ-1", Text: "Run the configured test command"},
		},
		TypedAcceptanceCriteria: []TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "Configured test precheck passes",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification:   AcceptanceVerification{Kind: "precheck"},
			},
		},
	}

	err := wo.Validate()
	if err == nil || !strings.Contains(err.Error(), ".verification.check must not be empty for precheck") {
		t.Fatalf("Validate() error = %v, want missing precheck check error", err)
	}
}

func TestWorkOrderJSONMarshalVersion2UsesCanonicalAcceptanceCriteriaShape(t *testing.T) {
	required := true
	wo := WorkOrder{
		SchemaVersion: 2,
		Title:         "Preserve observability routes",
		Type:          "refactor",
		TargetModule:  "internal/api",
		Requirements: []WorkOrderRequirement{
			{ID: "REQ-1", Text: "Static assets remain reachable"},
		},
		TypedAcceptanceCriteria: []TypedAcceptanceCriterion{
			{
				ID:             "AC-1",
				Description:    "GET /assets/observability.css returns 200",
				RequirementIDs: []string{"REQ-1"},
				Required:       &required,
				Verification:   AcceptanceVerification{Kind: "http_smoke", Route: "observability_assets"},
			},
		},
	}

	data, err := json.Marshal(wo)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"schema_version":2`) {
		t.Fatalf("marshaled JSON missing schema_version: %s", content)
	}
	if !strings.Contains(content, `"acceptance_criteria":[{"id":"AC-1"`) {
		t.Fatalf("marshaled JSON missing typed acceptance criteria payload: %s", content)
	}
}
