// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// provision's CreateVM returns success with an empty id after the VM is in
// the world. The answer and the state must name that VM, and a second call
// must not create another.
func TestBreak_Provision_EmptyVMIDOrphansVM(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	cloud.failNth("CreateVM", 1, faultEmpty)
	text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v text=%s", cloud.liveVMs(), text)
	}
	id := cloud.liveVMs()[0].ID
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	blob := text
	if sf.ActiveDeployment() != nil {
		blob += "\n" + sf.ActiveDeployment().VMID
	}
	if !strings.Contains(blob, id) {
		t.Fatalf("VM %s is billed but the answer and state do not name it\n%s\nstate=%+v", id, text, sf.Deployments)
	}
	vms, _ := cloud.creates()
	cloud.disarm()
	text2, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if v2, _ := cloud.creates(); v2 != vms {
		t.Fatalf("retry created another VM (%d→%d)\n%s", vms, v2, text2)
	}
}
