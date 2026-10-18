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
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// pollCheckCloud records whether the new VM is in the state while
// provision waits for it.
type pollCheckCloud struct {
	cancelCloud
	dir       string
	seenState bool
	polls     int
}

func (c *pollCheckCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	return &provision.VM{ID: "vm-made"}, nil
}
func (c *pollCheckCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.polls++
	if sf, err := state.Read(state.DefaultPath(c.dir)); err == nil {
		for _, d := range sf.Deployments {
			if d.VMID == "vm-made" {
				c.seenState = true
			}
		}
	}
	if c.polls >= 2 {
		return &provision.VM{ID: "vm-made", Status: "active"}, nil
	}
	return &provision.VM{ID: "vm-made", Status: "creating"}, nil
}
func (c *pollCheckCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "in_use"}, nil
}

// provision has no dry_run: the schema does not declare it and the CLI
// refuses it, so a confirmed call with it creates the VM like any other.
// (A hidden dry_run once returned the request body after the gate.)
func TestProvision_NoUndeclaredDryRun(t *testing.T) {
	cloud := &pollCheckCloud{}
	dir := t.TempDir()
	cloud.dir = dir
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
			Timings:   engine.Timings{VMPollInterval: time.Millisecond, FIPPollInterval: time.Millisecond, FIPRetryInterval: time.Millisecond},
		}, nil
	})
	tl, _ := r.Lookup("provision")
	if _, ok := tl.InputSchema["properties"].(map[string]any)["dry_run"]; ok {
		t.Fatal("dry_run is declared now: drop this test, test the declared behaviour")
	}
	// An undeclared argument is refused before the cloud (release QA,
	// D-mcp 4), not silently taken or ignored.
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true, "dry_run": true})
	if !isErr || !strings.Contains(text, "USAGE") || !strings.Contains(text, "dry_run") || cloud.creates != 0 {
		t.Fatalf("creates = %d, answer %s: an undeclared argument must be refused", cloud.creates, text)
	}
}

// R5: provision records the VM right after creating it, as the CLI deploy:
// a hard stop during the wait left the VM and its FIP without a record.
func TestQAr3_Provision_VMRecordedBeforeWait(t *testing.T) {
	cloud := &pollCheckCloud{}
	dir := t.TempDir()
	cloud.dir = dir
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
			Timings:   engine.Timings{VMPollInterval: time.Millisecond, FIPPollInterval: time.Millisecond, FIPRetryInterval: time.Millisecond},
		}, nil
	})
	_, _ = r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if cloud.polls == 0 {
		t.Fatal("provision never waited for the VM")
	}
	if !cloud.seenState {
		t.Fatal("the VM was not in the state while provision waited for it")
	}
}
