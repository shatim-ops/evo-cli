// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The world vm_stop makes: a deployment recorded in the directory, its VM
// stopped. logs and deploy over MCP name the cause and vm_start as the
// next step — with and without consent, before any write — and never a
// delete («Риск», conclusion 132, У2).
func stoppedDeployment(t *testing.T, vmState string) (*Registry, *faultCloud, string) {
	t.Helper()
	c := &faultCloud{}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, vmState)
	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRecords(t, dir, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: "web", VMIP: "203.0.113.9",
		ProjectID: "p", SSHUser: "user1", Status: "running", ComposePath: compose, ComposeHash: "another"})
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:   staticToken{}, Cloud: c,
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
			// A dial of port 22 is a test's failure: nothing goes to a
			// stopped VM.
			SSHDial: func(context.Context, string) error {
				if vmState != "running" {
					t.Errorf("port 22 of a %s VM was dialed", vmState)
				}
				return nil
			},
		}, nil
	})
	r.SetStateDir(dir)
	return r, c, vm.ID
}

// cloudWrites is the number of calls of the world that are not reads.
func cloudWrites(c *faultCloud) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, op := range c.log {
		if !strings.HasPrefix(op, "Get") && !strings.HasPrefix(op, "List") {
			n++
		}
	}
	return n
}

func TestLogsDeploy_AStoppedVMNamesTheCauseAndVMStart(t *testing.T) {
	for _, vmState := range []string{"stopped", "stopping"} {
		for _, call := range []struct {
			tool, reason string
			args         map[string]any
		}{
			{"logs", "SSH_TIMEOUT", map[string]any{}},
			{"deploy", "SSH_TIMEOUT", map[string]any{}},
			{"deploy", "SSH_TIMEOUT", map[string]any{"confirmed": true}},
		} {
			r, c, id := stoppedDeployment(t, vmState)
			writes := cloudWrites(c)
			m, text, isErr := callNamed(t, r, call.tool, call.args)
			if !isErr || m["status"] != "error" || m["reason"] != call.reason {
				t.Fatalf("%s %v of a deployment whose VM is %s: %s, want an error with reason %s", call.tool, call.args, vmState, text, call.reason)
			}
			next, _ := m["next_args"].(map[string]any)
			if m["cause"] != "vm_stopped" || m["vm_state"] != vmState || m["next_tool"] != "vm_start" || next["vm_id"] != id || next["confirmed"] != nil {
				t.Errorf("%s: %s, want cause vm_stopped, vm_state %s and next_tool vm_start of %s without consent", call.tool, text, vmState, id)
			}
			hint, _ := m["hint"].(string)
			for _, bad := range []string{"destroy", "vm delete", "deploy again"} {
				if strings.Contains(hint, bad) {
					t.Errorf("%s: the hint advises %q: %s", call.tool, bad, hint)
				}
			}
			if got := cloudWrites(c); got != writes {
				t.Errorf("%s %v wrote to the cloud (%d → %d)", call.tool, call.args, writes, got)
			}
		}
	}
}

// A running VM is the path as it was: deploy asks its question.
func TestDeploy_ARunningVMAsksAsBefore(t *testing.T) {
	r, _, _ := stoppedDeployment(t, "running")
	m, text, _ := callNamed(t, r, "deploy", map[string]any{})
	if m["status"] != "confirmation_required" || m["update"] != true {
		t.Fatalf("deploy to a running VM: %s, want the question of an update", text)
	}
}

// Every state a power tool takes a VM from, through or to — but running —
// is one logs, verify and deploy say something of beyond the failure of
// SSH (engine.VMStateCovered): vm_reboot came after D104 and its
// rebooting was left out (the interim delta audit of 1.9, FB19I-5). A new
// power action with a state of its own fails here until it is covered.
func TestPowerStates_EveryStateOffRunningIsCoveredByLogsAndVerify(t *testing.T) {
	for tool, act := range vmPowerActions {
		for _, st := range []string{act.from, act.via, act.to} {
			if st != "" && st != "running" && !engine.VMStateCovered(st) {
				t.Errorf("%s goes through %q: logs and verify name no cause for it (engine.vmStoppedStates / vmTransitStates)", tool, st)
			}
		}
	}
}

// sshTimesOut is a deployer whose SSH does not answer, as a VM on its way
// up does.
type sshTimesOut struct{ deploy.Deployer }

func (sshTimesOut) Connect(context.Context, string, int, string, []byte, string) error {
	return clierrors.New(clierrors.CodeSSHTimeout, "ssh: connection timed out", "")
}

// The cause of a failure on a VM on its way up reaches the MCP answer of
// logs as the CLI's --json error has it, with the reason logs had (the
// interim delta audit of 1.9, FB19I-5).
func TestLogs_AFailureOnAVMOnItsWayUpNamesTheStateOverMCP(t *testing.T) {
	for _, vmState := range []string{"starting", "rebooting"} {
		r, _, id := stoppedDeployment(t, vmState)
		newEngine := r.newEngine
		r.newEngine = func(a map[string]any) (*engine.Engine, error) {
			e, err := newEngine(a)
			if e != nil {
				e.Deployer = sshTimesOut{}
			}
			return e, err
		}
		m, text, isErr := callNamed(t, r, "logs", map[string]any{})
		next, _ := m["next_args"].(map[string]any)
		if !isErr || m["reason"] != "SSH_TIMEOUT" || m["cause"] != "vm_in_transition" || m["vm_state"] != vmState ||
			m["next_tool"] != "vm_get" || next["vm_id"] != id {
			t.Errorf("logs of a deployment whose VM is %s, SSH timing out: %s, want SSH_TIMEOUT with cause vm_in_transition, vm_state %s and vm_get of %s", vmState, text, vmState, id)
		}
	}
}
