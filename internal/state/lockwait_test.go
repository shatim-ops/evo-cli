// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

// A short write (an MCP tool recording the VM it created) waits for a
// deploy or destroy in the same directory instead of failing or racing it.
func TestLockWait_WaitsForHolder(t *testing.T) {
	old := lockRetryEvery
	lockRetryEvery = 10 * time.Millisecond
	t.Cleanup(func() { lockRetryEvery = old })

	path := filepath.Join(t.TempDir(), ".evo", "state.json")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		unlock()
	}()
	start := time.Now()
	unlock2, err := LockWait(path, 5*time.Second)
	if err != nil {
		t.Fatalf("LockWait: %v", err)
	}
	unlock2()
	// wall-clock: a lower bound — the holder sleeps 100 ms, and a sleep never ends early.
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Errorf("LockWait returned after %v, before the holder released", waited)
	}
}

func TestLockWait_GivesUpAfterMaxWait(t *testing.T) {
	old := lockRetryEvery
	lockRetryEvery = 10 * time.Millisecond
	t.Cleanup(func() { lockRetryEvery = old })

	path := filepath.Join(t.TempDir(), ".evo", "state.json")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := LockWait(path, 50*time.Millisecond); codeOf(err) != clierr.CodeStateLocked {
		t.Fatalf("LockWait on a held lock: %v, want %s", err, clierr.CodeStateLocked)
	}
}

// Update reads, changes and writes under the lock.
func TestUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evo", "state.json")
	if err := Update(path, time.Second, func(f *File) error {
		f.UpsertDeployment(DeploymentRecord{ID: "vm-1", VMID: "vm-1", Status: "creating"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := f.FindDeployment("vm-1"); d == nil || d.Status != "creating" {
		t.Fatalf("state after Update: %+v", f.Deployments)
	}
}

// FileLock is held across open files (as across processes): a second
// holder waits, then gets ErrFileLocked; after the release it gets the lock.
func TestFileLock_ExclusiveAndReleased(t *testing.T) {
	old := lockRetryEvery
	lockRetryEvery = 10 * time.Millisecond
	t.Cleanup(func() { lockRetryEvery = old })

	path := filepath.Join(t.TempDir(), "jobs", "stk-0123456789.lock")
	unlock, err := FileLock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FileLock(path, 50*time.Millisecond); !errors.Is(err, ErrFileLocked) {
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			t.Fatalf("second FileLock while held: %v, want ErrFileLocked", err)
		}
	}
	unlock()
	unlock2, err := FileLock(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("FileLock after release: %v", err)
	}
	unlock2()
}
