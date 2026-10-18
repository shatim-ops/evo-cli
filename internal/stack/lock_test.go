// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package stack

import (
	"strings"
	"testing"
	"time"
)

// LockJob checks the id before it builds a path from it, and a held lock
// makes a second LockJob fail with a message an agent can act on.
func TestLockJob(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if _, err := LockJob("../../etc/passwd", time.Millisecond); err == nil || !strings.Contains(err.Error(), "invalid job_id") {
		t.Fatalf("bad id: %v", err)
	}
	unlock, err := LockJob("stk-0123456789", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockJob("stk-0123456789", 50*time.Millisecond); err == nil || !strings.Contains(err.Error(), "another evo process") {
		t.Errorf("held job lock: %v", err)
	}
	unlock()
	unlock2, err := LockJob("stk-0123456789", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	unlock2()
}
