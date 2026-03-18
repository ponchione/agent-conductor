package main

import (
	"encoding/json"
	"fmt"

	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
)

type planRequirement struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

type planWorkOrder struct {
	SchemaVersion      int                               `json:"schema_version"`
	Title              string                            `json:"title"`
	Type               string                            `json:"type"`
	TargetModule       string                            `json:"target_module"`
	ReferenceModule    string                            `json:"reference_module"`
	KnownFiles         []string                          `json:"known_files"`
	Requirements       []models.WorkOrderRequirement     `json:"requirements"`
	AcceptanceCriteria []models.TypedAcceptanceCriterion `json:"acceptance_criteria"`
	Constraints        []string                          `json:"constraints"`
	DependsOn          []string                          `json:"depends_on,omitempty"`
	WhyNow             string                            `json:"why_now,omitempty"`
	Size               string                            `json:"size,omitempty"`
	AuditSource        string                            `json:"-"`
}

func (pw planWorkOrder) toWorkOrder() (models.WorkOrder, error) {
	if pw.SchemaVersion != models.WorkOrderSchemaVersion {
		return models.WorkOrder{}, fmt.Errorf("schema_version must be %d", models.WorkOrderSchemaVersion)
	}
	wo := models.WorkOrder{
		SchemaVersion:           pw.SchemaVersion,
		Title:                   pw.Title,
		Type:                    pw.Type,
		TargetModule:            pw.TargetModule,
		ReferenceModule:         pw.ReferenceModule,
		KnownFiles:              pw.KnownFiles,
		Requirements:            pw.Requirements,
		TypedAcceptanceCriteria: pw.AcceptanceCriteria,
		Constraints:             pw.Constraints,
		AuditSource:             pw.AuditSource,
	}
	if err := wo.Validate(); err != nil {
		return models.WorkOrder{}, err
	}
	return wo, nil
}

type planDocument struct {
	Requirements     []planRequirement `json:"requirements,omitempty"`
	NonGoals         []string          `json:"non_goals,omitempty"`
	ExistingSystem   []string          `json:"existing_system,omitempty"`
	PlanningWarnings []string          `json:"planning_warnings,omitempty"`
	WorkOrders       []planWorkOrder   `json:"work_orders"`
}

func parsePlanResponse(raw string) (*planDocument, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp planDocument
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	if len(resp.WorkOrders) == 0 {
		return nil, fmt.Errorf("response contained no work orders")
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (pd *planDocument) Validate() error {
	if len(pd.WorkOrders) == 0 {
		return fmt.Errorf("response contained no work orders")
	}

	requirementIDs := make(map[string]bool, len(pd.Requirements))
	requirementsByID := make(map[string]planRequirement, len(pd.Requirements))
	for _, req := range pd.Requirements {
		if req.ID == "" {
			return fmt.Errorf("requirement missing id")
		}
		if requirementIDs[req.ID] {
			return fmt.Errorf("duplicate requirement id %q", req.ID)
		}
		requirementIDs[req.ID] = true
		requirementsByID[req.ID] = req
	}

	for i, pw := range pd.WorkOrders {
		wo, err := pw.toWorkOrder()
		if err != nil {
			return fmt.Errorf("work order %d (%q): %w", i+1, pw.Title, err)
		}
		if len(requirementIDs) == 0 {
			continue
		}
		for _, req := range wo.Requirements {
			planReq, ok := requirementsByID[req.ID]
			if !ok {
				return fmt.Errorf("work order %d (%q) references unknown requirement %q", i+1, pw.Title, req.ID)
			}
			if planReq.Text != "" && req.Text != "" && planReq.Text != req.Text {
				return fmt.Errorf("work order %d (%q) requirement %q text does not match top-level requirement", i+1, pw.Title, req.ID)
			}
		}
	}

	return nil
}

func (pd *planDocument) ToWorkOrders() ([]models.WorkOrder, error) {
	workOrders := make([]models.WorkOrder, len(pd.WorkOrders))
	for i, pw := range pd.WorkOrders {
		wo, err := pw.toWorkOrder()
		if err != nil {
			return nil, fmt.Errorf("work order %d (%q): %w", i+1, pw.Title, err)
		}
		workOrders[i] = wo
	}
	return workOrders, nil
}

func (pd *planDocument) MarshalIndented() ([]byte, error) {
	return json.MarshalIndent(pd, "", "  ")
}

type auditPlanWorkOrder struct {
	planWorkOrder
	AuditAction string `json:"audit_action"`
}

type auditSummary struct {
	Added     int      `json:"added"`
	Modified  int      `json:"modified"`
	Unchanged int      `json:"unchanged"`
	Changes   []string `json:"changes"`
}

type auditResponse struct {
	Requirements     []planRequirement    `json:"requirements,omitempty"`
	NonGoals         []string             `json:"non_goals,omitempty"`
	ExistingSystem   []string             `json:"existing_system,omitempty"`
	PlanningWarnings []string             `json:"planning_warnings,omitempty"`
	WorkOrders       []auditPlanWorkOrder `json:"work_orders"`
	AuditSummary     auditSummary         `json:"audit_summary"`
}

func (ar *auditResponse) toPlanDocument() *planDocument {
	workOrders := make([]planWorkOrder, len(ar.WorkOrders))
	for i, aw := range ar.WorkOrders {
		workOrders[i] = aw.planWorkOrder
		if aw.AuditAction == "added" || aw.AuditAction == "modified" {
			workOrders[i].AuditSource = aw.AuditAction
		}
	}
	return &planDocument{
		Requirements:     ar.Requirements,
		NonGoals:         ar.NonGoals,
		ExistingSystem:   ar.ExistingSystem,
		PlanningWarnings: ar.PlanningWarnings,
		WorkOrders:       workOrders,
	}
}
