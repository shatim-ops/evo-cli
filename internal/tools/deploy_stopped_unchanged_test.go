// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The usual world after vm_stop: the deployment finished with this very
// compose file, and its VM is stopped. deploy used to answer «the same
// file is already running: nothing to do» — the check «unchanged» stood
// before the read of the VM — while the service was down (the interim
// delta audit of 1.9, FB19I-1). The «Миграция» of D104 promises the
// refusal with and without consent: the record with the hash of the file
// is refused as the changed one is.
func TestDeploy_AStoppedVMIsRefusedWhenTheFileIsUnchangedToo(t *testing.T) {
	for _, vmState := range []string{"stopped", "stopping"} {
		for _, confirmed := range []bool{false, true} {
			r, c, id := stoppedDeployment(t, vmState)
			dir := r.stateDirOf
			p, err := compose.Parse(filepath.Join(dir, "docker-compose.yml"))
			if err != nil {
				t.Fatal(err)
			}
			sf, err := state.Read(state.DefaultPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			sf.Deployments[0].ComposeHash, sf.Deployments[0].DeployedHash = p.Hash, p.Hash
			if err := state.Write(state.DefaultPath(dir), sf); err != nil {
				t.Fatal(err)
			}
			writes := cloudWrites(c)
			m, text, isErr := callNamed(t, r, "deploy", map[string]any{"confirmed": confirmed})
			if !isErr || m["reason"] != "SSH_TIMEOUT" || m["cause"] != "vm_stopped" || m["vm_state"] != vmState {
				t.Fatalf("deploy of the same file to a %s VM, confirmed=%v: %s, want the refusal with cause vm_stopped", vmState, confirmed, text)
			}
			next, _ := m["next_args"].(map[string]any)
			if m["next_tool"] != "vm_start" || next["vm_id"] != id || next["confirmed"] != nil {
				t.Errorf("the refusal does not lead to vm_start of %s without consent: %s", id, text)
			}
			if got := cloudWrites(c); got != writes {
				t.Errorf("deploy wrote to the cloud (%d → %d)", writes, got)
			}
		}
	}
	// A running VM with the same file: «nothing to do», as it was.
	r, _, _ := stoppedDeployment(t, "running")
	dir := r.stateDirOf
	p, _ := compose.Parse(filepath.Join(dir, "docker-compose.yml"))
	sf, _ := state.Read(state.DefaultPath(dir))
	sf.Deployments[0].ComposeHash, sf.Deployments[0].DeployedHash = p.Hash, p.Hash
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if m, text, _ := callNamed(t, r, "deploy", map[string]any{}); m["unchanged"] != true {
		t.Errorf("deploy of the same file to a running VM: %s, want unchanged as before", text)
	}
}
