package git

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitconfig "github.com/ponchione/agent-conductor/internal/config"
)

type GitManager struct {
	cfg *gitconfig.ProjectConfig
}

func New(cfg *gitconfig.ProjectConfig) *GitManager {
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
		slog.Warn("git fetch failed", "error", err)
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

// GetDiff returns the diff between two branches
func (g *GitManager) GetDiff(repoPath, base, target string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}

	// Resolve Base
	baseHash, err := r.ResolveRevision(plumbing.Revision(base))
	if err != nil {
		return "", fmt.Errorf("failed to resolve base %s: %w", base, err)
	}

	// Resolve Target
	targetHash, err := r.ResolveRevision(plumbing.Revision(target))
	if err != nil {
		return "", fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	// Get Commit Objects
	baseCommit, err := r.CommitObject(*baseHash)
	if err != nil {
		return "", err
	}
	targetCommit, err := r.CommitObject(*targetHash)
	if err != nil {
		return "", err
	}

	// Get Trees
	baseTree, err := baseCommit.Tree()
	if err != nil {
		return "", err
	}
	targetTree, err := targetCommit.Tree()
	if err != nil {
		return "", err
	}

	// Get Changes
	changes, err := baseTree.Diff(targetTree)
	if err != nil {
		return "", err
	}

	// Patch
	patch, err := changes.Patch()
	if err != nil {
		return "", err
	}

	return patch.String(), nil
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

type CommitInfo struct {
	Hash    string
	Message string
	Author  string
	Date    time.Time
}

// GetRecentCommits returns the last n commits affecting the given path
func (g *GitManager) GetRecentCommits(repoPath string, count int, pathFilter string) ([]CommitInfo, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	// Retrieve the commit history
	cIter, err := r.Log(&git.LogOptions{
		Order:    git.LogOrderCommitterTime,
		FileName: &pathFilter,
	})
	if err != nil {
		return nil, err
	}

	var commits []CommitInfo
	err = cIter.ForEach(func(c *object.Commit) error {
		if len(commits) >= count {
			return fmt.Errorf("limit reached") // stop iteration
		}

		// Clean message (first line only)
		msg := c.Message
		lines := splitLines(msg)
		if len(lines) > 0 {
			msg = lines[0]
		}

		commits = append(commits, CommitInfo{
			Hash:    c.Hash.String()[:7],
			Message: msg,
			Author:  c.Author.Name,
			Date:    c.Author.When,
		})
		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, err
	}

	return commits, nil
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
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
