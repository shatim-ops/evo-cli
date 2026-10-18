// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func breakIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
}

// breakTwoCloud is a provision cloud that records the VM and then drops the
// caller's context, plus the DeleteVM/GetVM destroy needs.
type breakTwoCloud struct {
	cancelCloud
	deletedVMs []string
}

func (c *breakTwoCloud) DeleteVM(_ context.Context, id string) error {
	c.deletedVMs = append(c.deletedVMs, id)
	return nil
}

func (c *breakTwoCloud) GetVM(context.Context, string) (*provision.VM, error) {
	return nil, clierrors.New(clierrors.CodeVMNotFound, "gone", "")
}

// provision in a directory that already has a running deploy appends a
// second active record. status and destroy without vm_id look at
// ActiveDeployment (the first). destroy then reports success and leaves
// the VM provision just created billed.
func TestBreak_State_SecondActiveSurvivesDestroy(t *testing.T) {
	breakIsolate(t)
	const (
		vmOld  = "00000000-0000-4000-8000-000000000021"
		fipOld = "00000000-0000-4000-8000-0000000000f2"
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &breakTwoCloud{cancelCloud: cancelCloud{cancel: cancel}}
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings: engine.Timings{
				VMPollInterval: time.Millisecond, VMPollTimeout: 30 * time.Millisecond,
				DestroyPollInterval: time.Millisecond, DestroyPollTimeout: 30 * time.Millisecond,
				DeletableWaitTimeout: 30 * time.Millisecond, DeletablePollInterval: time.Millisecond,
				FIPRetryInterval: time.Millisecond,
			},
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})

	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{
		ID: vmOld, VMID: vmOld, VMIP: "203.0.113.21", FloatingIPID: fipOld,
		ProjectID: "p", Status: "running",
	})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}

	text, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	if !strings.Contains(text, "vm-made") {
		t.Fatalf("provision did not record a second VM (isErr=%v): %s", isErr, text)
	}
	got, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Deployments) < 2 || got.ActiveDeployment().VMID != vmOld {
		t.Fatalf("state after provision = %+v", got.Deployments)
	}

	text, isErr = r.handlers["destroy"](context.Background(), map[string]any{"confirmed": true})
	left, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	still := left.ActiveDeployment()
	// One directory has one deployment (engine.Deploy). destroy of that
	// deployment must not answer ok while another active VM is still billed.
	if !isErr && still != nil {
		t.Fatalf("destroy answered ok and left VM %s active (deleted %v); state=%+v text=%s",
			still.VMID, cloud.deletedVMs, left.Deployments, text)
	}
}
