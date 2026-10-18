// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

func codeOf(err error) string {
	var ce *clierr.CLIError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Two writers (two evo processes, or MCP calls over HTTP) used the
// same "state.json.tmp": one rename found the file gone, or moved the
// other's half-written content into place.
func TestWrite_ConcurrentWritersNeverFailOrCorrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Replacing one target from several writers at once may fail with a
		// sharing violation there; Lock keeps writers apart in practice.
		t.Skip("concurrent rename onto one file is not reliable on Windows")
	}
	path := DefaultPath(t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				f := &File{Version: 1}
				for k := 0; k < 50; k++ {
					f.Deployments = append(f.Deployments, DeploymentRecord{ID: fmt.Sprintf("w%d-%d-%d", w, i, k), VMID: "vm"})
				}
				if err := Write(path, f); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Write: %v", err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("state after concurrent writes: %v", err)
	}
	if len(f.Deployments) != 50 {
		t.Fatalf("deployments = %d, want 50 from one writer", len(f.Deployments))
	}
	ents, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range ents {
		if strings.Contains(e.Name(), "tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestLock_SecondHolderIsRefused(t *testing.T) {
	path := DefaultPath(t.TempDir())
	unlock, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err := Lock(path); codeOf(err) != clierr.CodeStateLocked {
		t.Fatalf("second Lock: %v, want %s", err, clierr.CodeStateLocked)
	}
	unlock()
	unlock2, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
	unlock2()
}

// A state file from a newer evo may hold fields this one would drop
// on rewrite: refuse it instead of silently losing records.
func TestRead_NewerVersionRefused(t *testing.T) {
	path := DefaultPath(t.TempDir())
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"version": 2, "deployments": [{"id":"vm-1","vm_id":"vm-1","status":"running"}]}`), 0o644)
	_, err := Read(path)
	if codeOf(err) != clierr.CodeStateCorrupted || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("Read: %v, want STATE_CORRUPTED naming version 2", err)
	}
}

func TestRead_MissingVersionIsOne(t *testing.T) {
	path := DefaultPath(t.TempDir())
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"deployments": []}`), 0o644)
	f, err := Read(path)
	if err != nil || f.Version != 1 {
		t.Fatalf("Read = %+v, %v; want version 1", f, err)
	}
}

// Deleting a broken state forgets VMs that are still billed: the hint must
// say so instead of advising a blind delete.
func TestRead_CorruptHintWarnsAboutRunningVMs(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "truncated": `{"version":1,"deployments":[{"id":"vm-`, "garbage": "\x00\x01"} {
		t.Run(name, func(t *testing.T) {
			path := DefaultPath(t.TempDir())
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, []byte(content), 0o644)
			_, err := Read(path)
			var ce *clierr.CLIError
			if !errors.As(err, &ce) || ce.Code != clierr.CodeStateCorrupted {
				t.Fatalf("Read: %v", err)
			}
			if !strings.Contains(ce.Hint, "console") {
				t.Errorf("hint %q does not send the user to check the VMs first", ce.Hint)
			}
		})
	}
}

func TestWrite_ReadOnlyDirIsStateError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("directory permissions are not enforced here")
	}
	root := t.TempDir()
	path := DefaultPath(root)
	if err := Write(path, &File{Version: 1}); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Dir(path), 0o555)
	defer os.Chmod(filepath.Dir(path), 0o755)
	if err := Write(path, &File{Version: 1}); codeOf(err) != clierr.CodeStateCorrupted {
		t.Fatalf("Write into read-only dir: %v", err)
	}
}
