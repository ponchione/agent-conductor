package main

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

func validatePlanDocument(doc *planDocument, cfg *config.ProjectConfig) error {
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

	for i, wo := range doc.WorkOrders {
		titleKey := strings.ToLower(strings.TrimSpace(wo.Title))
		if prior, ok := seenTitles[titleKey]; ok {
			return fmt.Errorf("work order %d (%q) overlaps with work order %d via duplicate title", i+1, wo.Title, prior+1)
		}
		seenTitles[titleKey] = i

		if len(wo.KnownFiles) > maxPlanKnownFilesPerWorkOrder {
			return fmt.Errorf("work order %d (%q) is oversized: %d known_files exceeds limit %d", i+1, wo.Title, len(wo.KnownFiles), maxPlanKnownFilesPerWorkOrder)
		}
		if len(wo.AcceptanceCriteria) > maxPlanAcceptanceCriteria {
			return fmt.Errorf("work order %d (%q) is oversized: %d acceptance criteria exceeds limit %d", i+1, wo.Title, len(wo.AcceptanceCriteria), maxPlanAcceptanceCriteria)
		}

		if len(wo.Covers) == 0 && len(doc.Requirements) > 0 {
			return fmt.Errorf("work order %d (%q) is missing requirement coverage", i+1, wo.Title)
		}
		for _, reqID := range wo.Covers {
			if _, ok := requirementCoverage[reqID]; ok {
				requirementCoverage[reqID]++
			}
		}

		if err := validateKnownFiles(wo, cfg); err != nil {
			return fmt.Errorf("work order %d (%q): %w", i+1, wo.Title, err)
		}
		if err := validateDependencyOrdering(i, wo, doc.WorkOrders); err != nil {
			return fmt.Errorf("work order %d (%q): %w", i+1, wo.Title, err)
		}

		if key := normalizedKnownFileSet(wo.KnownFiles); key != "" {
			if prior, ok := seenKnownFileSets[key]; ok {
				return fmt.Errorf("work order %d (%q) obviously overlaps with work order %d via identical known_files", i+1, wo.Title, prior+1)
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

func validateKnownFiles(wo planWorkOrder, cfg *config.ProjectConfig) error {
	for _, knownFile := range wo.KnownFiles {
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

func validateDependencyOrdering(index int, wo planWorkOrder, workOrders []planWorkOrder) error {
	if len(wo.DependsOn) == 0 {
		return nil
	}

	byTitle := make(map[string]int, len(workOrders))
	for i, candidate := range workOrders {
		byTitle[strings.ToLower(strings.TrimSpace(candidate.Title))] = i
	}

	for _, dep := range wo.DependsOn {
		depKey := strings.ToLower(strings.TrimSpace(dep))
		depIndex, ok := byTitle[depKey]
		if !ok {
			return fmt.Errorf("depends_on references unknown work order %q", dep)
		}
		if depIndex >= index {
			return fmt.Errorf("depends_on references work order %q at position %d, which is not earlier in the sequence", dep, depIndex+1)
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
