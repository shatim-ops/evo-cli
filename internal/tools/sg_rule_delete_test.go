// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// sg_rule_delete deletes one rule of a security group by its id
// (docs/internal/briefs/sg-rules.md §4): the table «what was read → the
// answer» has a world per row here.

const (
	sgZoneGroup  = "00000000-0000-4000-8000-0000000000e1"
	sgOtherGroup = "00000000-0000-4000-8000-0000000000e2"
	sgRuleSSH    = "00000000-0000-4000-8000-000000000e01"
	sgRuleWeb    = "00000000-0000-4000-8000-000000000e02"
	sgRuleOther  = "00000000-0000-4000-8000-000000000e03"
	sgRuleNone   = "00000000-0000-4000-8000-000000000e7f"
)

// sgWorld is a project with two security groups and their rules. A rule
// deleted goes to "deleting" and leaves the group on the read after the
// next (stays, when stuck).
type sgWorld struct {
	*faultCloud
	mu      sync.Mutex
	rules   map[string][]provision.SGRule
	deletes []string
	stuck   bool
	refuse  error
}

func newSGWorld() *sgWorld {
	return &sgWorld{faultCloud: &faultCloud{}, rules: map[string][]provision.SGRule{
		sgZoneGroup: {
			{ID: sgRuleSSH, Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0", State: "created"},
			{ID: sgRuleWeb, Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "443:443", RemoteIPPrefix: "0.0.0.0/0", State: "created"},
		},
		sgOtherGroup: {
			{ID: sgRuleOther, Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "10.0.0.0/8", State: "created"},
		},
	}}
}

func (w *sgWorld) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{
		{ID: sgZoneGroup, Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: faultZone, Default: true},
		{ID: sgOtherGroup, Name: "db", AvailabilityZoneID: faultZone},
	}, nil
}

func (w *sgWorld) ListSecurityGroupRules(_ context.Context, sgID string) ([]*provision.SGRule, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []*provision.SGRule
	kept := w.rules[sgID][:0:0]
	for _, r := range w.rules[sgID] {
		if r.State == "deleting" && !w.stuck {
			continue // gone by this read
		}
		kept = append(kept, r)
		c := r
		out = append(out, &c)
	}
	w.rules[sgID] = kept
	return out, nil
}

func (w *sgWorld) DeleteSecurityGroupRule(_ context.Context, sgID, ruleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deletes = append(w.deletes, ruleID)
	if w.refuse != nil {
		return w.refuse
	}
	for i := range w.rules[sgID] {
		if w.rules[sgID][i].ID == ruleID {
			w.rules[sgID][i].State = "deleting"
			return nil
		}
	}
	return clerrors.New(clerrors.CodeNotFound, "rule not found", "")
}

func (w *sgWorld) setRule(sgID, ruleID string, change func(*provision.SGRule)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.rules[sgID] {
		if w.rules[sgID][i].ID == ruleID {
			change(&w.rules[sgID][i])
		}
	}
}

func (w *sgWorld) deleted() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.deletes)
}

func sgRuleRegistry(t *testing.T, w *sgWorld) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    w,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings:  engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 50 * time.Millisecond},
		}, nil
	}), dir
}

// A rule that is there: the preview names the rule whole, the group and
// that it is the zone's shared one, and what stops getting through; with
// the yes one DELETE, and the answer once the rule is gone.
func TestSGRuleDelete_AsksWithAPreviewAndDeletesOnce(t *testing.T) {
	w := newSGWorld()
	r, _ := sgRuleRegistry(t, w)
	args := map[string]any{"security_group": "SSH-access_ru.AZ-1", "rule_id": sgRuleWeb}
	m, text, _ := callNamed(t, r, "sg_rule_delete", args)
	if m["status"] != "confirmation_required" || w.deleted() != 0 {
		t.Fatalf("sg_rule_delete without consent: %s (deletes %d)", text, w.deleted())
	}
	rule, _ := m["rule"].(map[string]any)
	if rule["id"] != sgRuleWeb || rule["direction"] != "ingress" || rule["protocol"] != "tcp" || rule["port_range"] != "443:443" || rule["source"] != "0.0.0.0/0" {
		t.Errorf("the preview does not name the rule whole: %s", text)
	}
	group, _ := m["security_group"].(map[string]any)
	if group["id"] != sgZoneGroup || group["name"] != "SSH-access_ru.AZ-1" || m["shared"] != true {
		t.Errorf("the preview does not name the group and that it is shared: %s", text)
	}
	if cuts, _ := m["cuts_access"].(string); !strings.Contains(cuts, "tcp") || !strings.Contains(cuts, "443") {
		t.Errorf("the preview does not say what stops getting through: %q", cuts)
	}
	args["confirmed"] = true
	m, text, isErr := callNamed(t, r, "sg_rule_delete", args)
	if isErr || m["status"] != "deleted" || m["rule_id"] != sgRuleWeb || w.deleted() != 1 {
		t.Fatalf("sg_rule_delete with consent: %s (deletes %d)", text, w.deleted())
	}
	// A repeat: the rule is not there — already deleted, no question, no
	// DELETE (D26).
	for _, confirmed := range []bool{false, true} {
		args["confirmed"] = confirmed
		m, text, isErr = callNamed(t, r, "sg_rule_delete", args)
		if isErr || m["status"] != "already_deleted" || w.deleted() != 1 {
			t.Errorf("the repeat, confirmed=%v: %s (deletes %d)", confirmed, text, w.deleted())
		}
	}
}

// The rest of the table: a rule being deleted, one still being made, a
// failed one, a rule of another group, a group that is not there.
func TestSGRuleDelete_TheRestOfTheTable(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		w := newSGWorld()
		r, _ := sgRuleRegistry(t, w)
		w.stuck = true
		w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = "deleting" })
		m, text, isErr := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb, "confirmed": confirmed})
		if isErr || m["status"] != "deleting" || m["already_deleting"] != true || w.deleted() != 0 {
			t.Errorf("a rule being deleted, confirmed=%v: %s", confirmed, text)
		}

		w = newSGWorld()
		r, _ = sgRuleRegistry(t, w)
		w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = "creating" })
		m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "creating") || w.deleted() != 0 {
			t.Errorf("a rule still being created, confirmed=%v: %s", confirmed, text)
		}

		// A rule of another group is not «already deleted»: a wrong group
		// is said, with the group the rule is in.
		w = newSGWorld()
		r, _ = sgRuleRegistry(t, w)
		m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleOther, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, sgOtherGroup) || w.deleted() != 0 {
			t.Errorf("a rule of another group, confirmed=%v: %s", confirmed, text)
		}

		// No such rule anywhere: already deleted (D26).
		m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleNone, "confirmed": confirmed})
		if isErr || m["status"] != "already_deleted" || w.deleted() != 0 {
			t.Errorf("no such rule, confirmed=%v: %s", confirmed, text)
		}

		// No such group: the call's error, not a repeat.
		m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": "nope", "rule_id": sgRuleWeb, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeNotFound || w.deleted() != 0 {
			t.Errorf("no such group, confirmed=%v: %s", confirmed, text)
		}
	}
	// A failed rule is asked about with a warning.
	w := newSGWorld()
	r, _ := sgRuleRegistry(t, w)
	w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = "error_deleting" })
	m, text, _ := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb})
	if m["status"] != "confirmation_required" || m["warning"] == nil {
		t.Errorf("a rule in error_deleting: %s, want the question with a warning", text)
	}
}

// What still lets the traffic in after the delete is named with its
// source: a person knows their address, evo does not.
func TestSGRuleDelete_NamesWhatStillAllows(t *testing.T) {
	w := newSGWorld()
	w.rules[sgZoneGroup] = append(w.rules[sgZoneGroup], provision.SGRule{ID: sgRuleOther, Direction: "ingress", EtherType: "IPv4",
		IPProtocol: "tcp", PortRange: "443:443", RemoteIPPrefix: "10.0.0.0/8", State: "created"})
	r, _ := sgRuleRegistry(t, w)
	m, text, _ := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb})
	still, _ := m["still_allowed_by"].([]any)
	if len(still) != 1 {
		t.Fatalf("still_allowed_by: %s", text)
	}
	if one, _ := still[0].(map[string]any); one["id"] != sgRuleOther || one["source"] != "10.0.0.0/8" {
		t.Errorf("still_allowed_by does not name the rule and its source: %s", text)
	}
}

// The last rule that lets TCP 22 in, on the group of a deployment evo
// manages over SSH, is not deleted without the word for it: refused before
// the question. With allow_cut_ssh the preview and the question; another
// rule on 22, or no deployment of this folder in the group — no refusal.
func TestSGRuleDelete_TheLastSSHRuleOfADeploymentIsRefused(t *testing.T) {
	record := state.DeploymentRecord{ID: faultID(0x11), VMID: faultID(0x11), VMName: "web", VMIP: "203.0.113.9",
		SecurityGroupID: sgZoneGroup, SecurityGroupName: "SSH-access_ru.AZ-1", Status: "running"}
	for _, confirmed := range []bool{false, true} {
		w := newSGWorld()
		r, dir := sgRuleRegistry(t, w)
		writeRecords(t, dir, record)
		args := map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleSSH, "confirmed": confirmed}
		m, text, isErr := callNamed(t, r, "sg_rule_delete", args)
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "allow_cut_ssh") || w.deleted() != 0 {
			t.Fatalf("the last SSH rule, confirmed=%v: %s (deletes %d)", confirmed, text, w.deleted())
		}
	}
	w := newSGWorld()
	r, dir := sgRuleRegistry(t, w)
	writeRecords(t, dir, record)
	m, text, _ := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleSSH, "allow_cut_ssh": true})
	if m["status"] != "confirmation_required" || m["cuts_ssh"] != true {
		t.Errorf("the last SSH rule with allow_cut_ssh: %s, want the question that says SSH is cut", text)
	}
	// Another rule lets 22 in: no refusal.
	w = newSGWorld()
	w.rules[sgZoneGroup] = append(w.rules[sgZoneGroup], provision.SGRule{ID: sgRuleOther, Direction: "ingress", EtherType: "IPv4",
		IPProtocol: "tcp", PortRange: "1:1024", RemoteIPPrefix: "10.0.0.0/8", State: "created"})
	r, dir = sgRuleRegistry(t, w)
	writeRecords(t, dir, record)
	if m, text, _ := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleSSH}); m["status"] != "confirmation_required" {
		t.Errorf("an SSH rule that is not the last: %s", text)
	}
	// No deployment of this folder in the group: no refusal.
	w = newSGWorld()
	r, _ = sgRuleRegistry(t, w)
	if m, text, _ := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleSSH}); m["status"] != "confirmation_required" {
		t.Errorf("the last SSH rule of a group with no deployment of this folder: %s", text)
	}
}

// The limit of the wait: the rule is still being deleted — said as such,
// with a read as the next step; a DELETE the platform refuses is an error.
func TestSGRuleDelete_SlowAndRefused(t *testing.T) {
	w := newSGWorld()
	w.stuck = true
	r, _ := sgRuleRegistry(t, w)
	m, text, isErr := callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb, "confirmed": true})
	if isErr || m["status"] != "deleting" || m["next_tool"] != "list_security_groups" || w.deleted() != 1 {
		t.Errorf("a rule that stays deleting: %s", text)
	}
	w = newSGWorld()
	w.refuse = clerrors.Wrap(clerrors.CodeAPIError, "refused", "", &provision.StatusError{Status: 422, Body: `[{"code":"zone_changes_prohibited","message":"refused"}]`})
	r, _ = sgRuleRegistry(t, w)
	m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleWeb, "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["next_tool"] != nil {
		t.Errorf("a DELETE the platform refuses: %s", text)
	}
}
