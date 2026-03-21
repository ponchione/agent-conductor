# CLI Lock Mechanism Implementation Plan (Spec 08)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent CLI mutation commands and the dashboard server from running simultaneously via a PID-based lock file.

**Architecture:** A new `internal/lock/` package provides `CheckLock`, `WriteLock`, and `RemoveLock` functions operating on `{dataDir}/topham.lock`. The serve command acquires the lock on startup and releases it on shutdown via defer + signal handling. Mutation commands (run, approve, reject, plan) check the lock before executing and refuse with a clear error when the server is active. Read commands are unaffected.

**Tech Stack:** Go (os, syscall, encoding/json)

---

## File Structure

**New Go files:**
- `internal/lock/lock.go` — ServerInfo, CheckLock, WriteLock, RemoveLock, isProcessAlive
- `internal/lock/lock_test.go` — Full lifecycle tests

**Modified Go files:**
- `cmd/conductor/serve.go` — Lock acquire on start, signal handling for graceful shutdown, lock release on exit
- `cmd/conductor/run.go` — Lock check before pipeline execution
- `cmd/conductor/plan.go` — Lock check before plan execution
- `cmd/conductor/gate.go` — Lock check in approve and reject commands

---

## Task 1: Lock Package

**Files:**
- Create: `internal/lock/lock.go`
- Create: `internal/lock/lock_test.go`

- [ ] **Step 1: Create lock.go with all functions**

Create `internal/lock/lock.go`:

```go
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockFileName = "topham.lock"

// ServerInfo describes a running topham server instance.
type ServerInfo struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// lockPath returns the full path to the lock file.
func lockPath(dataDir string) string {
	return filepath.Join(dataDir, lockFileName)
}

// CheckLock checks if a topham server is actively running.
// Returns (serverInfo, nil) if a live server holds the lock.
// Returns (nil, nil) if no lock exists or the lock is stale (stale locks are cleaned up).
// Returns (nil, err) on filesystem errors.
func CheckLock(dataDir string) (*ServerInfo, error) {
	data, err := os.ReadFile(lockPath(dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read lock file: %w", err)
	}

	var info ServerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		// Corrupt lock file — remove it
		slog.Warn("removing corrupt lock file", "error", err)
		_ = RemoveLock(dataDir)
		return nil, nil
	}

	if isProcessAlive(info.PID) {
		return &info, nil
	}

	// Stale lock — PID is dead
	slog.Warn("cleaning up stale lock file", "pid", info.PID)
	_ = RemoveLock(dataDir)
	return nil, nil
}

// WriteLock writes a lock file for the current process.
// Uses O_CREATE|O_EXCL for atomic creation — returns an error if the file already exists.
func WriteLock(dataDir string, port int) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	info := ServerInfo{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock info: %w", err)
	}

	f, err := os.OpenFile(lockPath(dataDir), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("lock file already exists at %s", lockPath(dataDir))
		}
		return fmt.Errorf("create lock file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}
	return nil
}

// RemoveLock removes the lock file. Returns nil if the file does not exist.
func RemoveLock(dataDir string) error {
	err := os.Remove(lockPath(dataDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove lock file: %w", err)
	}
	return nil
}

// CheckServerLock is a convenience function for mutation commands.
// If a live server holds the lock, it returns a user-facing error directing
// them to the dashboard. Returns nil if no lock or stale lock.
func CheckServerLock(dataDir string) error {
	info, err := CheckLock(dataDir)
	if err != nil {
		return fmt.Errorf("check server lock: %w", err)
	}
	if info != nil {
		return fmt.Errorf(
			"Topham server is active on port %d (PID %d, started %s).\nUse the dashboard at http://localhost:%d or stop the server first.",
			info.Port, info.PID, info.StartedAt.Format(time.RFC3339), info.Port,
		)
	}
	return nil
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
```

- [ ] **Step 2: Create lock_test.go**

Create `internal/lock/lock_test.go`:

```go
package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteLockCreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := WriteLock(dir, 8088); err != nil {
		t.Fatalf("WriteLock() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "topham.lock"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var info ServerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if info.Port != 8088 {
		t.Fatalf("Port = %d, want 8088", info.Port)
	}
	if info.StartedAt.IsZero() {
		t.Fatal("StartedAt is zero")
	}
}

func TestWriteLockExclusive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := WriteLock(dir, 8088); err != nil {
		t.Fatalf("first WriteLock() error = %v", err)
	}
	if err := WriteLock(dir, 8089); err == nil {
		t.Fatal("second WriteLock() should have failed")
	}
}

func TestRemoveLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := WriteLock(dir, 8088); err != nil {
		t.Fatalf("WriteLock() error = %v", err)
	}
	if err := RemoveLock(dir); err != nil {
		t.Fatalf("RemoveLock() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "topham.lock")); !os.IsNotExist(err) {
		t.Fatal("lock file should not exist after RemoveLock")
	}
}

func TestRemoveLockIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := RemoveLock(dir); err != nil {
		t.Fatalf("RemoveLock() on missing file error = %v", err)
	}
}

func TestCheckLockNoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	info, err := CheckLock(dir)
	if err != nil {
		t.Fatalf("CheckLock() error = %v", err)
	}
	if info != nil {
		t.Fatalf("CheckLock() = %+v, want nil", info)
	}
}

func TestCheckLockLiveProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write a lock with the current process PID (which is alive)
	if err := WriteLock(dir, 8088); err != nil {
		t.Fatalf("WriteLock() error = %v", err)
	}

	info, err := CheckLock(dir)
	if err != nil {
		t.Fatalf("CheckLock() error = %v", err)
	}
	if info == nil {
		t.Fatal("CheckLock() = nil, want live ServerInfo")
	}
	if info.PID != os.Getpid() {
		t.Fatalf("PID = %d, want %d", info.PID, os.Getpid())
	}
}

func TestCheckLockStalePID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Write a lock file with a dead PID
	info := ServerInfo{
		PID:       999999999, // almost certainly not running
		Port:      8088,
		StartedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "topham.lock"), data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := CheckLock(dir)
	if err != nil {
		t.Fatalf("CheckLock() error = %v", err)
	}
	if result != nil {
		t.Fatal("CheckLock() should return nil for stale lock")
	}

	// Verify the stale lock file was cleaned up
	if _, err := os.Stat(filepath.Join(dir, "topham.lock")); !os.IsNotExist(err) {
		t.Fatal("stale lock file should have been removed")
	}
}

func TestCheckServerLockLive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := WriteLock(dir, 9999); err != nil {
		t.Fatalf("WriteLock() error = %v", err)
	}

	err := CheckServerLock(dir)
	if err == nil {
		t.Fatal("CheckServerLock() should return error for live lock")
	}
}

func TestCheckServerLockNoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := CheckServerLock(dir); err != nil {
		t.Fatalf("CheckServerLock() error = %v, want nil", err)
	}
}

func TestIsProcessAlive(t *testing.T) {
	t.Parallel()

	if !isProcessAlive(os.Getpid()) {
		t.Fatal("isProcessAlive(self) = false, want true")
	}
	if isProcessAlive(999999999) {
		t.Fatal("isProcessAlive(999999999) = true, want false")
	}
}
```

- [ ] **Step 3: Verify build and tests**

Run: `cd /home/gernsback/source/agent-conductor && go test ./internal/lock/ -v -count=1`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/lock/lock.go internal/lock/lock_test.go
git commit -m "feat(spec08): add lock package with PID-based lock file management"
```

---

## Task 2: Serve Command Lock Integration

**Files:**
- Modify: `cmd/conductor/serve.go`

- [ ] **Step 1: Add lock integration to serve command**

In `cmd/conductor/serve.go`:

1. Add imports:
```go
"context"
"net"
"os/signal"
"strconv"
"syscall"

"github.com/ponchione/agent-conductor/internal/lock"
```

2. Replace the RunE body to add lock check, lock write, signal handling, and graceful shutdown. The new flow:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    dbPath, err := resolveServeDBPath(cmd)
    if err != nil {
        return err
    }

    // Resolve data dir for lock file
    dataDir := ""
    if cfg != nil {
        dataDir = cfg.Project.DataDir
    } else if serveDataDir != "" {
        dataDir = serveDataDir
    }

    // Parse port from addr for lock file
    _, portStr, _ := net.SplitHostPort(serveAddr)
    port, _ := strconv.Atoi(portStr)

    // Check for existing live server
    if dataDir != "" {
        existing, err := lock.CheckLock(dataDir)
        if err != nil {
            return fmt.Errorf("check lock: %w", err)
        }
        if existing != nil {
            return fmt.Errorf("another topham server is already running (PID %d, port %d). Stop it first or use that instance.", existing.PID, existing.Port)
        }

        // Write lock
        if err := lock.WriteLock(dataDir, port); err != nil {
            return fmt.Errorf("write lock: %w", err)
        }
        defer lock.RemoveLock(dataDir)
    }

    db, err := database.NewDB(dbPath)
    if err != nil {
        return fmt.Errorf("failed to open database: %w", err)
    }
    defer db.Close()

    gitMgr := git.New(nil)
    baseBranch := "main"
    if cfg != nil && cfg.Git.BaseBranch != "" {
        baseBranch = cfg.Git.BaseBranch
    }

    rq := api.NewRunQueue()

    workOrderDir := ""
    if cfg != nil {
        workOrderDir = filepath.Join(cfg.Project.DataDir, "work-orders")
    }

    handler := api.NewServer(db, gitMgr, baseBranch, rq, workOrderDir, cfg)
    srv := &http.Server{Addr: serveAddr, Handler: handler}

    // Signal handling for graceful shutdown
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    go func() {
        <-ctx.Done()
        slog.Info("shutting down server")
        srv.Shutdown(context.Background())
    }()

    slog.Info("starting observability API server", "addr", serveAddr, "db", dbPath)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        return fmt.Errorf("serve API: %w", err)
    }
    return nil
    // defer runs here, removing the lock file
},
```

- [ ] **Step 2: Verify build**

Run: `cd /home/gernsback/source/agent-conductor && go vet ./cmd/conductor/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/conductor/serve.go
git commit -m "feat(spec08): integrate lock acquire/release into serve command"
```

---

## Task 3: Mutation Command Guards

**Files:**
- Modify: `cmd/conductor/run.go`
- Modify: `cmd/conductor/plan.go`
- Modify: `cmd/conductor/gate.go`

- [ ] **Step 1: Add lock check to run command**

In `cmd/conductor/run.go`, add import `"github.com/ponchione/agent-conductor/internal/lock"`.

In the RunE function, after `config.Validate(cfg)` (around line 45-47) and before the prompts loading, add:

```go
if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
    return err
}
```

- [ ] **Step 2: Add lock check to plan command**

In `cmd/conductor/plan.go`, add import `"github.com/ponchione/agent-conductor/internal/lock"`.

In the `runPlan` function, at the very beginning (after any config validation), add:

```go
if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
    return err
}
```

Read the `runPlan` function first to find the right insertion point — it should be early, before any plan execution begins.

- [ ] **Step 3: Add lock check to approve command**

In `cmd/conductor/gate.go`, add import `"github.com/ponchione/agent-conductor/internal/lock"`.

In the `approveCmd` RunE function, after the UUID validation (line 35-37) and before `openDB()`, add:

```go
if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
    return err
}
```

- [ ] **Step 4: Add lock check to reject command**

In the same `gate.go` file, in the `rejectCmd` RunE function, after the UUID validation and before `openDB()`, add:

```go
if err := lock.CheckServerLock(cfg.Project.DataDir); err != nil {
    return err
}
```

- [ ] **Step 5: Verify build and tests**

Run: `cd /home/gernsback/source/agent-conductor && make build && make test`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/conductor/run.go cmd/conductor/plan.go cmd/conductor/gate.go
git commit -m "feat(spec08): add lock guards to mutation commands (run, plan, approve, reject)"
```

---

## Task 4: Final Verification

- [ ] **Step 1: Run full build**

Run: `make build`
Expected: PASS.

- [ ] **Step 2: Run full tests**

Run: `make test`
Expected: all packages pass.

- [ ] **Step 3: Run go vet on all packages**

Run: `go vet ./...`
Expected: PASS.
