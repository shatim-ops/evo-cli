// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// runningVMCloud is a world whose VM of the record runs: an update reads
// the VM before it probes port 22 (a stopped VM is refused with the cause).
type runningVMCloud struct{ *cancelCloud }

func (runningVMCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	return &provision.VM{ID: id, Status: "running"}, nil
}

// M2 (axis 4, axis 1): deploy uploads over SSH from this host. Its preview
// names what that needs of this host's network — outbound TCP 22 to the
// VM's public address — for a new VM and an update alike; an update probes
// port 22 of the VM before the question and warns when it does not answer
// (with the check and the likely causes). A warning, not a refusal: the
// codes stay; the update of an unchanged compose file probes nothing.
func TestDeploy_PreviewNamesTheSSHUploadAndProbesPort22(t *testing.T) {
	updateRegistry := func(t *testing.T, dial func(context.Context, string) error, hash string) (*Registry, *[]string) {
		t.Helper()
		dir := t.TempDir()
		compose := writeTestCompose(t)
		sf := &state.File{Version: 1}
		sf.UpsertDeployment(state.DeploymentRecord{ID: "d-1", VMID: "vm-1", VMIP: "203.0.113.7", Status: "running", ComposeHash: hash, ProjectID: "p"})
		if err := state.Write(state.DefaultPath(dir), sf); err != nil {
			t.Fatal(err)
		}
		var dialed []string
		r := New(func(map[string]any) (*engine.Engine, error) {
			var buf bytes.Buffer
			return &engine.Engine{
				Config:   config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose},
				Auth:     staticToken{},
				Cloud:    runningVMCloud{&cancelCloud{cancel: func() {}}},
				Printer:  output.NewWithWriter(&buf, output.ModeText, false),
				StateDir: dir,
				SSHDial: func(ctx context.Context, addr string) error {
					dialed = append(dialed, addr)
					return dial(ctx, addr)
				},
			}, nil
		})
		r.SetStateDir(dir)
		return r, &dialed
	}
	answer := func(t *testing.T, r *Registry, args map[string]any) map[string]any {
		t.Helper()
		text, _ := r.handlers["deploy"](context.Background(), args)
		var a map[string]any
		if err := json.Unmarshal([]byte(text), &a); err != nil {
			t.Fatalf("deploy: %s", text)
		}
		return a
	}

	t.Run("an update whose port 22 does not answer", func(t *testing.T) {
		// The probe has a deadline within the preview's lookups, and the
		// dial meets it: the world has no network, the seam answers.
		r, dialed := updateRegistry(t, func(ctx context.Context, _ string) error {
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("the probe has no deadline")
			}
			return context.DeadlineExceeded
		}, "old")
		a := answer(t, r, map[string]any{})
		if a["update"] != true || a["ssh_reachable"] != false || a["ssh_probe_error"] != "timeout" || !strings.Contains(a["upload"].(string), "TCP 22 to 203.0.113.7") {
			t.Fatalf("answer: %v", a)
		}
		ws, _ := a["warnings"].([]any)
		w := ""
		if len(ws) == 1 {
			w, _ = ws[0].(string)
		}
		if !strings.Contains(w, "nc -vz 203.0.113.7 22") || !strings.Contains(w, "(timeout)") {
			t.Errorf("the warning names no check or class: %v", a["warnings"])
		}
		if a["status"] != "confirmation_required" {
			t.Errorf("a warning, not a refusal: status %v", a["status"])
		}
		if strings.Join(*dialed, ",") != "203.0.113.7:22" {
			t.Errorf("dialed %v, want port 22 of the VM once", *dialed)
		}
	})
	t.Run("an update whose port 22 answers", func(t *testing.T) {
		r, _ := updateRegistry(t, func(context.Context, string) error { return nil }, "old")
		a := answer(t, r, map[string]any{})
		if a["ssh_reachable"] != true || a["warnings"] != nil || a["ssh_probe_error"] != nil {
			t.Errorf("answer: %v", a)
		}
	})
	t.Run("a new VM", func(t *testing.T) {
		r, _ := cancelRegistry(t, &cancelCloud{cancel: func() {}})
		a := answer(t, r, map[string]any{})
		p, _ := json.Marshal(a["preview"])
		if !strings.Contains(string(p), `"key":"upload"`) || !strings.Contains(string(p), "outbound TCP 22") {
			t.Errorf("the preview of a new VM does not name the SSH upload: %s", p)
		}
	})
}
