// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"fmt"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// «The internet» is a threshold, not two literals: a refusal that advises
// to narrow the source is passed with 0.0.0.0/1 and 128.0.0.0/1 otherwise
// («Риск», conclusion 146, У1).
func TestInternet_IsAThresholdNotTwoLiterals(t *testing.T) {
	for cidr, want := range map[string]bool{
		"0.0.0.0/0": true, "0.0.0.0/1": true, "128.0.0.0/1": true, "64.0.0.0/2": true, "8.0.0.0/7": true,
		"10.0.0.0/7": true, // not wholly private
		"::/0":       true, "::/1": true, "2000::/3": true, "2001::/16": true,
		"10.0.0.0/8": false, "203.0.113.0/24": false, "8.0.0.0/8": false, "203.0.113.7/32": false,
		"172.16.0.0/12": false, "192.168.0.0/16": false,
		"fc00::/7": false, "fd00::/8": false, "fe80::/10": false, "2001:db8::/32": false,
		"203.0.113.7": false, "": false, "not a cidr": false,
	} {
		if got := Internet(cidr); got != want {
			t.Errorf("Internet(%q) = %v, want %v", cidr, got, want)
		}
	}
}

func TestUse_SharedByDefaultFlagOrByCount(t *testing.T) {
	iface := func(vm, group string) *provision.Interface {
		return &provision.Interface{ID: "if-" + vm, VM: &provision.InterfaceVM{ID: "id-" + vm, Name: vm},
			SecurityGroups: []provision.InterfaceGroup{{ID: group}}}
	}
	own := &provision.SecurityGroup{ID: "g-own", Name: "own"}
	zone := &provision.SecurityGroup{ID: "g-zone", Name: "zone", Default: true}
	// A group of its own with one interface is not shared; with two it is.
	if u := Use(own, []*provision.Interface{iface("a", "g-own"), iface("b", "g-zone")}, true); u.Shared || !u.Counted || u.Interfaces != 1 || len(u.VMs) != 1 || u.VMs[0] != "a" {
		t.Errorf("one interface in its own group: %+v", u)
	}
	if u := Use(own, []*provision.Interface{iface("a", "g-own"), iface("b", "g-own")}, true); !u.Shared || u.Interfaces != 2 {
		t.Errorf("two interfaces in a group: %+v", u)
	}
	// The zone's default group is shared whatever is in it.
	if u := Use(zone, nil, true); !u.Shared || u.Interfaces != 0 {
		t.Errorf("the zone's default group, empty: %+v", u)
	}
	// Interfaces unread: a group the API says has interfaces counts as
	// shared — a failed read does not lift the rule (У3).
	if u := Use(&provision.SecurityGroup{ID: "g", HasInterfaces: true}, nil, false); !u.Shared || u.Counted {
		t.Errorf("interfaces unread, has_interfaces: %+v", u)
	}
	if u := Use(&provision.SecurityGroup{ID: "g"}, nil, false); u.Shared {
		t.Errorf("interfaces unread, no interfaces by the API: %+v", u)
	}
	// More VMs than a preview names.
	var many []*provision.Interface
	for i := 0; i < MaxVMsNamed+3; i++ {
		many = append(many, iface(fmt.Sprintf("vm-%02d", i), "g-own"))
	}
	if u := Use(own, many, true); len(u.VMs) != MaxVMsNamed || u.MoreVMs != 3 || u.Interfaces != MaxVMsNamed+3 {
		t.Errorf("many VMs: %+v", u)
	}
}
