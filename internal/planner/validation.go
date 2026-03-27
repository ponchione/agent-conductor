package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
)

const (
	maxPlanKnownFilesPerWorkOrder = 8
	maxPlanAcceptanceCriteria     = 12
)

// ValidatePlanDocument validates a plan document against its config,
// checking for requirement coverage, dependency ordering, known file existence,
// duplicate titles, and other business rules.
func ValidatePlanDocument(doc *PlanDocument, cfg *config.ProjectConfig) error {
	if doc == nil {
		return fmt.Errorf("plan document is required")
	}
	if err := doc.Validate(); err != nil {
		return err
	}

	requirementCoverage := make(map[string]int, len(doc.Requirements))
	for _, req := range doc.Requirements {
		requirementCoverage[req.ID] = 0
	}

	seenTitles := make(map[string]int)
	seenKnownFileSets := make(map[string]int)
	tasks := doc.Tasks()
	taskIndexByID := make(map[string]int, len(tasks))
	for i, task := range tasks {
		taskIndexByID[task.ID] = i
	}

	for i, task := range tasks {
		titleKey := strings.ToLower(strings.TrimSpace(task.Title))
		if prior, ok := seenTitles[titleKey]; ok {
			return fmt.Errorf("task %d (%q) overlaps with task %d via duplicate title", i+1, task.Title, prior+1)
		}
		seenTitles[titleKey] = i

		if len(task.KnownFiles) > maxPlanKnownFilesPerWorkOrder {
			return fmt.Errorf("task %d (%q) is oversized: %d known_files exceeds limit %d", i+1, task.Title, len(task.KnownFiles), maxPlanKnownFilesPerWorkOrder)
		}
		if len(task.AcceptanceCriteria) > maxPlanAcceptanceCriteria {
			return fmt.Errorf("task %d (%q) is oversized: %d acceptance criteria exceeds limit %d", i+1, task.Title, len(task.AcceptanceCriteria), maxPlanAcceptanceCriteria)
		}

		for _, req := range task.Requirements {
			if _, ok := requirementCoverage[req.ID]; ok {
				requirementCoverage[req.ID]++
			}
		}

		if err := validateKnownFiles(task, cfg); err != nil {
			return fmt.Errorf("task %d (%q): %w", i+1, task.Title, err)
		}
		if err := validateDependencyOrdering(i, task, taskIndexByID); err != nil {
			return fmt.Errorf("task %d (%q): %w", i+1, task.Title, err)
		}

		if key := normalizedKnownFileSet(task.KnownFiles); key != "" {
			if prior, ok := seenKnownFileSets[key]; ok {
				return fmt.Errorf("task %d (%q) obviously overlaps with task %d via identical known_files", i+1, task.Title, prior+1)
			}
			seenKnownFileSets[key] = i
		}
	}

	for _, req := range doc.Requirements {
		if requirementCoverage[req.ID] == 0 {
			return fmt.Errorf("requirement %q is not covered by any work order", req.ID)
		}
	}

	return nil
}

func validateKnownFiles(task PlanTask, cfg *config.ProjectConfig) error {
	for _, knownFile := range task.KnownFiles {
		cleaned := filepath.Clean(strings.TrimSpace(knownFile))
		if cleaned == "." || cleaned == "" {
			return fmt.Errorf("known_files contains an invalid path %q", knownFile)
		}
		if filepath.IsAbs(cleaned) {
			return fmt.Errorf("known_files path %q must be relative", knownFile)
		}
		if strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("known_files path %q escapes the repository root", knownFile)
		}

		absPath := filepath.Join(cfg.Project.Path, cleaned)
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("known_files path %q does not exist", knownFile)
			}
			return fmt.Errorf("known_files path %q could not be checked: %w", knownFile, err)
		}
		if info.IsDir() {
			return fmt.Errorf("known_files path %q must reference a file, not a directory", knownFile)
		}
	}
	return nil
}

func validateDependencyOrdering(index int, task PlanTask, taskIndexByID map[string]int) error {
	if len(task.DependsOn) == 0 {
		return nil
	}

	for _, dep := range task.DependsOn {
		depIndex, ok := taskIndexByID[dep]
		if !ok {
			return fmt.Errorf("depends_on references unknown task id %q", dep)
		}
		if depIndex >= index {
			return fmt.Errorf("depends_on references task %q at position %d, which is not earlier in the sequence", dep, depIndex+1)
		}
	}

	return nil
}

func normalizedKnownFileSet(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed != "" {
			normalized = append(normalized, filepath.Clean(trimmed))
		}
	}
	if len(normalized) < 2 {
		return ""
	}
	slices.Sort(normalized)
	return strings.Join(normalized, "|")
}
