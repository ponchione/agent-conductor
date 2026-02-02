package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ponchione/agent-conductor/internal/config"
)

type GitManager struct {
	cfg *config.Config
}

func New(cfg *config.Config) *GitManager {
	return &GitManager{cfg: cfg}
}

// runGit executes a git command in the given directory
func (g *GitManager) runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w\nstderr: %s", args[0], err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// CreateBranch fetches latest base and creates a new branch
func (g *GitManager) CreateBranch(repoPath, branchName, baseBranch string) error {
	_, _ = g.runGit(repoPath, "fetch", "origin", baseBranch)

	startPoint := "origin/" + baseBranch
	if _, err := g.runGit(repoPath, "rev-parse", "--verify", startPoint); err != nil {
		startPoint = baseBranch
	}

	_, err := g.runGit(repoPath, "checkout", "-b", branchName, startPoint)
	return err
}

func (g *GitManager) Checkout(repoPath, branchName string) error {
	_, err := g.runGit(repoPath, "checkout", branchName)
	return err
}

func (g *GitManager) Commit(repoPath, message string) error {

	//if g.cfg.Git.CommitAuthorName != "" {
	//	g.runGit(repoPath, "config", "user.name", g.cfg.Git.CommitAuthorName)
	//	g.runGit(repoPath, "config", "user.email", g.cfg.Git.CommitAuthorEmail)
	//}

	_, err := g.runGit(repoPath, "add", ".")
	if err != nil {
		return err
	}

	_, err = g.runGit(repoPath, "commit", "-m", message)
	return err
}

func (g *GitManager) GetChangedFiles(repoPath string) ([]string, error) {
	output, err := g.runGit(repoPath, "diff", "--name-only")
	if err != nil {
		return nil, err
	}

	staged, err := g.runGit(repoPath, "diff", "--name-only", "--cached")
	if err != nil {
		return nil, err
	}

	files := make(map[string]struct{})
	if output != "" {
		for _, f := range strings.Split(output, "\n") {
			files[f] = struct{}{}
		}
	}
	if staged != "" {
		for _, f := range strings.Split(staged, "\n") {
			files[f] = struct{}{}
		}
	}

	result := make([]string, 0, len(files))
	for f := range files {
		result = append(result, f)
	}
	return result, nil
}

func (g *GitManager) HasUncommittedChanges(repoPath string) (bool, error) {
	output, err := g.runGit(repoPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return output != "", nil
}

func (g *GitManager) Push(repoPath, branchName string) error {
	_, err := g.runGit(repoPath, "push", "-u", "origin", branchName)
	return err
}

// GetCurrentBranch returns the name of the currently checked out branch
func (g *GitManager) GetCurrentBranch(repoPath string) (string, error) {
	return g.runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
}
