// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// A failed read is not proof that the delete finished. Check the actual
// disk endpoint and pending record on successive doctor runs.
func TestContract_DoctorRetriesInterruptedDiskDelete(t *testing.T) {
	f := &ownerFake{goneStatus: http.StatusForbidden}
	r := ownerRegistry(t, f, false, ownProject)
	eng, err := r.newEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := intent.Dir(eng.StateDir)
	if _, err := r.beginIntent(context.Background(), "disk_delete", map[string]any{"disk_id": resGone, "confirmed": true}); err != nil {
		t.Fatal(err)
	}
	pending := intent.Pending(dir)
	if len(pending) != 1 {
		t.Fatalf("pending before doctor: %v", pending)
	}
	intent.End(pending[0], history.Outcome{})
	for _, tc := range []struct {
		status, left int
		why          string
	}{{http.StatusForbidden, 1, "could not read"}, {http.StatusNotFound, 0, "the delete took effect"}} {
		f.goneStatus = tc.status
		before := len(f.log())
		text := doctorOnFake(t, r)
		card := false
		for _, q := range f.log()[before:] {
			if q == "GET compute.api.cloud.ru/api/v1/disks/"+resGone {
				card = true
			}
			if !strings.HasPrefix(q, "GET ") || strings.HasSuffix(q, "/api/v1/disks") {
				t.Errorf("unexpected doctor request: %s", q)
			}
		}
		if !card || len(intent.Pending(dir)) != tc.left || !strings.Contains(text, tc.why) {
			t.Errorf("HTTP %d: card read %v, pending %d: %s", tc.status, card, len(intent.Pending(dir)), text)
		}
	}
}
