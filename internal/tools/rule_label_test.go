// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

const labelVMID = "0000a1a1-0000-4000-8000-000000000001"

// labelCloud is a project whose group holds a rule folder A's evo opened
// for the VM (labelled with its id), a rule of another VM and one of the
// people; the VM is listed until it is deleted, or gone from the start.
type labelCloud struct {
	targetCloud
	listed  bool
	rules   []*provision.SGRule
	deleted []string
}

func (c *labelCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	if !c.listed || c.vmGone {
		return nil, nil
	}
	return []*provision.VM{{ID: labelVMID, Name: "web", Status: "running"}}, nil
}
func (c *labelCloud) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	if !c.listed {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	return c.targetCloud.GetVM(ctx, id)
}
func (*labelCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "default"}}, nil
}
func (c *labelCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return c.rules, nil
}
func (c *labelCloud) DeleteSecurityGroupRule(_ context.Context, _, id string) error {
	c.deleted = append(c.deleted, id)
	return nil
}
func (*labelCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}

func newLabelCloud(listed bool) *labelCloud {
	return &labelCloud{listed: listed, targetCloud: targetCloud{vmStatus: "running"}, rules: []*provision.SGRule{
		{ID: "rule-a", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", Description: sgrules.RuleLabel("8080", labelVMID)},
		{ID: "rule-other", Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", Description: sgrules.RuleLabel("80", "0000b2b2-0000-4000-8000-000000000002")},
		{ID: "rule-people", Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", Description: "office ssh"},
	}}
}

// folderB is a registry in a folder whose state records nothing.
func folderB(t *testing.T, cloud provision.CloudAPI) *Registry {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "p", KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings:  engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
		}, nil
	})
}

// P6 (G2-2): a port rule opened for a VM from folder A is closed by the
// VM's delete from folder B — found by the label with the VM's id, named
// in the preview before the question, deleted once the VM is gone. Only
// that VM's rule: another VM's and the people's stay. A VM already gone
// with such a rule left is no longer "nothing to do": its delete closes
// the rule; with none left, it is already_deleted as before (D26).
func TestDestroyTargeted_ClosesTheVMsLabelledRulesFromAnotherFolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listed bool
	}{{"the VM deleted by this call", true}, {"the VM already gone", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newLabelCloud(tc.listed)
			r := folderB(t, cloud)
			preview, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID})
			if !strings.Contains(preview, "rule-a") || strings.Contains(preview, "rule-other") || strings.Contains(preview, "rule-people") {
				t.Fatalf("the preview does not name exactly the VM's rule:\n%s", preview)
			}
			if len(cloud.deleted) > 0 {
				t.Fatalf("the preview deleted %v", cloud.deleted)
			}
			text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID, "confirmed": true})
			if isErr {
				t.Fatalf("destroy: %s", text)
			}
			if strings.Join(cloud.deleted, ",") != "rule-a" {
				t.Errorf("deleted rules %v, want rule-a only", cloud.deleted)
			}
			var a struct {
				Status string `json:"status"`
				Rules  struct {
					Closed []string `json:"closed"`
				} `json:"security_group_rules"`
			}
			if json.Unmarshal([]byte(text), &a) != nil || strings.Join(a.Rules.Closed, ",") != "8080 (vm "+labelVMID+")" {
				t.Errorf("the answer does not say port 8080 of the VM was closed: %s", text)
			}
		})
	}

	t.Run("gone, no rule left", func(t *testing.T) {
		cloud := newLabelCloud(false)
		cloud.rules = cloud.rules[1:]
		text, _ := folderB(t, cloud).handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID, "confirmed": true})
		if !strings.Contains(text, "already_deleted") || len(cloud.deleted) > 0 {
			t.Errorf("answer %s, deleted %v; want already_deleted and nothing deleted", text, cloud.deleted)
		}
	})
}
