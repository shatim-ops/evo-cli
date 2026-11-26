// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
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
	c.rules = slices.DeleteFunc(c.rules, func(r *provision.SGRule) bool { return r.ID == id })
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
	r := New(func(map[string]any) (*engine.Engine, error) {
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
	r.SetStateDir(dir)
	return r
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

// unreadRulesCloud answers 503 for the rules of the groups.
type unreadRulesCloud struct{ *labelCloud }

func (*unreadRulesCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, clerrors.New(clerrors.CodeAPIError, "API returned status 503", "")
}

// A rule list that cannot be read is not "no rules" (axis 3): the preview
// and the answer say the labelled rules were not all checked, with the
// class of the error and the way to close them; the VM's delete goes on —
// the clean-up comes first. A VM already gone answers the same, not a bare
// already_deleted.
func TestDestroyTargeted_AnUnreadRuleListIsSaid(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listed bool
	}{{"the VM deleted by this call", true}, {"the VM already gone", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &unreadRulesCloud{newLabelCloud(tc.listed)}
			r := folderB(t, cloud)
			preview, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID})
			if !strings.Contains(preview, `"labelled_rules_unchecked": "API_ERROR"`) || !strings.Contains(preview, "not all checked") {
				t.Fatalf("the preview does not say the rules were not checked:\n%s", preview)
			}
			text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": labelVMID, "confirmed": true})
			if isErr {
				t.Fatalf("the VM's delete must go on: %s", text)
			}
			answer := decode(t, text)
			hint, _ := answer["hint"].(string)
			if answer["labelled_rules_unchecked"] != "API_ERROR" || !strings.Contains(hint, `destroy with vm_id="`+labelVMID+`"`) || !strings.Contains(text, "not all checked") {
				t.Errorf("the answer does not say the rules were not checked: %s", text)
			}
			if tc.listed && !cloud.vmGone {
				t.Error("the VM was not deleted")
			}
		})
	}
}

// The VM had no record in this directory. An accepted rule deletion must
// leave its own exact IDs on disk for another process, even if the rule
// is already deleting or its group later cannot be read.
type pendingLabelCloud struct {
	*labelCloud
	dir        string
	sawReceipt bool
	readFailed bool
	requested  bool
}

func (c *pendingLabelCloud) DeleteSecurityGroupRule(_ context.Context, group, id string) error {
	c.deleted = append(c.deleted, id)
	sf, err := state.Read(state.DefaultPath(c.dir))
	if err != nil {
		return err
	}
	d := sf.FindDeployment(labelVMID)
	if d != nil && len(d.OpenedRules) == 1 {
		r := d.OpenedRules[0]
		c.sawReceipt = r.ID == id && r.SecurityGroupID == group && d.VMID == labelVMID
	}
	c.requested = true
	for _, r := range c.rules {
		if r.ID == id {
			r.State = "deleting"
		}
	}
	return nil
}

func (c *pendingLabelCloud) ListFloatingIPs(ctx context.Context, project string) ([]*provision.FloatingIP, error) {
	if c.requested {
		return []*provision.FloatingIP{{ID: "other-address", Name: "fip-" + labelVMID}}, nil
	}
	return c.labelCloud.ListFloatingIPs(ctx, project)
}

func (c *pendingLabelCloud) ListSecurityGroupRules(ctx context.Context, group string) ([]*provision.SGRule, error) {
	if c.requested && c.readFailed {
		return nil, clerrors.New(clerrors.CodeAPIError, "inventory failed", "")
	}
	return c.labelCloud.ListSecurityGroupRules(ctx, group)
}

func TestDestroyTargeted_KeepsLabelReceiptUntilCompleteRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{{"visible", false}, {"unreadable", true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &pendingLabelCloud{labelCloud: newLabelCloud(false), readFailed: tc.fail}
			reg := folderB(t, c)
			c.dir, _ = reg.stateDir()
			call := func() map[string]any {
				t.Helper()
				r, failed := reg.handlers["destroy"](t.Context(), map[string]any{"vm_id": labelVMID, "confirmed": true})
				var out map[string]any
				if failed || json.Unmarshal([]byte(r), &out) != nil {
					t.Fatalf("destroy: %s", r)
				}
				return out
			}
			first := call()
			if first["cleanup_pending"] != true || first["hint"] == nil || !c.sawReceipt || !slices.Equal(c.deleted, []string{"rule-a"}) {
				t.Fatalf("unconfirmed result=%v before-DELETE receipt=%v deleted=%v", first, c.sawReceipt, c.deleted)
			}
			saved, err := state.Read(state.DefaultPath(c.dir))
			if err != nil {
				t.Fatal(err)
			}
			d := saved.FindDeployment(labelVMID)
			if d == nil || d.Status != "destroyed" || len(d.OpenedRules) != 1 || d.OpenedRules[0].DeleteError == "" || d.OpenedRules[0].ID != "rule-a" {
				t.Fatalf("pending state: %+v", d)
			}
			c.readFailed = false
			if still := call(); still["cleanup_pending"] != true || len(c.deleted) != 1 {
				t.Fatalf("repeated deleting response=%v deletes=%v", still, c.deleted)
			}
			// A rule of the same ID re-labelled for someone else is not
			// ours to delete on retry.
			c.rules[0].State = "active"
			c.rules[0].Description = sgrules.RuleLabel("8080", "another-vm")
			if changed := call(); changed["cleanup_pending"] != true || len(c.deleted) != 1 {
				t.Fatalf("changed owner response=%v deletes=%v", changed, c.deleted)
			}
			c.rules = slices.DeleteFunc(c.rules, func(r *provision.SGRule) bool { return r.ID == "rule-a" })
			final := call()
			saved, err = state.Read(state.DefaultPath(c.dir))
			if err != nil || final["cleanup_pending"] == true || saved.FindDeployment(labelVMID) != nil || !slices.Equal(c.deleted, []string{"rule-a"}) || len(c.rules) != 2 {
				t.Fatalf("final=%v state=%+v deletes=%v", final, saved, c.deleted)
			}
		})
	}
}

// No deployment or rule ID was saved in this folder when the VM was
// deleted. A new call must still be able to find its labelled rules.
type intermittentLabelCloud struct {
	*labelCloud
	readable bool
}

func (c *intermittentLabelCloud) ListSecurityGroupRules(ctx context.Context, id string) ([]*provision.SGRule, error) {
	if !c.readable {
		return nil, clerrors.New(clerrors.CodeAPIError, "inventory unavailable", "")
	}
	return c.labelCloud.ListSecurityGroupRules(ctx, id)
}

func TestDestroyTargeted_RetryUncheckedInventoryWithoutKnownRule(t *testing.T) {
	cloud := &intermittentLabelCloud{labelCloud: newLabelCloud(true)}
	registry := folderB(t, cloud)
	eng, err := registry.newEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := state.DefaultPath(eng.StateDir)
	for attempt := 0; attempt < 3; attempt++ {
		cloud.readable = attempt == 2
		args := map[string]any{"vm_id": labelVMID, "confirmed": true}
		text, failed := registry.handlers["destroy"](t.Context(), args)
		var result struct {
			Pending bool `json:"cleanup_pending"`
		}
		if failed || json.Unmarshal([]byte(text), &result) != nil || result.Pending == cloud.readable {
			t.Fatalf("attempt %d: %s", attempt, text)
		}
		sf, err := state.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if (sf.FindDeployment(labelVMID) != nil) == cloud.readable {
			t.Fatalf("attempt %d: state=%v", attempt, sf.Deployments)
		}
	}
	if !slices.Equal(cloud.deleted, []string{"rule-a"}) {
		t.Fatalf("rule deletes: %v", cloud.deleted)
	}
}
