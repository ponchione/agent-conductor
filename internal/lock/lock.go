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
