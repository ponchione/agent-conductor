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
