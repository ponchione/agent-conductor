package git

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/ponchione/agent-conductor/internal/config"
)

// CreateBranch creates a new branch at the head of baseBranch.
// Returns an error if the branch already exists.
func (g *GitManager) CreateBranch(repoPath, branchName, baseBranch string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	baseRef, err := r.Reference(plumbing.NewBranchReferenceName(baseBranch), true)
	if err != nil {
		return fmt.Errorf("resolve base branch %s: %w", baseBranch, err)
	}

	newRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branchName), baseRef.Hash())
	if err := r.Storer.SetReference(newRef); err != nil {
		return fmt.Errorf("create branch %s: %w", branchName, err)
	}

	return nil
}

// CheckoutBranch switches the worktree to the named branch.
func (g *GitManager) CheckoutBranch(repoPath, branchName string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	}); err != nil {
		return fmt.Errorf("checkout %s: %w", branchName, err)
	}

	return nil
}

// BranchExists reports whether a local branch reference exists.
func (g *GitManager) BranchExists(repoPath, branchName string) (bool, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, fmt.Errorf("open repo: %w", err)
	}

	_, err = r.Reference(plumbing.NewBranchReferenceName(branchName), true)
	if err == plumbing.ErrReferenceNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check branch %s: %w", branchName, err)
	}
	return true, nil
}

// MergeBranch fast-forward merges target into base within the given repo.
// Only fast-forward merges are supported; returns an error if branches have diverged.
func (g *GitManager) MergeBranch(repoPath, base, target string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(base),
	}); err != nil {
		return fmt.Errorf("checkout %s: %w", base, err)
	}

	targetRef, err := r.Reference(plumbing.NewBranchReferenceName(target), true)
	if err != nil {
		return fmt.Errorf("resolve branch %s: %w", target, err)
	}

	if err := r.Merge(*targetRef, git.MergeOptions{
		Strategy: git.FastForwardMerge,
	}); err != nil {
		return fmt.Errorf("merge %s into %s: %w", target, base, err)
	}

	head, err := r.Head()
	if err != nil {
		return fmt.Errorf("get HEAD after merge: %w", err)
	}
	if err := wt.Reset(&git.ResetOptions{
		Commit: head.Hash(),
		Mode:   git.HardReset,
	}); err != nil {
		return fmt.Errorf("reset worktree: %w", err)
	}

	return nil
}

// DeleteBranch removes a local branch reference from the repository.
func (g *GitManager) DeleteBranch(repoPath, branchName string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	return r.Storer.RemoveReference(plumbing.NewBranchReferenceName(branchName))
}

type GitManager struct {
	cfg *gitconfig.ProjectConfig
}

func New(cfg *gitconfig.ProjectConfig) *GitManager {
	return &GitManager{cfg: cfg}
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
