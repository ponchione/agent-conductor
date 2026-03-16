package verify

import (
	"strings"

	"github.com/ponchione/agent-conductor/internal/models"
)

const (
	statusPass = "PASS"
	statusWarn = "WARN"
	statusFail = "FAIL"
)

func NormalizeVerificationReport(
	wo *models.WorkOrder,
	report *models.VerificationReport,
	preChecks []PreCheckResult,
) *models.VerificationReport {
	if report == nil {
		report = &models.VerificationReport{}
	}

	report.Completeness.CriteriaResults = mergeCriterionResults(
		buildCriterionCatalog(wo),
		report.Completeness.CriteriaResults,
		preChecks,
	)
	report.Completeness.AllCriteriaMet = allCriteriaMet(report.Completeness.CriteriaResults)

	if len(report.PatternConsistency.Issues) > 0 {
		report.PatternConsistency.FollowsConventions = false
	}
	if report.Status == "" {
		report.Status = statusPass
	}
	report.Status = DeriveWorkflowStatus(report)
	return report
}

func BuildPrecheckOnlyReport(
	wo *models.WorkOrder,
	preChecks []PreCheckResult,
	summary string,
) *models.VerificationReport {
	report := &models.VerificationReport{
		Summary: summary,
		PatternConsistency: models.PatternConsistency{
			FollowsConventions: true,
			Issues:             []string{},
		},
		Concerns: []string{},
	}
	return NormalizeVerificationReport(wo, report, preChecks)
}

func DeriveWorkflowStatus(report *models.VerificationReport) string {
	severity := 0

	for _, criterion := range report.Completeness.CriteriaResults {
		required := criterion.Required == nil || *criterion.Required
		switch criterion.NormalizedResult() {
		case models.CriterionResultUnmet:
			if required {
				severity = maxSeverity(severity, 2)
			} else {
				severity = maxSeverity(severity, 1)
			}
		case models.CriterionResultUnassessable:
			severity = maxSeverity(severity, 1)
		}
	}

	if report.ScopeDrift.Detected || len(report.ScopeDrift.UnscopedFiles) > 0 {
		severity = maxSeverity(severity, 1)
	}
	if len(report.PatternConsistency.Issues) > 0 {
		severity = maxSeverity(severity, 1)
	}
	if len(report.Concerns) > 0 {
		severity = maxSeverity(severity, 1)
	}

	switch severity {
	case 2:
		return statusFail
	case 1:
		return statusWarn
	default:
		return statusPass
	}
}

func buildCriterionCatalog(wo *models.WorkOrder) []models.CriterionResult {
	if wo == nil {
		return nil
	}

	if wo.EffectiveSchemaVersion() >= 2 && len(wo.TypedAcceptanceCriteria) > 0 {
		results := make([]models.CriterionResult, 0, len(wo.TypedAcceptanceCriteria))
		for _, criterion := range wo.TypedAcceptanceCriteria {
			results = append(results, models.CriterionResult{
				CriterionID:      criterion.ID,
				Criterion:        criterion.Description,
				Required:         criterion.Required,
				VerificationKind: criterion.Verification.Kind,
			})
		}
		return results
	}

	required := true
	results := make([]models.CriterionResult, 0, len(wo.AcceptanceCriteria))
	for _, criterion := range wo.AcceptanceCriteria {
		results = append(results, models.CriterionResult{
			Criterion:        criterion,
			Required:         &required,
			VerificationKind: "legacy",
		})
	}
	return results
}

func mergeCriterionResults(
	catalog []models.CriterionResult,
	reportResults []models.CriterionResult,
	preChecks []PreCheckResult,
) []models.CriterionResult {
	grouped := make(map[string][]models.CriterionResult)
	var extras []models.CriterionResult

	for _, result := range preChecks {
		key := criterionKey(result)
		grouped[key] = append(grouped[key], result)
	}
	for _, result := range reportResults {
		key := criterionKey(result)
		grouped[key] = append(grouped[key], result)
	}

	merged := make([]models.CriterionResult, 0, len(catalog))
	seenKeys := make(map[string]bool, len(catalog))
	for _, meta := range catalog {
		key := criterionKey(meta)
		seenKeys[key] = true
		candidates := grouped[key]
		if len(candidates) == 0 {
			merged = append(merged, finalizeCriterion(meta, nil))
			continue
		}
		merged = append(merged, finalizeCriterion(meta, candidates))
	}

	for _, candidates := range grouped {
		if len(candidates) == 0 {
			continue
		}
		key := criterionKey(candidates[0])
		if seenKeys[key] {
			continue
		}
		extras = append(extras, finalizeCriterion(candidates[0], candidates))
	}

	return append(merged, extras...)
}

func finalizeCriterion(meta models.CriterionResult, candidates []models.CriterionResult) models.CriterionResult {
	result := meta
	if strings.TrimSpace(result.Criterion) == "" && len(candidates) > 0 {
		result.Criterion = candidates[0].Criterion
	}
	if result.Required == nil {
		for _, candidate := range candidates {
			if candidate.Required != nil {
				result.Required = candidate.Required
				break
			}
		}
	}
	if result.VerificationKind == "" {
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.VerificationKind) != "" {
				result.VerificationKind = candidate.VerificationKind
				break
			}
		}
	}

	notes := make([]string, 0, len(candidates))
	finalResult := ""
	for _, candidate := range candidates {
		switch candidate.NormalizedResult() {
		case models.CriterionResultUnmet:
			finalResult = models.CriterionResultUnmet
		case models.CriterionResultMet:
			if finalResult != models.CriterionResultUnmet {
				finalResult = models.CriterionResultMet
			}
		case models.CriterionResultUnassessable:
			if finalResult == "" {
				finalResult = models.CriterionResultUnassessable
			}
		}
		if note := strings.TrimSpace(candidate.Notes); note != "" && !containsString(notes, note) {
			notes = append(notes, note)
		}
		if result.CriterionID == "" && candidate.CriterionID != "" {
			result.CriterionID = candidate.CriterionID
		}
	}

	if finalResult == "" {
		finalResult = models.CriterionResultUnassessable
		if len(notes) == 0 {
			notes = append(notes, "No verification result recorded for criterion")
		}
	}

	result.Result = finalResult
	result.Notes = strings.Join(notes, " | ")
	return result
}

func allCriteriaMet(results []models.CriterionResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if result.NormalizedResult() != models.CriterionResultMet {
			return false
		}
	}
	return true
}

func criterionKey(result models.CriterionResult) string {
	if strings.TrimSpace(result.CriterionID) != "" {
		return "id:" + strings.ToLower(strings.TrimSpace(result.CriterionID))
	}
	return "description:" + strings.ToLower(strings.TrimSpace(result.Criterion))
}

func maxSeverity(current, candidate int) int {
	if candidate > current {
		return candidate
	}
	return current
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
