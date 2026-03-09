package git

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/ponchione/agent-conductor/internal/config"
)

// CreateBranch creates a new branch at the head of baseBranch.
func (g *GitManager) CreateBranch(repoPath, branchName, baseBranch string) error {
	return g.run(repoPath, "branch", branchName, baseBranch)
}

// CheckoutBranch switches the worktree to the named branch.
func (g *GitManager) CheckoutBranch(repoPath, branchName string) error {
	return g.run(repoPath, "checkout", branchName)
}

// BranchExists reports whether a local branch reference exists.
func (g *GitManager) BranchExists(repoPath, branchName string) (bool, error) {
	err := g.run(repoPath, "rev-parse", "--verify", "refs/heads/"+branchName)
	if err != nil {
		// rev-parse exits non-zero if the ref doesn't exist.
		return false, nil
	}
	return true, nil
}

// MergeBranch fast-forward merges target into base within the given repo.
// Only fast-forward merges are supported; returns an error if branches have diverged.
func (g *GitManager) MergeBranch(repoPath, base, target string) error {
	if err := g.run(repoPath, "checkout", base); err != nil {
		return fmt.Errorf("checkout %s: %w", base, err)
	}
	if err := g.run(repoPath, "merge", "--ff-only", target); err != nil {
		return fmt.Errorf("merge %s into %s: %w", target, base, err)
	}
	return nil
}

// DeleteBranch removes a local branch.
func (g *GitManager) DeleteBranch(repoPath, branchName string) error {
	return g.run(repoPath, "branch", "-d", branchName)
}

type GitManager struct {
	cfg *gitconfig.ProjectConfig
}

func New(cfg *gitconfig.ProjectConfig) *GitManager {
	return &GitManager{cfg: cfg}
}

// run executes a git command in the given repo directory.
func (g *GitManager) run(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

// runOutput executes a git command and returns its stdout.
func (g *GitManager) runOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WorktreeClean returns true if there are no uncommitted or unstaged changes.
func (g *GitManager) WorktreeClean(repoPath string) (bool, error) {
	out, err := g.runOutput(repoPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// GetDiff returns the diff between two branches.
func (g *GitManager) GetDiff(repoPath, base, target string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}

	baseHash, err := r.ResolveRevision(plumbing.Revision(base))
	if err != nil {
		return "", fmt.Errorf("failed to resolve base %s: %w", base, err)
	}

	targetHash, err := r.ResolveRevision(plumbing.Revision(target))
	if err != nil {
		return "", fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	baseCommit, err := r.CommitObject(*baseHash)
	if err != nil {
		return "", err
	}
	targetCommit, err := r.CommitObject(*targetHash)
	if err != nil {
		return "", err
	}

	baseTree, err := baseCommit.Tree()
	if err != nil {
		return "", err
	}
	targetTree, err := targetCommit.Tree()
	if err != nil {
		return "", err
	}

	changes, err := baseTree.Diff(targetTree)
	if err != nil {
		return "", err
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", err
	}

	return patch.String(), nil
}

// GetChangedFilesBetween returns the list of file paths that differ between base and target branches.
func (g *GitManager) GetChangedFilesBetween(repoPath, base, target string) ([]string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	baseHash, err := r.ResolveRevision(plumbing.Revision(base))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base %s: %w", base, err)
	}
	targetHash, err := r.ResolveRevision(plumbing.Revision(target))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	baseCommit, err := r.CommitObject(*baseHash)
	if err != nil {
		return nil, err
	}
	targetCommit, err := r.CommitObject(*targetHash)
	if err != nil {
		return nil, err
	}

	baseTree, err := baseCommit.Tree()
	if err != nil {
		return nil, err
	}
	targetTree, err := targetCommit.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := baseTree.Diff(targetTree)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, c := range changes {
		if c.To.Name != "" {
			files = append(files, c.To.Name)
		} else if c.From.Name != "" {
			files = append(files, c.From.Name)
		}
	}
	return files, nil
}
