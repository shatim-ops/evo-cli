// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

// The world «the record is there, the VM is stopped» (release plan 1.9,
// «deploy, verify, logs по записи с остановленной ВМ называют причину и
// vm_start»; condition У2 of the conclusion of «Риск» to vm_stop): vm_stop
// makes a state evo did not have — a deployment whose VM does not run.
// deploy, verify and logs read the VM before they go to it and say why
// they do not: the VM is stopped, and the way on is vm_start — not SSH
// that times out, and not «delete it and deploy again», which loses the
// data on the VM's disks.

// reached counts what went to the VM itself: SSH and the probe of its
// ports.
type reached struct {
	deploy.Deployer
	mu      sync.Mutex
	connect int
	probes  int
}

func (r *reached) Connect(ctx context.Context, _ string, _ int, _ string, _ []byte, _ string) error {
	r.mu.Lock()
	r.connect++
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("ssh: connection timed out")
}

func (r *reached) Close() error { return nil }

func (r *reached) probe(context.Context, string, []int, time.Duration) []verify.ProbeResult {
	r.mu.Lock()
	r.probes++
	r.mu.Unlock()
	return []verify.ProbeResult{{Port: 80, Healthy: false}}
}

func (r *reached) counts() (connect, probes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connect, r.probes
}

// unreadableVMs is the cloud that does not answer a read of a VM.
type unreadableVMs struct{ *faultCloud }

var errVMRead = &provision.StatusError{Status: 503, Body: "unavailable"}

func (unreadableVMs) GetVM(context.Context, string) (*provision.VM, error) { return nil, errVMRead }
func (unreadableVMs) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return nil, errVMRead
}
func (unreadableVMs) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, errVMRead
}

// stoppedVMWorld is a directory with the record of a finished deployment
// whose VM the cloud has in vmState; unreadable — the cloud does not
// answer a read of it.
func stoppedVMWorld(t *testing.T, vmState string, unreadable bool) (*Registry, func() *engine.Engine, *faultCloud, *reached) {
	t.Helper()
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	vm, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	cloud.setVMState(vm.ID, vmState)
	dir := t.TempDir()
	compose := writeTestCompose(t)
	writeRecords(t, dir, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: "web", VMIP: "203.0.113.9",
		SSHUser: "user1", Status: "running", ComposePath: compose, ExposedPorts: []int{80}})
	seen := &reached{}
	var api provision.CloudAPI = cloud
	if unreadable {
		api = unreadableVMs{cloud}
	}
	mk := func() *engine.Engine {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     api,
			Deployer:  seen,
			Probe:     seen.probe,
			Printer:   output.NewWithWriter(&buf, output.ModePlain, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   faultToolsTimings(),
		}
	}
	return New(func(map[string]any) (*engine.Engine, error) { return mk(), nil }), mk, cloud, seen
}

// stoppedVMCalls are the three ways to a deployment: each returns the
// text of its answer (an error's text and hint for the engine's verify,
// which is a command of the CLI and no tool) and whether it failed.
var stoppedVMCalls = map[string]func(ctx context.Context, r *Registry, mk func() *engine.Engine) (string, bool){
	"logs": func(ctx context.Context, r *Registry, _ func() *engine.Engine) (string, bool) {
		return r.handlers["logs"](ctx, map[string]any{})
	},
	"deploy": func(ctx context.Context, r *Registry, _ func() *engine.Engine) (string, bool) {
		return r.handlers["deploy"](ctx, map[string]any{"confirmed": true})
	},
	"verify": func(ctx context.Context, _ *Registry, mk func() *engine.Engine) (string, bool) {
		err := mk().Verify(ctx)
		if err == nil {
			return "", false
		}
		text := err.Error()
		var ce *clerrors.CLIError
		if errors.As(err, &ce) {
			text += "\n" + ce.Hint
			if tool, _ := ce.Details["next_tool"].(string); tool != "" {
				text += "\nnext_tool: " + tool
			}
		}
		return text, true
	},
}

func TestStoppedVM_DeployVerifyLogsNameTheCauseAndVMStart(t *testing.T) {
	for name, call := range stoppedVMCalls {
		t.Run(name, func(t *testing.T) {
			r, mk, cloud, seen := stoppedVMWorld(t, "stopped", false)
			before := len(cloud.callLog())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			text, isErr := call(ctx, r, mk)
			if !isErr {
				t.Fatalf("%s of a deployment whose VM is stopped is not a refusal: %s", name, text)
			}
			if !strings.Contains(text, "stopped") && !strings.Contains(text, "остановлен") {
				t.Errorf("%s does not say the VM is stopped: %s", name, text)
			}
			if !strings.Contains(text, "vm_start") && !strings.Contains(text, "vm start") {
				t.Errorf("%s does not name vm_start as the way on: %s", name, text)
			}
			if strings.Contains(text, "destroy") {
				t.Errorf("%s of a stopped VM names destroy: the data on its disks would be lost: %s", name, text)
			}
			if connect, probes := seen.counts(); connect+probes != 0 {
				t.Errorf("%s went to a stopped VM: %d SSH connects, %d probes: %s", name, connect, probes, text)
			}
			for _, op := range cloud.callLog()[before:] {
				if !strings.HasPrefix(op, "Get") && !strings.HasPrefix(op, "List") && !strings.HasPrefix(op, "Find") && op != "SetToken" {
					t.Errorf("%s of a stopped VM sent %s: the refusal writes nothing", name, op)
				}
			}
		})
	}
}

// A read of the VM that fails is not «the VM is stopped»: the call goes
// the way it went before — to the VM (the plan: «сбой чтения состояния —
// прежний путь»), and names no vm_start.
func TestStoppedVM_UnreadableStateGoesTheWayItWent(t *testing.T) {
	for name, call := range stoppedVMCalls {
		t.Run(name, func(t *testing.T) {
			r, mk, _, seen := stoppedVMWorld(t, "stopped", true)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			text, _ := call(ctx, r, mk)
			if strings.Contains(text, "vm_start") {
				t.Errorf("%s names vm_start for a VM whose state it could not read: %s", name, text)
			}
			if name == "deploy" {
				return // deploy reads the VM on its way on its own; what it does then is its tests'
			}
			if connect, probes := seen.counts(); connect+probes == 0 {
				t.Errorf("%s did not go to the VM though its state was not read: %s", name, text)
			}
		})
	}
}

// A VM that runs: nothing of the above — the call goes to the VM.
func TestStoppedVM_RunningVMIsReached(t *testing.T) {
	for _, name := range []string{"logs", "verify"} {
		r, mk, _, seen := stoppedVMWorld(t, "running", false)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		text, _ := stoppedVMCalls[name](ctx, r, mk)
		cancel()
		if connect, probes := seen.counts(); connect+probes == 0 || strings.Contains(text, "vm_start") {
			t.Errorf("%s of a deployment whose VM runs: %d connects, %d probes: %s", name, connect, probes, text)
		}
	}
}
