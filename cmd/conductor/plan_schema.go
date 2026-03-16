package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
)

type planRequirement struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

type planWorkOrder struct {
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	TargetModule       string   `json:"target_module"`
	ReferenceModule    string   `json:"reference_module"`
	KnownFiles         []string `json:"known_files"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
	Covers             []string `json:"covers,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	WhyNow             string   `json:"why_now,omitempty"`
	Size               string   `json:"size,omitempty"`
	AuditSource        string   `json:"-"`
}

func (pw planWorkOrder) toWorkOrder() (models.WorkOrder, error) {
	wo := models.WorkOrder{
		Title:              pw.Title,
		Type:               pw.Type,
		TargetModule:       pw.TargetModule,
		ReferenceModule:    pw.ReferenceModule,
		KnownFiles:         pw.KnownFiles,
		AcceptanceCriteria: pw.AcceptanceCriteria,
		Constraints:        pw.Constraints,
		AuditSource:        pw.AuditSource,
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
	for _, req := range pd.Requirements {
		if req.ID == "" {
			return fmt.Errorf("requirement missing id")
		}
		if requirementIDs[req.ID] {
			return fmt.Errorf("duplicate requirement id %q", req.ID)
		}
		requirementIDs[req.ID] = true
	}

	for i, pw := range pd.WorkOrders {
		wo, err := pw.toWorkOrder()
		if err != nil {
			return fmt.Errorf("work order %d (%q): %w", i+1, pw.Title, err)
		}
		_ = wo

		for _, reqID := range pw.Covers {
			if len(requirementIDs) == 0 {
				break
			}
			if !requirementIDs[reqID] {
				return fmt.Errorf("work order %d (%q) covers unknown requirement %q", i+1, pw.Title, reqID)
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

func (pd *planDocument) InheritMissingMetadata(from *planDocument) {
	if from == nil {
		return
	}
	if len(pd.Requirements) == 0 {
		pd.Requirements = slices.Clone(from.Requirements)
	}
	if len(pd.NonGoals) == 0 {
		pd.NonGoals = slices.Clone(from.NonGoals)
	}
	if len(pd.ExistingSystem) == 0 {
		pd.ExistingSystem = slices.Clone(from.ExistingSystem)
	}
	if len(pd.PlanningWarnings) == 0 {
		pd.PlanningWarnings = slices.Clone(from.PlanningWarnings)
	}

	byKey := make(map[string]planWorkOrder, len(from.WorkOrders))
	for _, wo := range from.WorkOrders {
		byKey[planWorkOrderKey(wo)] = wo
	}

	for i := range pd.WorkOrders {
		match, ok := byKey[planWorkOrderKey(pd.WorkOrders[i])]
		if !ok {
			continue
		}
		if len(pd.WorkOrders[i].Covers) == 0 {
			pd.WorkOrders[i].Covers = slices.Clone(match.Covers)
		}
		if len(pd.WorkOrders[i].DependsOn) == 0 {
			pd.WorkOrders[i].DependsOn = slices.Clone(match.DependsOn)
		}
		if pd.WorkOrders[i].WhyNow == "" {
			pd.WorkOrders[i].WhyNow = match.WhyNow
		}
		if pd.WorkOrders[i].Size == "" {
			pd.WorkOrders[i].Size = match.Size
		}
	}
}

func planWorkOrderKey(wo planWorkOrder) string {
	return strings.ToLower(strings.Join([]string{wo.Title, wo.Type, wo.TargetModule}, "::"))
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
