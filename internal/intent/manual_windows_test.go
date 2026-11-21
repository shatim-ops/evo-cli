// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"path/filepath"
	"testing"
)

// Watch registration precedes preview and owns no persistent stage. Any
// notification latches: even if a burst of changes is coalesced, the operator
// must review again. There is no finite event buffer to drain or overflow.
func TestManualDirectoryWatchInvalidatesPreview(t *testing.T) {
	_, _, load := manualFixture(t)
	snap, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if snap.source.unchanged() != nil {
		t.Fatal("unstable initial snapshot")
	}
	sibling := filepath.Join(snap.Record.Dir, "another.tmp")
	for range 32 {
		if err = os.WriteFile(sibling, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(sibling); err != nil {
			t.Fatal(err)
		}
	}
	if err = snap.Apply(load); err == nil {
		t.Fatal("accepted a signalled snapshot")
	}
	if err = snap.source.unchanged(); err == nil {
		t.Fatal("notification reset")
	}
	marks, err := filepath.Glob(filepath.Join(snap.audit, "decision-*"))
	if err != nil || len(marks) != 0 {
		t.Fatal(marks, err)
	}
	fresh, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err = fresh.Apply(load); err != nil {
		t.Fatal(err)
	}
}
func TestManualDirectoryWatchFailsClosed(t *testing.T) {
	if s, _, err := readManualSource(filepath.Join(t.TempDir(), "missing", "receipt.json")); err == nil {
		s.Close()
		t.Fatal("missing directory accepted")
	}
	var s manualSource
	if err := s.unchanged(); err == nil {
		t.Fatal("invalid watch accepted")
	}
}
