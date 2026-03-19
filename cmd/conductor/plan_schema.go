package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ponchione/agent-conductor/internal/llm"
	"github.com/ponchione/agent-conductor/internal/models"
	"gopkg.in/yaml.v3"
)

const planManifestVersion = 1

type planRequirement struct {
	ID     string `yaml:"id" json:"id"`
	Text   string `yaml:"text" json:"text"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type rawEpicPlanResponse struct {
	Requirements     []planRequirement `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	NonGoals         []string          `yaml:"non_goals,omitempty" json:"non_goals,omitempty"`
	ExistingSystem   []string          `yaml:"existing_system,omitempty" json:"existing_system,omitempty"`
	PlanningWarnings []string          `yaml:"planning_warnings,omitempty" json:"planning_warnings,omitempty"`
	Epics            []rawPlanEpic     `yaml:"epics" json:"epics"`
}

type rawPlanEpic struct {
	EpicRef        string   `yaml:"epic_ref" json:"epic_ref"`
	Title          string   `yaml:"title" json:"title"`
	Description    string   `yaml:"description" json:"description"`
	Covers         []string `yaml:"covers,omitempty" json:"covers,omitempty"`
	DependsOnEpics []string `yaml:"depends_on_epics,omitempty" json:"depends_on_epics,omitempty"`
}

type rawTaskPlanResponse struct {
	Tasks []rawPlanTask `yaml:"tasks" json:"tasks"`
}

type rawPlanTask struct {
	TaskRef            string                            `yaml:"task_ref" json:"task_ref"`
	SchemaVersion      int                               `yaml:"schema_version" json:"schema_version"`
	Title              string                            `yaml:"title" json:"title"`
	Type               string                            `yaml:"type" json:"type"`
	TargetModule       string                            `yaml:"target_module" json:"target_module"`
	ReferenceModule    string                            `yaml:"reference_module" json:"reference_module"`
	KnownFiles         []string                          `yaml:"known_files" json:"known_files"`
	Requirements       []models.WorkOrderRequirement     `yaml:"requirements" json:"requirements"`
	AcceptanceCriteria []models.TypedAcceptanceCriterion `yaml:"acceptance_criteria" json:"acceptance_criteria"`
	Constraints        []string                          `yaml:"constraints" json:"constraints"`
	DependsOn          []string                          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Size               string                            `yaml:"size,omitempty" json:"size,omitempty"`
}

type planDocument struct {
	Version          int               `yaml:"version" json:"version"`
	SpecFile         string            `yaml:"spec_file,omitempty" json:"spec_file,omitempty"`
	SpecFingerprint  string            `yaml:"spec_fingerprint,omitempty" json:"spec_fingerprint,omitempty"`
	CreatedAt        string            `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	SessionID        string            `yaml:"session_id,omitempty" json:"session_id,omitempty"`
	GenerationModel  string            `yaml:"generation_model,omitempty" json:"generation_model,omitempty"`
	Requirements     []planRequirement `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	NonGoals         []string          `yaml:"non_goals,omitempty" json:"non_goals,omitempty"`
	ExistingSystem   []string          `yaml:"existing_system,omitempty" json:"existing_system,omitempty"`
	PlanningWarnings []string          `yaml:"planning_warnings,omitempty" json:"planning_warnings,omitempty"`
	Epics            []planEpic        `yaml:"epics" json:"epics"`
}

type planEpic struct {
	ID             string     `yaml:"id" json:"id"`
	EpicRef        string     `yaml:"epic_ref" json:"epic_ref"`
	Title          string     `yaml:"title" json:"title"`
	Description    string     `yaml:"description" json:"description"`
	Covers         []string   `yaml:"covers,omitempty" json:"covers,omitempty"`
	DependsOnEpics []string   `yaml:"depends_on_epics,omitempty" json:"depends_on_epics,omitempty"`
	Tasks          []planTask `yaml:"tasks" json:"tasks"`
}

type planTask struct {
	ID                 string                            `yaml:"id" json:"id"`
	TaskRef            string                            `yaml:"task_ref" json:"task_ref"`
	SchemaVersion      int                               `yaml:"schema_version" json:"schema_version"`
	EpicID             string                            `yaml:"epic_id" json:"epic_id"`
	Title              string                            `yaml:"title" json:"title"`
	Type               string                            `yaml:"type" json:"type"`
	TargetModule       string                            `yaml:"target_module" json:"target_module"`
	ReferenceModule    string                            `yaml:"reference_module" json:"reference_module"`
	KnownFiles         []string                          `yaml:"known_files" json:"known_files"`
	Requirements       []models.WorkOrderRequirement     `yaml:"requirements" json:"requirements"`
	AcceptanceCriteria []models.TypedAcceptanceCriterion `yaml:"acceptance_criteria" json:"acceptance_criteria"`
	Constraints        []string                          `yaml:"constraints" json:"constraints"`
	DependsOn          []string                          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Size               string                            `yaml:"size,omitempty" json:"size,omitempty"`
	AuditSource        string                            `yaml:"audit_source,omitempty" json:"audit_source,omitempty"`
}

type planManifestMetadata struct {
	SpecFile        string
	SpecFingerprint string
	CreatedAt       string
	SessionID       string
	GenerationModel string
}

func canonicalEpicID(index int) string {
	return fmt.Sprintf("epic-%03d", index)
}

func canonicalTaskID(index int) string {
	return fmt.Sprintf("task-%03d", index)
}

func buildEpicIDMap(epics []planEpic) (map[string]string, error) {
	idsByRef := make(map[string]string, len(epics))
	for _, epic := range epics {
		epicRef := strings.TrimSpace(epic.EpicRef)
		epicID := strings.TrimSpace(epic.ID)
		if epicRef == "" {
			return nil, fmt.Errorf("epic_ref must not be empty")
		}
		if epicID == "" {
			return nil, fmt.Errorf("epic %q missing canonical id", epicRef)
		}
		if _, ok := idsByRef[epicRef]; ok {
			return nil, fmt.Errorf("duplicate epic_ref %q", epicRef)
		}
		idsByRef[epicRef] = epicID
	}
	return idsByRef, nil
}

func buildTaskIDMap(tasks []planTask) (map[string]string, error) {
	idsByRef := make(map[string]string, len(tasks))
	for _, task := range tasks {
		taskRef := strings.TrimSpace(task.TaskRef)
		taskID := strings.TrimSpace(task.ID)
		if taskRef == "" {
			return nil, fmt.Errorf("task_ref must not be empty")
		}
		if taskID == "" {
			return nil, fmt.Errorf("task %q missing canonical id", taskRef)
		}
		if _, ok := idsByRef[taskRef]; ok {
			return nil, fmt.Errorf("duplicate task_ref %q", taskRef)
		}
		idsByRef[taskRef] = taskID
	}
	return idsByRef, nil
}

func remapDependencyRefs(refs []string, idsByRef map[string]string, label string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}

	remapped := make([]string, len(refs))
	for i, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			return nil, fmt.Errorf("%s[%d] must not be empty", label, i)
		}
		id, ok := idsByRef[trimmed]
		if !ok {
			return nil, fmt.Errorf("%s references unknown ref %q", label, trimmed)
		}
		remapped[i] = id
	}
	return remapped, nil
}

func (pt planTask) toWorkOrder() (models.WorkOrder, error) {
	if pt.SchemaVersion != models.WorkOrderSchemaVersion {
		return models.WorkOrder{}, fmt.Errorf("schema_version must be %d", models.WorkOrderSchemaVersion)
	}
	wo := models.WorkOrder{
		SchemaVersion:           pt.SchemaVersion,
		Title:                   pt.Title,
		Type:                    pt.Type,
		TargetModule:            pt.TargetModule,
		ReferenceModule:         pt.ReferenceModule,
		KnownFiles:              pt.KnownFiles,
		Requirements:            pt.Requirements,
		TypedAcceptanceCriteria: pt.AcceptanceCriteria,
		Constraints:             pt.Constraints,
		AuditSource:             pt.AuditSource,
	}
	if err := wo.Validate(); err != nil {
		return models.WorkOrder{}, err
	}
	return wo, nil
}

func parseEpicPlanResponse(raw string) (*rawEpicPlanResponse, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp rawEpicPlanResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}
	return &resp, nil
}

func parseTaskPlanResponse(raw string) (*rawTaskPlanResponse, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp rawTaskPlanResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}
	return &resp, nil
}

func parsePlanResponse(raw string) (*planDocument, error) {
	cleaned := llm.CleanLLMResponse(raw)

	var resp planDocument
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	if len(resp.Epics) == 0 {
		return nil, fmt.Errorf("response contained no epics")
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}
	return &resp, nil
}

func parsePlanManifestYAML(data []byte) (*planDocument, error) {
	var doc planDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("YAML unmarshal failed: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

type selectedPlanTask struct {
	Plan *planDocument
	Epic planEpic
	Task planTask
}

func selectPlanTask(doc *planDocument, canonicalTaskID string) (*selectedPlanTask, error) {
	if doc == nil {
		return nil, fmt.Errorf("plan document is required")
	}

	taskID := strings.TrimSpace(canonicalTaskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}

	for _, epic := range doc.Epics {
		for _, task := range epic.Tasks {
			if task.ID == taskID {
				wo, err := task.toWorkOrder()
				if err != nil {
					return nil, fmt.Errorf("task %q: %w", taskID, err)
				}
				task.AcceptanceCriteria = wo.TypedAcceptanceCriteria
				return &selectedPlanTask{
					Plan: doc,
					Epic: epic,
					Task: task,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("plan manifest does not contain task %q", taskID)
}

func (resp *rawEpicPlanResponse) Validate() error {
	if resp == nil {
		return fmt.Errorf("epic response is required")
	}
	if len(resp.Epics) == 0 {
		return fmt.Errorf("response contained no epics")
	}

	requirementIDs := make(map[string]bool, len(resp.Requirements))
	for i, req := range resp.Requirements {
		if strings.TrimSpace(req.ID) == "" {
			return fmt.Errorf("requirements[%d].id must not be empty", i)
		}
		if requirementIDs[req.ID] {
			return fmt.Errorf("duplicate requirement id %q", req.ID)
		}
		requirementIDs[req.ID] = true
	}

	epicRefs := make(map[string]bool, len(resp.Epics))
	for i, epic := range resp.Epics {
		if strings.TrimSpace(epic.EpicRef) == "" {
			return fmt.Errorf("epics[%d].epic_ref must not be empty", i)
		}
		if epicRefs[epic.EpicRef] {
			return fmt.Errorf("duplicate epic_ref %q", epic.EpicRef)
		}
		epicRefs[epic.EpicRef] = true
		if strings.TrimSpace(epic.Title) == "" {
			return fmt.Errorf("epics[%d].title must not be empty", i)
		}
		if strings.TrimSpace(epic.Description) == "" {
			return fmt.Errorf("epics[%d].description must not be empty", i)
		}
		for _, reqID := range epic.Covers {
			if len(requirementIDs) == 0 {
				continue
			}
			if !requirementIDs[reqID] {
				return fmt.Errorf("epic %q covers unknown requirement %q", epic.EpicRef, reqID)
			}
		}
	}

	return nil
}

func (resp *rawTaskPlanResponse) Validate() error {
	if resp == nil {
		return fmt.Errorf("task response is required")
	}
	if len(resp.Tasks) == 0 {
		return fmt.Errorf("response contained no tasks")
	}

	taskRefs := make(map[string]bool, len(resp.Tasks))
	for i, task := range resp.Tasks {
		if strings.TrimSpace(task.TaskRef) == "" {
			return fmt.Errorf("tasks[%d].task_ref must not be empty", i)
		}
		if taskRefs[task.TaskRef] {
			return fmt.Errorf("duplicate task_ref %q", task.TaskRef)
		}
		taskRefs[task.TaskRef] = true

		if _, err := task.toPlanTask("", ""); err != nil {
			return fmt.Errorf("tasks[%d] (%q): %w", i, task.TaskRef, err)
		}
	}

	return nil
}

func (rt rawPlanTask) toPlanTask(id, epicID string) (planTask, error) {
	task := planTask{
		ID:                 id,
		TaskRef:            rt.TaskRef,
		SchemaVersion:      rt.SchemaVersion,
		EpicID:             epicID,
		Title:              rt.Title,
		Type:               rt.Type,
		TargetModule:       rt.TargetModule,
		ReferenceModule:    rt.ReferenceModule,
		KnownFiles:         rt.KnownFiles,
		Requirements:       rt.Requirements,
		AcceptanceCriteria: rt.AcceptanceCriteria,
		Constraints:        rt.Constraints,
		DependsOn:          rt.DependsOn,
		Size:               rt.Size,
	}
	if _, err := task.toWorkOrder(); err != nil {
		return planTask{}, err
	}
	return task, nil
}

func assemblePlanDocument(meta planManifestMetadata, epicResp *rawEpicPlanResponse, taskResponses []*rawTaskPlanResponse) (*planDocument, error) {
	if err := epicResp.Validate(); err != nil {
		return nil, err
	}
	if len(taskResponses) != len(epicResp.Epics) {
		return nil, fmt.Errorf("task response count %d does not match epic count %d", len(taskResponses), len(epicResp.Epics))
	}

	epics := make([]planEpic, len(epicResp.Epics))
	taskIndex := 1
	for epicIndex, rawEpic := range epicResp.Epics {
		taskResp := taskResponses[epicIndex]
		if err := taskResp.Validate(); err != nil {
			return nil, fmt.Errorf("epic %q task response: %w", rawEpic.EpicRef, err)
		}

		epicID := canonicalEpicID(epicIndex + 1)
		tasks := make([]planTask, len(taskResp.Tasks))
		for i, rawTask := range taskResp.Tasks {
			taskID := canonicalTaskID(taskIndex)
			taskIndex++

			task, err := rawTask.toPlanTask(taskID, epicID)
			if err != nil {
				return nil, fmt.Errorf("epic %q task %q: %w", rawEpic.EpicRef, rawTask.TaskRef, err)
			}
			tasks[i] = task
		}

		epics[epicIndex] = planEpic{
			ID:             epicID,
			EpicRef:        rawEpic.EpicRef,
			Title:          rawEpic.Title,
			Description:    rawEpic.Description,
			Covers:         rawEpic.Covers,
			DependsOnEpics: append([]string(nil), rawEpic.DependsOnEpics...),
			Tasks:          tasks,
		}
	}

	epicIDsByRef, err := buildEpicIDMap(epics)
	if err != nil {
		return nil, err
	}
	taskIDsByRef, err := buildTaskIDMap(flattenPlanTasks(epics))
	if err != nil {
		return nil, err
	}

	for epicIndex := range epics {
		remappedEpics, err := remapDependencyRefs(epics[epicIndex].DependsOnEpics, epicIDsByRef, "depends_on_epics")
		if err != nil {
			return nil, fmt.Errorf("epic %q: %w", epics[epicIndex].EpicRef, err)
		}
		epics[epicIndex].DependsOnEpics = remappedEpics

		for taskIndex := range epics[epicIndex].Tasks {
			remappedTasks, err := remapDependencyRefs(epics[epicIndex].Tasks[taskIndex].DependsOn, taskIDsByRef, "depends_on")
			if err != nil {
				return nil, fmt.Errorf("task %q: %w", epics[epicIndex].Tasks[taskIndex].TaskRef, err)
			}
			epics[epicIndex].Tasks[taskIndex].DependsOn = remappedTasks
		}
	}

	doc := &planDocument{
		Version:          planManifestVersion,
		SpecFile:         meta.SpecFile,
		SpecFingerprint:  meta.SpecFingerprint,
		CreatedAt:        meta.CreatedAt,
		SessionID:        meta.SessionID,
		GenerationModel:  meta.GenerationModel,
		Requirements:     epicResp.Requirements,
		NonGoals:         epicResp.NonGoals,
		ExistingSystem:   epicResp.ExistingSystem,
		PlanningWarnings: epicResp.PlanningWarnings,
		Epics:            epics,
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return doc, nil
}

func (pd *planDocument) Validate() error {
	if pd.Version != planManifestVersion {
		return fmt.Errorf("version must be %d", planManifestVersion)
	}
	if len(pd.Epics) == 0 {
		return fmt.Errorf("response contained no epics")
	}

	requirementIDs := make(map[string]bool, len(pd.Requirements))
	requirementsByID := make(map[string]planRequirement, len(pd.Requirements))
	for _, req := range pd.Requirements {
		if strings.TrimSpace(req.ID) == "" {
			return fmt.Errorf("requirement missing id")
		}
		if requirementIDs[req.ID] {
			return fmt.Errorf("duplicate requirement id %q", req.ID)
		}
		requirementIDs[req.ID] = true
		requirementsByID[req.ID] = req
	}

	epicIndexByID := make(map[string]int, len(pd.Epics))
	if _, err := buildEpicIDMap(pd.Epics); err != nil {
		return err
	}

	taskIndexByID := make(map[string]int, pd.TaskCount())
	if _, err := buildTaskIDMap(pd.Tasks()); err != nil {
		return err
	}

	taskPosition := 0
	for epicIndex, epic := range pd.Epics {
		if strings.TrimSpace(epic.ID) == "" {
			return fmt.Errorf("epic %d missing id", epicIndex+1)
		}
		if prior, ok := epicIndexByID[epic.ID]; ok {
			return fmt.Errorf("epic %d (%q) duplicates epic id %q used by epic %d", epicIndex+1, epic.Title, epic.ID, prior+1)
		}
		epicIndexByID[epic.ID] = epicIndex

		if strings.TrimSpace(epic.Title) == "" {
			return fmt.Errorf("epic %d (%q) title must not be empty", epicIndex+1, epic.ID)
		}
		if strings.TrimSpace(epic.Description) == "" {
			return fmt.Errorf("epic %d (%q) description must not be empty", epicIndex+1, epic.Title)
		}
		for _, reqID := range epic.Covers {
			if len(requirementIDs) == 0 {
				continue
			}
			if !requirementIDs[reqID] {
				return fmt.Errorf("epic %d (%q) covers unknown requirement %q", epicIndex+1, epic.Title, reqID)
			}
		}
		if len(epic.Tasks) == 0 {
			return fmt.Errorf("epic %d (%q) contains no tasks", epicIndex+1, epic.Title)
		}

		for taskIndex, task := range epic.Tasks {
			if strings.TrimSpace(task.ID) == "" {
				return fmt.Errorf("task %d in epic %q missing id", taskIndex+1, epic.Title)
			}
			if prior, ok := taskIndexByID[task.ID]; ok {
				return fmt.Errorf("task %d (%q) duplicates task id %q used by task %d", taskPosition+1, task.Title, task.ID, prior+1)
			}
			taskIndexByID[task.ID] = taskPosition
			taskPosition++

			if strings.TrimSpace(task.EpicID) == "" {
				return fmt.Errorf("task %q missing epic_id", task.Title)
			}
			if task.EpicID != epic.ID {
				return fmt.Errorf("task %q epic_id %q does not match parent epic id %q", task.Title, task.EpicID, epic.ID)
			}

			wo, err := task.toWorkOrder()
			if err != nil {
				return fmt.Errorf("task %q: %w", task.Title, err)
			}
			if len(requirementIDs) == 0 {
				continue
			}
			for _, req := range wo.Requirements {
				planReq, ok := requirementsByID[req.ID]
				if !ok {
					return fmt.Errorf("task %q references unknown requirement %q", task.Title, req.ID)
				}
				if planReq.Text != "" && req.Text != "" && planReq.Text != req.Text {
					return fmt.Errorf("task %q requirement %q text does not match top-level requirement", task.Title, req.ID)
				}
			}
		}
	}

	for epicIndex, epic := range pd.Epics {
		for _, dep := range epic.DependsOnEpics {
			depIndex, ok := epicIndexByID[dep]
			if !ok {
				return fmt.Errorf("epic %d (%q) depends_on_epics references unknown epic id %q", epicIndex+1, epic.Title, dep)
			}
			if depIndex >= epicIndex {
				return fmt.Errorf("epic %d (%q) depends_on_epics references epic %q at position %d, which is not earlier in the sequence", epicIndex+1, epic.Title, dep, depIndex+1)
			}
		}
	}

	for taskIndex, task := range pd.Tasks() {
		for _, dep := range task.DependsOn {
			depIndex, ok := taskIndexByID[dep]
			if !ok {
				return fmt.Errorf("task %d (%q) depends_on references unknown task id %q", taskIndex+1, task.Title, dep)
			}
			if depIndex >= taskIndex {
				return fmt.Errorf("task %d (%q) depends_on references task %q at position %d, which is not earlier in the sequence", taskIndex+1, task.Title, dep, depIndex+1)
			}
		}
	}

	return nil
}

func (pd *planDocument) ToWorkOrders() ([]models.WorkOrder, error) {
	tasks := pd.Tasks()
	workOrders := make([]models.WorkOrder, len(tasks))
	for i, task := range tasks {
		wo, err := task.toWorkOrder()
		if err != nil {
			return nil, fmt.Errorf("task %d (%q): %w", i+1, task.Title, err)
		}
		workOrders[i] = wo
	}
	return workOrders, nil
}

func (pd *planDocument) Tasks() []planTask {
	if pd == nil || len(pd.Epics) == 0 {
		return nil
	}

	total := 0
	for _, epic := range pd.Epics {
		total += len(epic.Tasks)
	}

	tasks := make([]planTask, 0, total)
	for _, epic := range pd.Epics {
		tasks = append(tasks, epic.Tasks...)
	}
	return tasks
}

func (pd *planDocument) TaskCount() int {
	return len(pd.Tasks())
}

func (pd *planDocument) MarshalIndented() ([]byte, error) {
	return json.MarshalIndent(pd, "", "  ")
}

type auditPlanTask struct {
	planTask
	AuditAction string `json:"audit_action"`
}

type auditPlanEpic struct {
	ID             string          `json:"id"`
	EpicRef        string          `json:"epic_ref"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Covers         []string        `json:"covers,omitempty"`
	DependsOnEpics []string        `json:"depends_on_epics,omitempty"`
	Tasks          []auditPlanTask `json:"tasks"`
}

type auditSummary struct {
	Added     int      `json:"added"`
	Modified  int      `json:"modified"`
	Unchanged int      `json:"unchanged"`
	Changes   []string `json:"changes"`
}

type auditResponse struct {
	Version          int               `json:"version,omitempty"`
	SpecFile         string            `json:"spec_file,omitempty"`
	SpecFingerprint  string            `json:"spec_fingerprint,omitempty"`
	CreatedAt        string            `json:"created_at,omitempty"`
	SessionID        string            `json:"session_id,omitempty"`
	GenerationModel  string            `json:"generation_model,omitempty"`
	Requirements     []planRequirement `json:"requirements,omitempty"`
	NonGoals         []string          `json:"non_goals,omitempty"`
	ExistingSystem   []string          `json:"existing_system,omitempty"`
	PlanningWarnings []string          `json:"planning_warnings,omitempty"`
	Epics            []auditPlanEpic   `json:"epics"`
	AuditSummary     auditSummary      `json:"audit_summary"`
}

func (ar *auditResponse) toPlanDocument() *planDocument {
	epics := make([]planEpic, len(ar.Epics))
	for i, epic := range ar.Epics {
		tasks := make([]planTask, len(epic.Tasks))
		for j, task := range epic.Tasks {
			tasks[j] = task.planTask
			if task.AuditAction == "added" || task.AuditAction == "modified" {
				tasks[j].AuditSource = task.AuditAction
			}
		}
		epics[i] = planEpic{
			ID:             epic.ID,
			EpicRef:        epic.EpicRef,
			Title:          epic.Title,
			Description:    epic.Description,
			Covers:         epic.Covers,
			DependsOnEpics: epic.DependsOnEpics,
			Tasks:          tasks,
		}
	}

	version := ar.Version
	if version == 0 {
		version = planManifestVersion
	}

	return &planDocument{
		Version:          version,
		SpecFile:         ar.SpecFile,
		SpecFingerprint:  ar.SpecFingerprint,
		CreatedAt:        ar.CreatedAt,
		SessionID:        ar.SessionID,
		GenerationModel:  ar.GenerationModel,
		Requirements:     ar.Requirements,
		NonGoals:         ar.NonGoals,
		ExistingSystem:   ar.ExistingSystem,
		PlanningWarnings: ar.PlanningWarnings,
		Epics:            epics,
	}
}
