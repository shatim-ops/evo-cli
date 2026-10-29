// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D59 (final Grok run on 1.5.0 rc.1): disk_delete of a disk attached to a
// VM asked for consent, and the platform refused the delete after it (422
// «Primary disk can not be deleted without vm», API_ERROR, exit 5). The
// disk's VMs are in the GetDisk answer planDiskDelete reads already (D26):
// the call is refused before the question, USAGE, with the way out — a
// boot disk goes with its VM (destroy), a data disk is detached first
// (disk_detach). Nothing is deleted.
func TestDiskDelete_AttachedRefusedBeforeTheQuestion(t *testing.T) {
	for _, c := range []struct {
		label, vms string
		want       []string
	}{
		{"boot disk", `[{"id":"` + liveID + `","name":"web","primary":true,"state":"running"}]`, []string{"web", "destroy"}},
		{"data disk", `[{"id":"` + liveID + `","name":"web","primary":false,"state":"running"}]`, []string{"web", "disk_detach"}},
	} {
		for _, confirmed := range []bool{false, true} {
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/v1/disks/") {
					return false
				}
				_, _ = io.WriteString(w, `{"id":"`+liveID+`","name":"data","state":"in_use","bootable":true,"vms":`+c.vms+`}`)
				return true
			}}
			r := apiFakeRegistry(t, f)
			m, text, isErr := callNamed(t, r, "disk_delete", map[string]any{"disk_id": liveID, "confirmed": confirmed})
			hint, _ := m["hint"].(string)
			if !isErr || m["reason"] != clerrors.CodeUsage || m["status"] == "confirmation_required" {
				t.Errorf("%s (confirmed=%v): want USAGE before the question: %s", c.label, confirmed, text)
			}
			for _, w := range c.want {
				if !strings.Contains(text, w) {
					t.Errorf("%s: the answer does not name %q: %s (hint %q)", c.label, w, text, hint)
				}
			}
			for _, call := range f.calls {
				if strings.HasPrefix(call, http.MethodDelete) {
					t.Errorf("%s: %s", c.label, call)
				}
			}
		}
	}
}
