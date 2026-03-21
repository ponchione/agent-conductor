package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

var errBranchNotFound = errors.New("branch not found")

// gitDiffResult holds the parsed output of the three git diff commands.
type gitDiffResult struct {
	Diff         string
	FilesChanged []string
	Additions    int
	Deletions    int
}

// runGitDiff runs three git commands against the repo to produce a diff between
// baseBranch and workflowBranch. Returns errBranchNotFound if the workflow
// branch does not exist.
func runGitDiff(repoPath, baseBranch, workflowBranch string) (gitDiffResult, error) {
	// 1. Check branch exists.
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+workflowBranch)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return gitDiffResult{}, fmt.Errorf("%w: %s", errBranchNotFound, strings.TrimSpace(string(out)))
	}

	// 2. Full diff.
	cmd = exec.Command("git", "diff", baseBranch+"..."+workflowBranch)
	cmd.Dir = repoPath
	diffOut, err := cmd.Output()
	if err != nil {
		return gitDiffResult{}, fmt.Errorf("git diff: %w", err)
	}

	// 3. File list.
	cmd = exec.Command("git", "diff", "--name-only", baseBranch+"..."+workflowBranch)
	cmd.Dir = repoPath
	nameOut, err := cmd.Output()
	if err != nil {
		return gitDiffResult{}, fmt.Errorf("git diff --name-only: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(nameOut)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	// 4. Stats (additions/deletions).
	cmd = exec.Command("git", "diff", "--stat", baseBranch+"..."+workflowBranch)
	cmd.Dir = repoPath
	statOut, err := cmd.Output()
	if err != nil {
		return gitDiffResult{}, fmt.Errorf("git diff --stat: %w", err)
	}

	additions, deletions := parseDiffStat(string(statOut))

	return gitDiffResult{
		Diff:         string(diffOut),
		FilesChanged: files,
		Additions:    additions,
		Deletions:    deletions,
	}, nil
}

// parseDiffStat extracts total insertions and deletions from the summary line
// of git diff --stat output. The summary line looks like:
//
//	3 files changed, 10 insertions(+), 2 deletions(-)
func parseDiffStat(stat string) (additions, deletions int) {
	lines := strings.Split(strings.TrimSpace(stat), "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	summary := lines[len(lines)-1]

	for _, part := range strings.Split(summary, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "insertion") {
			additions = extractLeadingInt(part)
		} else if strings.Contains(part, "deletion") {
			deletions = extractLeadingInt(part)
		}
	}
	return additions, deletions
}

// extractLeadingInt extracts the first integer from a string like "10 insertions(+)".
func extractLeadingInt(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(fields[0])
	return n
}

func (s *Server) handleGetWorkflowDiff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	wf, _, _, err := s.db.GetWorkflowDetailForUI(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get workflow: %v", err))
		return
	}

	result, err := runGitDiff(wf.TargetRepo, s.baseBranch, wf.GitBranch)
	if err != nil {
		if errors.Is(err, errBranchNotFound) {
			writeError(w, http.StatusNotFound, "branch not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("git diff: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, workflowDiffResponse{
		Diff:           result.Diff,
		BaseBranch:     s.baseBranch,
		WorkflowBranch: wf.GitBranch,
		FilesChanged:   result.FilesChanged,
		Stats: diffStatsResponse{
			Additions: result.Additions,
			Deletions: result.Deletions,
			Files:     len(result.FilesChanged),
		},
	})
}
