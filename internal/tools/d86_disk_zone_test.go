// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D86 (agent run of 1.7.0, №13; owner's decision 05.11): disk_create with
// no zone — none in the call, none in the configuration, no attach_to —
// showed a preview with an empty zone and went to the cloud without one.
// As provision without a region (D15): CONFIG_INVALID, code 2, missing
// ["zone"], before the preview and the question; nothing is written.
func TestDiskCreate_NoZoneRefusedBeforeTheQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, c := agentDiskWorld(t, map[string]any{})
		m, text, isErr := callNamed(t, r, "disk_create", map[string]any{"name": "data", "size_gb": float64(20), "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeConfigInvalid || m["status"] == "confirmation_required" {
			t.Errorf("confirmed=%v: want CONFIG_INVALID before the question: %s", confirmed, text)
		}
		if missing, _ := m["missing"].([]any); len(missing) != 1 || missing[0] != "zone" {
			t.Errorf("confirmed=%v: missing %v, want [zone]: %s", confirmed, m["missing"], text)
		}
		if hint, _ := m["hint"].(string); !strings.Contains(hint, "zone") {
			t.Errorf("confirmed=%v: the hint does not name zone: %s", confirmed, text)
		}
		if len(c.writes) != 0 {
			t.Errorf("confirmed=%v: written %v", confirmed, c.writes)
		}
	}
}

func (c *agentDiskCloud) CreateDisk(context.Context, provision.CreateDiskReq) (map[string]any, error) {
	c.wrote("create")
	return map[string]any{"id": faultDisk}, nil
}
