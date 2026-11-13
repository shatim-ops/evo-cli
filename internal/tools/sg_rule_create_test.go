// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// sg_rule_create adds one rule to a security group, named whole by the
// caller (docs/internal/briefs/sg-rules.md §3): a world per line of the
// brief's list of tests.

// sgMakeWorld is sgWorld with the write of a rule: a rule added is
// "creating" and "created" on the read after the next (stays creating
// when slow; "error_creating" when broken). lose makes the answer of the
// write an error although the rule is made.
type sgMakeWorld struct {
	*sgWorld
	adds   []provision.SGRule
	slow   bool
	broken bool
	lose   bool
	// lag: a rule the write made is not listed for this many reads of the
	// group's rules — the list is behind the write.
	lag    int
	hidden map[string]int
	// conflict: the write finds the rule made by another call and answers 409.
	conflict bool
	refuse   error
	reads    map[string]int
}

func newSGMakeWorld() *sgMakeWorld {
	return &sgMakeWorld{sgWorld: newSGWorld(), reads: map[string]int{}}
}

func (w *sgMakeWorld) AddSecurityGroupRule(_ context.Context, sgID, _ string, r provision.SGRule) (*provision.SGRule, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adds = append(w.adds, r)
	if w.refuse != nil {
		return nil, w.refuse
	}
	r.ID = fmt.Sprintf("00000000-0000-4000-8000-000000000f%02d", len(w.adds))
	r.State = "creating"
	if r.RemoteSecurityGroupID != "" {
		r.RemoteSecurityGroup, r.RemoteSecurityGroupID = &provision.SGRuleGroup{ID: r.RemoteSecurityGroupID}, ""
	}
	w.rules[sgID] = append(w.rules[sgID], r)
	if w.lag > 0 {
		if w.hidden == nil {
			w.hidden = map[string]int{}
		}
		w.hidden[r.ID] = w.lag
	}
	if w.lose {
		return nil, clerrors.New(clerrors.CodeAPIUnreachable, "connection reset", "")
	}
	if w.conflict {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "conflict", "", &provision.StatusError{Status: 409, Body: `[{"code":"conflict","message":"exists"}]`})
	}
	return &r, nil
}

func (w *sgMakeWorld) ListSecurityGroupRules(ctx context.Context, sgID string) ([]*provision.SGRule, error) {
	w.mu.Lock()
	for i := range w.rules[sgID] {
		r := &w.rules[sgID][i]
		if r.State != "creating" || !strings.Contains(r.ID, "000000000f") || w.slow {
			continue
		}
		if w.reads[r.ID]++; w.reads[r.ID] > 1 {
			r.State = "created"
			if w.broken {
				r.State = "error_creating"
			}
		}
	}
	behind := map[string]bool{}
	for id, left := range w.hidden {
		if left > 0 {
			w.hidden[id], behind[id] = left-1, true
		}
	}
	w.mu.Unlock()
	rules, err := w.sgWorld.ListSecurityGroupRules(ctx, sgID)
	shown := rules[:0:0]
	for _, r := range rules {
		if !behind[r.ID] {
			shown = append(shown, r)
		}
	}
	return shown, err
}

func (w *sgMakeWorld) added() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.adds)
}

func sgMakeRegistry(t *testing.T, w *sgMakeWorld) *Registry {
	t.Helper()
	r, _ := sgRegistryOver(t, w)
	return r
}

// Each direction and protocol builds the body the API takes; the preview
// names the rule, the group and its use, and the price as unknown; with
// the yes one request, and the answer once the rule is created.
func TestSGRuleCreate_BuildsTheBodyAndAnswersCreated(t *testing.T) {
	for _, c := range []struct {
		args map[string]any
		want provision.SGRule
	}{
		{map[string]any{"direction": "ingress", "protocol": "tcp", "ports": "8443", "cidr": "203.0.113.7/24"},
			provision.SGRule{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "8443:8443", RemoteIPPrefix: "203.0.113.0/24"}},
		{map[string]any{"direction": "ingress", "protocol": "udp", "ports": "8000:8100", "cidr": "2001:db8::/48", "description": "metrics"},
			provision.SGRule{Direction: "ingress", EtherType: "IPv6", IPProtocol: "udp", PortRange: "8000:8100", RemoteIPPrefix: "2001:db8::/48", Description: "metrics"}},
		{map[string]any{"direction": "egress", "protocol": "tcp", "ports": "5432", "cidr": "10.0.0.0/8"},
			provision.SGRule{Direction: "egress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "10.0.0.0/8"}},
		{map[string]any{"direction": "ingress", "protocol": "tcp", "ports": "5432", "remote_group": "db"},
			provision.SGRule{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "5432:5432", RemoteSecurityGroupID: sgOtherGroup}},
	} {
		w := newSGMakeWorld()
		r := sgMakeRegistry(t, w)
		c.args["security_group"] = sgZoneGroup
		m, text, _ := callNamed(t, r, "sg_rule_create", c.args)
		p, _ := m["preview"].(map[string]any)
		rule, _ := p["rule"].(map[string]any)
		if m["status"] != "confirmation_required" || w.added() != 0 || rule["port_range"] != c.want.PortRange || rule["ether_type"] != c.want.EtherType {
			t.Fatalf("%v without consent: %s (writes %d)", c.args, text, w.added())
		}
		if p["shared"] != true || p["tariff_url"] == "" || p["price_classes"] == nil {
			t.Errorf("%v: the preview does not name the group as shared and the price as unknown: %s", c.args, text)
		}
		c.args["confirmed"] = true
		m, text, isErr := callNamed(t, r, "sg_rule_create", c.args)
		if isErr || m["status"] != "created" || m["rule_id"] == "" || w.added() != 1 {
			t.Fatalf("%v with consent: %s (writes %d)", c.args, text, w.added())
		}
		if w.adds[0] != c.want {
			t.Errorf("%v: the body is %+v, want %+v", c.args, w.adds[0], c.want)
		}
	}
}

// The same rule there already: already_exists with its id, no question and
// no write — with consent and without. A wider rule is not «there
// already»: the question, with the wider rule named.
func TestSGRuleCreate_TheSameRuleIsAlreadyThereAWiderOneIsNamed(t *testing.T) {
	same := map[string]any{"security_group": sgZoneGroup, "direction": "ingress", "protocol": "tcp", "ports": "443:443", "cidr": "0.0.0.0/0", allowInternetArg: true}
	for _, confirmed := range []bool{false, true} {
		w := newSGMakeWorld()
		r := sgMakeRegistry(t, w)
		same["confirmed"] = confirmed
		m, text, isErr := callNamed(t, r, "sg_rule_create", same)
		if isErr || m["status"] != "already_exists" || m["rule_id"] != sgRuleWeb || w.added() != 0 {
			t.Errorf("the same rule, confirmed=%v: %s (writes %d)", confirmed, text, w.added())
		}
	}
	w := newSGMakeWorld()
	r := sgMakeRegistry(t, w)
	m, text, _ := callNamed(t, r, "sg_rule_create", map[string]any{"security_group": sgZoneGroup, "direction": "ingress", "protocol": "tcp", "ports": "443", "cidr": "203.0.113.0/24"})
	p, _ := m["preview"].(map[string]any)
	covered, _ := p["covered_by"].([]any)
	if m["status"] != "confirmation_required" || len(covered) != 1 {
		t.Fatalf("a rule narrower than one there: %s, want the question with covered_by", text)
	}
	if one, _ := covered[0].(map[string]any); one["id"] != sgRuleWeb {
		t.Errorf("covered_by does not name the wider rule: %s", text)
	}
	// A rule with the same fields that failed is not «there»: named, asked.
	w = newSGMakeWorld()
	w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = "error_creating" })
	r = sgMakeRegistry(t, w)
	same["confirmed"] = false
	m, text, _ = callNamed(t, r, "sg_rule_create", same)
	p, _ = m["preview"].(map[string]any)
	if failed, _ := p["failed_rules"].([]any); m["status"] != "confirmation_required" || len(failed) != 1 || failed[0] != sgRuleWeb {
		t.Errorf("the same rule in error_creating: %s, want the question that names it", text)
	}
	// The same rule still being created: its outcome, no second request.
	w = newSGMakeWorld()
	w.slow = true
	w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = "creating" })
	r = sgMakeRegistry(t, w)
	same["confirmed"] = true
	m, text, isErr := callNamed(t, r, "sg_rule_create", same)
	if isErr || m["status"] != "creating" || m["rule_id"] != sgRuleWeb || m["next_tool"] != "list_security_groups" || w.added() != 0 {
		t.Errorf("the same rule in creating: %s (writes %d)", text, w.added())
	}
}

// The internet on a shared group (D105): refused before the question
// without the word, with consent and without — by the threshold, not two
// literals; with the word the question says «open to the internet». A
// group that is not shared: the warning, no refusal. Egress and a source
// group are not «the internet».
func TestSGRuleCreate_TheInternetOnASharedGroupNeedsTheWord(t *testing.T) {
	base := func(group, cidr string) map[string]any {
		return map[string]any{"security_group": group, "direction": "ingress", "protocol": "tcp", "ports": "8080", "cidr": cidr}
	}
	for _, cidr := range []string{"0.0.0.0/0", "0.0.0.0/1", "128.0.0.0/1", "::/0", "::/1", "10.0.0.0/7"} {
		for _, confirmed := range []bool{false, true} {
			w := newSGMakeWorld()
			r := sgMakeRegistry(t, w)
			args := base(sgZoneGroup, cidr)
			args["confirmed"] = confirmed
			m, text, isErr := callNamed(t, r, "sg_rule_create", args)
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, allowInternetArg) || w.added() != 0 {
				t.Errorf("%s on the zone's group, confirmed=%v: %s (writes %d)", cidr, confirmed, text, w.added())
			}
			if strings.Contains(text, "open_ports") {
				t.Errorf("%s: the refusal names another tool: %s", cidr, text)
			}
		}
	}
	w := newSGMakeWorld()
	r := sgMakeRegistry(t, w)
	args := base(sgZoneGroup, "0.0.0.0/0")
	args[allowInternetArg] = true
	m, text, _ := callNamed(t, r, "sg_rule_create", args)
	p, _ := m["preview"].(map[string]any)
	if m["status"] != "confirmation_required" || p["open_to_internet"] != true || p["warning"] == nil {
		t.Errorf("the internet with the word: %s, want the question that says it", text)
	}
	// Not shared: asked, with the warning.
	m, text, _ = callNamed(t, r, "sg_rule_create", base(sgOtherGroup, "0.0.0.0/0"))
	p, _ = m["preview"].(map[string]any)
	if m["status"] != "confirmation_required" || p["open_to_internet"] != true || p["shared"] != false {
		t.Errorf("the internet on a group that is not shared: %s", text)
	}
	// Narrow, egress, a source group: no refusal and no «internet».
	for name, args := range map[string]map[string]any{
		"narrow": base(sgZoneGroup, "203.0.113.0/24"),
		"egress": {"security_group": sgZoneGroup, "direction": "egress", "protocol": "tcp", "ports": "443", "cidr": "0.0.0.0/0"},
		"group":  {"security_group": sgZoneGroup, "direction": "ingress", "protocol": "tcp", "ports": "8080", "remote_group": sgOtherGroup},
	} {
		m, text, _ := callNamed(t, r, "sg_rule_create", args)
		p, _ := m["preview"].(map[string]any)
		if m["status"] != "confirmation_required" || p["open_to_internet"] != nil {
			t.Errorf("%s: %s", name, text)
		}
	}
}

// What the schema and the plan refuse before the cloud: no default for
// the direction or the source, icmp and any are not there yet, a port that
// is not one.
func TestSGRuleCreate_RefusedBeforeTheCloud(t *testing.T) {
	full := func(change func(map[string]any)) map[string]any {
		a := map[string]any{"security_group": sgZoneGroup, "direction": "ingress", "protocol": "tcp", "ports": "8080", "cidr": "10.0.0.0/8", "confirmed": true}
		change(a)
		return a
	}
	for name, args := range map[string]map[string]any{
		"no direction":   full(func(a map[string]any) { delete(a, "direction") }),
		"no source":      full(func(a map[string]any) { delete(a, "cidr") }),
		"two sources":    full(func(a map[string]any) { a["remote_group"] = "db" }),
		"icmp":           full(func(a map[string]any) { a["protocol"] = "icmp" }),
		"any":            full(func(a map[string]any) { a["protocol"] = "any" }),
		"port 0":         full(func(a map[string]any) { a["ports"] = "0" }),
		"range reversed": full(func(a map[string]any) { a["ports"] = "90:80" }),
		"port word":      full(func(a map[string]any) { a["ports"] = "ssh" }),
		"long words":     full(func(a map[string]any) { a["description"] = strings.Repeat("я", 256) }),
		"no such group":  full(func(a map[string]any) { a["remote_group"], a["cidr"] = "nope", "" }),
	} {
		w := newSGMakeWorld()
		r := sgMakeRegistry(t, w)
		m, text, isErr := callNamed(t, r, "sg_rule_create", args)
		if !isErr || m["status"] == "confirmation_required" || w.added() != 0 {
			t.Errorf("%s: %s (writes %d), want a refusal before the question", name, text, w.added())
		}
	}
}

// The ends of the write: the limit of the wait — creating, with a read as
// the next step; a rule that failed — an error that names it; a lost
// answer does not make a second rule; a refusal of the platform is an
// error with its code and no step.
func TestSGRuleCreate_SlowBrokenLostRefused(t *testing.T) {
	args := map[string]any{"security_group": sgOtherGroup, "direction": "ingress", "protocol": "tcp", "ports": "8080", "cidr": "10.0.0.0/8", "confirmed": true}
	w := newSGMakeWorld()
	w.slow = true
	m, text, isErr := callNamed(t, sgMakeRegistry(t, w), "sg_rule_create", args)
	if isErr || m["status"] != "creating" || m["rule_id"] == "" || m["next_tool"] != "list_security_groups" || w.added() != 1 {
		t.Errorf("a rule that stays creating: %s", text)
	}
	w = newSGMakeWorld()
	w.broken = true
	m, text, isErr = callNamed(t, sgMakeRegistry(t, w), "sg_rule_create", args)
	if !isErr || m["rule_id"] == "" || m["state"] != "error_creating" || !strings.Contains(text, "sg_rule_delete") {
		t.Errorf("a rule that failed: %s", text)
	}
	w = newSGMakeWorld()
	w.lose = true
	r := sgMakeRegistry(t, w)
	m, text, isErr = callNamed(t, r, "sg_rule_create", args)
	if isErr || m["status"] != "created" || m["rule_id"] == "" || w.added() != 1 {
		t.Errorf("a lost answer of a rule that was made: %s (writes %d)", text, w.added())
	}
	if m, text, _ := callNamed(t, r, "sg_rule_create", args); m["status"] != "already_exists" || w.added() != 1 {
		t.Errorf("the repeat after a lost answer: %s (writes %d)", text, w.added())
	}
	// A conflict (409) with the rule there: another call made it between
	// the plan and the write — already_exists, not «created» by this one.
	w = newSGMakeWorld()
	w.conflict = true
	m, text, isErr = callNamed(t, sgMakeRegistry(t, w), "sg_rule_create", args)
	if isErr || m["status"] != "already_exists" || m["rule_id"] == "" || w.added() != 1 {
		t.Errorf("a conflict with the rule there: %s (writes %d)", text, w.added())
	}
	w = newSGMakeWorld()
	w.refuse = clerrors.Wrap(clerrors.CodeAPIError, "refused", "", &provision.StatusError{Status: 422, Body: `[{"code":"zone_changes_prohibited","message":"refused"}]`})
	m, text, isErr = callNamed(t, sgMakeRegistry(t, w), "sg_rule_create", args)
	details, _ := m["details"].(map[string]any)
	if !isErr || m["reason"] != clerrors.CodeAPIError || details["platform_code"] != "zone_changes_prohibited" || m["next_tool"] != nil || w.added() != 1 {
		t.Errorf("a create the platform refuses: %s", text)
	}
}

// The write is taken, its answer is lost, and the list of rules is behind:
// the rule is there and not yet listed. One read after the lost answer
// used to end the call with «try again», and the same call with the same
// consent made a second rule (the interim delta audit of 1.9, FB19I-12).
// The call waits for the rule to be listed; when the wait ends without it
// the error says the rule may have been created and names no repeat, and
// a consented repeat is asked again instead of sent.
func TestSGRuleCreate_ALostAnswerAndAListBehindMakeOneRule(t *testing.T) {
	args := map[string]any{"security_group": sgOtherGroup, "direction": "ingress", "protocol": "tcp", "ports": "8080", "cidr": "10.0.0.0/8", "confirmed": true}
	// The list catches up within the wait: created, one write.
	w := newSGMakeWorld()
	w.lose, w.lag = true, 4
	m, text, isErr := callNamed(t, sgMakeRegistry(t, w), "sg_rule_create", args)
	if isErr || m["status"] != "created" || m["rule_id"] == "" || w.added() != 1 {
		t.Fatalf("a lost answer with the list 4 reads behind: %s (writes %d), want created by one write", text, w.added())
	}
	// The list does not catch up within the wait: an error that says the
	// rule may be there and sends to the read, not to a repeat.
	w = newSGMakeWorld()
	w.lose, w.lag = true, 1<<20
	r := sgMakeRegistry(t, w)
	m, text, isErr = callNamed(t, r, "sg_rule_create", args)
	if !isErr || w.added() != 1 {
		t.Fatalf("a lost answer with the list far behind: %s (writes %d)", text, w.added())
	}
	hint, _ := m["hint"].(string)
	if !strings.Contains(hint, "may have been created") || !strings.Contains(hint, "list_security_groups") || strings.Contains(hint, "try again") || m["next_tool"] == "sg_rule_create" {
		t.Errorf("the error does not say the rule may be there, or sends to a repeat: %s", text)
	}
	// The same call with the same consent: asked again, not sent.
	m, text, _ = callNamed(t, r, "sg_rule_create", args)
	if m["status"] != "confirmation_required" || m["interrupted"] == nil || w.added() != 1 {
		t.Errorf("the consented repeat after a cut call: %s (writes %d), want the question again and no second write", text, w.added())
	}
}
