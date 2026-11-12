// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D105: a tool that changes the rules of an existing group does not open a
// port to the whole internet on a shared group without the person's own
// word for it — refused before the question, with the consent and without
// (owner's decision 12.11; «Риск», conclusion 146). The world's group is
// the zone's default one: shared.

func openPortsArgs(cidr string, more ...map[string]any) map[string]any {
	args := map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(8080)}}
	if cidr != "" {
		args["cidr"] = cidr
	}
	for _, m := range more {
		for k, v := range m {
			args[k] = v
		}
	}
	return args
}

func rulesAdded(c *faultCloud) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rules)
}

func TestOpenPorts_TheWholeInternetOnASharedGroupIsRefusedWithoutTheWord(t *testing.T) {
	// The default source is the whole internet; the others pass a refusal
	// that knows two literals only (У1).
	for _, cidr := range []string{"", "0.0.0.0/0", "0.0.0.0/1", "128.0.0.0/1", "::/0", "::/1"} {
		for _, confirmed := range []bool{false, true} {
			cloud := &faultCloud{}
			r, _ := faultToolsRegistry(t, cloud)
			m, text, isErr := callNamed(t, r, "open_ports", openPortsArgs(cidr, map[string]any{"confirmed": confirmed}))
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, allowInternetArg) || rulesAdded(cloud) != 0 {
				t.Errorf("cidr %q, confirmed=%v: %s (rules %d), want the refusal before the question", cidr, confirmed, text, rulesAdded(cloud))
			}
			// The refusal names no other tool as a way round.
			for _, tool := range []string{"sg_rule_create", "close_ports", "provision", "deploy"} {
				if strings.Contains(text, tool) {
					t.Errorf("the refusal names %s: %s", tool, text)
				}
			}
		}
	}
}

// With the word the preview says «open to the internet», the group and how
// many interfaces the rule acts on, and asks; with the yes the rule is
// added. A source narrower than the threshold asks as before.
func TestOpenPorts_WithTheWordOrANarrowSourceAsks(t *testing.T) {
	cloud := &faultCloud{}
	if _, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "db"}); err != nil {
		t.Fatal(err)
	}
	r, _ := faultToolsRegistry(t, cloud)
	m, text, _ := callNamed(t, r, "open_ports", openPortsArgs("", map[string]any{allowInternetArg: true}))
	if m["status"] != "confirmation_required" || m["open_to_internet"] != true || m["shared"] != true || m["interfaces"] != float64(2) {
		t.Fatalf("with the word: %s, want the question that says the internet, the shared group and 2 interfaces", text)
	}
	if vms, _ := m["vms"].([]any); len(vms) != 2 || vms[0] != "db" || vms[1] != "web" {
		t.Errorf("the preview does not name the VMs under the group: %s", text)
	}
	m, text, isErr := callNamed(t, r, "open_ports", openPortsArgs("", map[string]any{allowInternetArg: true, "confirmed": true}))
	if isErr || rulesAdded(cloud) != 1 {
		t.Fatalf("with the word and the yes: %s (rules %d)", text, rulesAdded(cloud))
	}
	_ = m
	for _, cidr := range []string{"203.0.113.0/24", "10.0.0.0/8", "8.0.0.0/8"} {
		cloud := &faultCloud{}
		r, _ := faultToolsRegistry(t, cloud)
		m, text, _ := callNamed(t, r, "open_ports", openPortsArgs(cidr))
		if m["status"] != "confirmation_required" || m["open_to_internet"] != nil || m["shared"] != true {
			t.Errorf("cidr %s: %s, want the question, the group shared and no «open to the internet»", cidr, text)
		}
	}
}

// A port that is open already is not a rule to add: nothing to refuse.
func TestOpenPorts_NothingToOpenIsNotRefused(t *testing.T) {
	cloud := &faultCloud{rules: []provision.SGRule{
		{ID: faultID(0x71), Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "0.0.0.0/0"},
	}}
	r, _ := faultToolsRegistry(t, cloud)
	m, text, _ := callNamed(t, r, "open_ports", openPortsArgs(""))
	if m["status"] != "confirmation_required" || m["open_to_internet"] != nil {
		t.Errorf("a port open already: %s", text)
	}
}

// The interfaces could not be read: the zone's default group is shared all
// the same, the preview says the count is unknown, and the refusal stands
// (У3).
func TestOpenPorts_InterfacesUnreadDoNotLiftTheRule(t *testing.T) {
	cloud := &faultCloud{}
	cloud.failNth("ListInterfaces", 1, faultHTTP500)
	r, _ := faultToolsRegistry(t, cloud)
	m, text, isErr := callNamed(t, r, "open_ports", openPortsArgs(""))
	if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "could not read") {
		t.Errorf("interfaces unread, the internet: %s, want the refusal that says the count is unknown", text)
	}
	cloud = &faultCloud{}
	cloud.failNth("ListInterfaces", 1, faultHTTP500)
	r, _ = faultToolsRegistry(t, cloud)
	m, text, _ = callNamed(t, r, "open_ports", openPortsArgs("203.0.113.0/24"))
	if m["status"] != "confirmation_required" || m["shared"] != true || m["interfaces_unread"] == nil || m["interfaces"] != nil {
		t.Errorf("interfaces unread, a narrow source: %s", text)
	}
}

// The rule is of the tools that change the rules of an existing group; a
// create opens its own ports as part of the consent to the create (owner's
// decision 12.11, variant 1): provision with a port to anyone, in the
// zone's default group, asks and creates as before — no word needed.
func TestD105_ProvisionWithAPortNeedsNoWord(t *testing.T) {
	cloud, _, r := provisionWorld(t)
	m := decode(t, mustCallErr(t, r, "provision", map[string]any{"name": "web", "ports": []any{"8080"}}))
	if m["status"] != "confirmation_required" {
		t.Fatalf("provision with a port, no consent: %v, want its question as before", m)
	}
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "ports": []any{"8080"}, "confirmed": true})
	m = decode(t, text)
	if isErr || m["reason"] == clerrors.CodeUsage || strings.Contains(text, allowInternetArg) {
		t.Fatalf("provision with a port and the consent: %s, want the create as before", text)
	}
	if groups, _ := cloud.ListSecurityGroups(context.Background(), ""); len(groups) != 1 || !groups[0].Default {
		t.Fatalf("the world's group is not the zone's default one: the test holds nothing")
	}
}
