// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D85 (agent run of 1.7.0, №14; owner's decision 05.11): disk_attach with a
// disk's name for disk_id went to the cloud and answered a raw 422
// uuid_parsing, API_ERROR, code 5 — even with --dry-run. The schema says
// disk_id is a UUID (D36), as disk_delete's: a name is USAGE, code 1, before
// the cloud and the question, in disk_attach, disk_detach and disk_resize.
// vm still takes a name (resolveVM; «Риск», R-40).
func TestDiskTools_NonUUIDDiskIdIsUsageBeforeTheCloud(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"disk_attach": {"disk_id": "data", "vm": "web"},
		"disk_detach": {"disk_id": "data", "vm": "web"},
		"disk_resize": {"disk_id": "data", "size_gb": float64(40)},
	} {
		for _, confirmed := range []bool{false, true} {
			r, c := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "available"})
			a := withConfirmed(args, confirmed)
			m, text, isErr := callNamed(t, r, tool, a)
			if !isErr || m["reason"] != clerrors.CodeUsage {
				t.Errorf("%s confirmed=%v: want USAGE before the cloud: %s", tool, confirmed, text)
			}
			if len(c.writes) != 0 {
				t.Errorf("%s confirmed=%v: written %v", tool, confirmed, c.writes)
			}
		}
	}
	// The VM by its name, as before.
	r, _ := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "available"})
	if m, text, _ := callNamed(t, r, "disk_attach", map[string]any{"disk_id": faultDisk, "vm": "web"}); m["status"] != "confirmation_required" {
		t.Errorf("disk_attach with the VM by its name: %s", text)
	}
}
