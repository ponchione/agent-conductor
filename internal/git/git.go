package git

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitconfig "github.com/ponchione/agent-conductor/internal/config"
)

type GitManager struct {
	cfg *gitconfig.Config
}

func New(cfg *gitconfig.Config) *GitManager {
	return &GitManager{cfg: cfg}
}

// CreateBranch fetches the latest base and creates a new branch
func (g *GitManager) CreateBranch(repoPath, branchName, baseBranch string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repo: %w", err)
	}

	// Fetch latest to ensure we have the base
	err = r.Fetch(&git.FetchOptions{
		RemoteName: "origin",
	})
	if err != nil && err != git.NoErrAlreadyUpToDate && err != git.ErrRemoteNotFound {

	}

	// Resolve the Hash of the base branch
	var hash *plumbing.Hash

	// Try remote reference
	refName := plumbing.NewRemoteReferenceName("origin", baseBranch)
	ref, err := r.Reference(refName, true)
	if err == nil {
		h := ref.Hash()
		hash = &h
	} else {
		// Fallback to local head
		refName = plumbing.NewBranchReferenceName(baseBranch)
		ref, err = r.Reference(refName, true)
		if err != nil {
			return fmt.Errorf("base branch %s not found: %w", baseBranch, err)
		}
		h := ref.Hash()
		hash = &h
	}

	// Create the new branch reference
	w, err := r.Worktree()
	if err != nil {
		return err
	}

	newBranchRef := plumbing.NewBranchReferenceName(branchName)
	// Check if exists
	if _, err := r.Reference(newBranchRef, true); err == nil {
		// Already exists - just checkout? Or error?
	} else {
		// Create it pointing to the hash
		newRef := plumbing.NewHashReference(newBranchRef, *hash)
		if err := r.Storer.SetReference(newRef); err != nil {
			return fmt.Errorf("failed to create branch ref: %w", err)
		}
	}

	//  Checkout
	return w.Checkout(&git.CheckoutOptions{
		Branch: newBranchRef,
		Create: false,
		Force:  false,
	})
}

func (g *GitManager) Checkout(repoPath, branchName string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
	})
}

func (g *GitManager) Commit(repoPath, message string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	if _, err := w.Add("."); err != nil {
		return err
	}

	// Author info
	author := &object.Signature{
		Name:  g.cfg.Git.CommitAuthorName,
		Email: g.cfg.Git.CommitAuthorEmail,
		When:  time.Now(),
	}

	_, err = w.Commit(message, &git.CommitOptions{
		Author: author,
	})
	return err
}

func (g *GitManager) GetChangedFiles(repoPath string) ([]string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	w, err := r.Worktree()
	if err != nil {
		return nil, err
	}

	status, err := w.Status()
	if err != nil {
		return nil, err
	}

	var changed []string
	for file, fileStatus := range status {
		if fileStatus.Worktree != git.Unmodified || fileStatus.Staging != git.Unmodified {
			changed = append(changed, file)
		}
	}

	return changed, nil
}

func (g *GitManager) HasUncommittedChanges(repoPath string) (bool, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, err
	}
	w, err := r.Worktree()
	if err != nil {
		return false, err
	}
	status, err := w.Status()
	if err != nil {
		return false, err
	}
	return !status.IsClean(), nil
}

func (g *GitManager) Push(repoPath, branchName string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	err = r.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName)),
		},
	})

	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// GetCurrentBranch returns the name of the currently checked out branch
func (g *GitManager) GetCurrentBranch(repoPath string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}

	head, err := r.Head()
	if err != nil {
		return "", err
	}

	if head.Name().IsBranch() {
		return head.Name().Short(), nil
	}
	return "", fmt.Errorf("detached head")
}
