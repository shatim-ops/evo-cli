// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
)

// A held record is closed only by an answer about the same action
// (release plan 1.9, «Удержанная запись закрывается чужим ответом с той
// же целью»; the interim delta audit of 13.11, high): a restore of a
// backup into disk-a that the platform took, whose disk is not listed yet,
// is held — the call answers «restoring» and sends nothing. A restore of
// the same backup into disk-b names the same target (backup_id) and
// another thing to make: it must not close the record of disk-a, or the
// call for disk-a gets a plain question and, with the consent, a second
// request for disk-a. The worlds of a held record had one call per target.
func TestHeld_AnotherNameOfTheSameTargetDoesNotCloseTheRecord(t *testing.T) {
	w := (&restoreWorld{never: true}).stateIn(t)
	a := map[string]any{"name": "disk-a", "confirmed": true}
	if m, text, _ := backupRestore(t, w, a); m["status"] != "restoring" || w.restores != 1 {
		t.Fatalf("the restore into disk-a, taken and not listed: want restoring after one request (sent %d): the world of the test changed\n%s", w.restores, text)
	}
	// The same backup into another name: taken, and its disk is listed.
	w.mu.Lock()
	w.never = false
	w.mu.Unlock()
	if m, text, _ := backupRestore(t, w, map[string]any{"name": "disk-b", "confirmed": true}); m["status"] != "created" || w.restores != 2 {
		t.Fatalf("the restore into disk-b: want created after the second request (sent %d): the world of the test changed\n%s", w.restores, text)
	}
	// Back to disk-a: its disk is still not listed, its restore still goes
	// on. The call says so and sends nothing — asked or with the consent.
	for _, confirmed := range []bool{false, true} {
		m, text, _ := backupRestore(t, w, map[string]any{"name": "disk-a", "confirmed": confirmed})
		if m["status"] != "restoring" {
			t.Errorf("disk-a again, confirmed=%v: status %v, want restoring — the answer about disk-b closed the held record of disk-a\n%s", confirmed, m["status"], text)
		}
		if w.restores != 2 {
			t.Errorf("disk-a again, confirmed=%v: %d restore requests, want 2 — a second request for disk-a was sent\n%s", confirmed, w.restores, text)
		}
	}
}
