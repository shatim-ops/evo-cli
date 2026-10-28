// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/journal"
)

// D46: the journal records a call and never changes it — its answer, and
// when the answer comes too. Another process holding journal.lock (a CLI
// call beside evo mcp, an export) loses the record of this call at most;
// the call does not wait out the lock before it answers (audit of 1.4.0,
// hypothesis: an MCP answer delayed up to two seconds).
func TestJournal_HeldLockDoesNotDelayTheAnswer(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no file locking on this platform")
	}
	setJournal(t, journalOnMode)
	f, err := os.OpenFile(filepath.Join(filepath.Dir(journal.Path()), "journal.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := config.LockFile(f); err != nil {
		t.Fatalf("hold the lock: %v", err)
	}
	defer func() { _ = config.UnlockFile(f) }()

	tl, _ := newTestServer().Lookup("vm_list")
	start := time.Now()
	tl.Handler(context.Background(), map[string]any{})
	// The journal waits up to two seconds (lockWait) for the lock: an
	// answer that waits for it takes at least that, one that does not,
	// milliseconds.
	// wall-clock: half of lockWait (2 s) tells them apart.
	if d := time.Since(start); d > time.Second {
		t.Errorf("vm_list answered after %s while another process held journal.lock: the call waited for the journal", d.Round(time.Millisecond))
	}
}
