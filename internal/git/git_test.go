package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ponchione/agent-conductor/internal/config"
)

func TestWorktreeDiffAgainstBase_IncludesTrackedAndUntrackedChanges(t *testing.T) {
	repoPath := initGitRepo(t)
	manager := New(&config.ProjectConfig{})

	if err := manager.CreateBranch(repoPath, "feature/test", "main"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	if err := manager.CheckoutBranch(repoPath, "feature/test"); err != nil {
		t.Fatalf("CheckoutBranch() error: %v", err)
	}

	trackedPath := filepath.Join(repoPath, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("base\nworktree change\n"), 0644); err != nil {
		t.Fatalf("writing tracked change: %v", err)
	}

	untrackedPath := filepath.Join(repoPath, "new.txt")
	if err := os.WriteFile(untrackedPath, []byte("brand new file\n"), 0644); err != nil {
		t.Fatalf("writing untracked file: %v", err)
	}

	files, err := manager.GetChangedFilesAgainstBase(repoPath, "main")
	if err != nil {
		t.Fatalf("GetChangedFilesAgainstBase() error: %v", err)
	}
	wantFiles := []string{"new.txt", "tracked.txt"}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("files = %v, want %v", files, wantFiles)
	}

	diff, err := manager.GetWorktreeDiffAgainstBase(repoPath, "main")
	if err != nil {
		t.Fatalf("GetWorktreeDiffAgainstBase() error: %v", err)
	}
	if !strings.Contains(diff, "worktree change") {
		t.Fatalf("diff missing tracked change:\n%s", diff)
	}
	if !strings.Contains(diff, "brand new file") {
		t.Fatalf("diff missing untracked file contents:\n%s", diff)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	repoPath := t.TempDir()

	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.name", "Test User")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "branch", "-m", "main")

	initialFile := filepath.Join(repoPath, "tracked.txt")
	if err := os.WriteFile(initialFile, []byte("base\n"), 0644); err != nil {
		t.Fatalf("writing initial file: %v", err)
	}

	runGit(t, repoPath, "add", "tracked.txt")
	runGit(t, repoPath, "commit", "-m", "initial commit")

	return repoPath
}

func runGit(t *testing.T, repoPath string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}
