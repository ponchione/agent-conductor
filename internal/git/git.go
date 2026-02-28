package git

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitconfig "github.com/ponchione/agent-conductor/internal/config"
)

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
