// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Agent run of 1.7.0 (m0, m1, m2; findings 2.1, №3): in a folder with
// provision's VM and deploy's deployment, logs, verify and status without
// a target took the first record — provision's VM, which runs no compose
// file (D62) — while deploy works on its own. They take deploy's
// deployment, as deploy does (ActiveComposeDeployment); a folder with
// provision's VM alone keeps it.
func TestNoTargetCommands_TakeDeploysDeploymentNotProvisionsVM(t *testing.T) {
	now := time.Now()
	vm := state.DeploymentRecord{ID: "vm-prov", VMID: "vm-prov", VMIP: "192.0.2.1", VMName: "vm-a", SSHUser: "user1",
		Status: "running", Source: state.SourceProvision, CreatedAt: now}
	dep := state.DeploymentRecord{ID: "vm-deploy", VMID: "vm-deploy", VMIP: "192.0.2.2", VMName: "evo-app", SSHUser: "user1",
		Status: "deploying", ComposePath: "/srv/app/docker-compose.yml", CreatedAt: now}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, newMockDeployer())
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(vm)
	sf.UpsertDeployment(dep)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"logs":   eng.Logs(context.Background(), LogsOptions{Tail: 10}),
		"verify": eng.Verify(context.Background()),
	} {
		// deploy's deployment is unfinished (D76); provision's VM is not.
		if err == nil || !strings.Contains(err.Error(), dep.ID) {
			t.Errorf("%s without a target: %v, want deploy's deployment %s", name, err, dep.ID)
		}
	}
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModeJSON, false)
	if err := eng.Status(context.Background(), StatusOptions{NoRefresh: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"vm_id": "`+dep.VMID+`"`) {
		t.Errorf("status without a target does not show deploy's deployment first: %s", out.String())
	}
	if others := OtherActiveVMs(sf); len(others) != 1 || others[0] != vm.VMID {
		t.Errorf("other_active_vm_ids %v, want provision's VM %s alone", others, vm.VMID)
	}
}
