// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Agent run of 1.7.0 (m0, m1, m2; findings 2.1, №3): destroy without a
// target in a folder with provision's VM and deploy's showed a preview
// with no target and asked, and after yes refused (several deployments):
// a call bound to fail asked for consent (§4). It refuses before the
// question, with the same answer as after it — CONFIG_INVALID, both VMs
// named, the way by vm_id (class D35).
func TestDestroy_SeveralRecordsRefusedBeforeTheQuestion(t *testing.T) {
	r, dir := fastRegistry(t, &targetCloud{}, config.Config{ProjectID: "p"})
	now := time.Now()
	writeRecords(t, dir,
		state.DeploymentRecord{ID: "0000a1a1-0000-4000-8000-0000000000d1", VMID: "0000a1a1-0000-4000-8000-0000000000d1", VMName: "vm-a",
			Status: "running", Source: state.SourceProvision, CreatedAt: now},
		state.DeploymentRecord{ID: "0000a1a1-0000-4000-8000-0000000000d2", VMID: "0000a1a1-0000-4000-8000-0000000000d2", VMName: "evo-app",
			Status: "running", ComposePath: "/srv/app/docker-compose.yml", CreatedAt: now})
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "destroy", map[string]any{"confirmed": confirmed})
		if !isErr || m["status"] == "confirmation_required" || m["reason"] != clerrors.CodeConfigInvalid {
			t.Errorf("confirmed=%v: want CONFIG_INVALID before the question, as after it: %s", confirmed, text)
		}
		if !strings.Contains(text, "vm-a") || !strings.Contains(text, "evo-app") {
			t.Errorf("confirmed=%v: the refusal does not name both VMs: %s", confirmed, text)
		}
	}
}
