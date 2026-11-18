// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"strings"
	"testing"
)

// placeKinds are the resources of the project another one is made in: a
// subnet, a VPC, a security group.
var placeKinds = []*ownerKind{kindSubnet, kindVPC, kindSG}

// placesWithoutKind are the arguments that name a place of a resource but
// have no ownerKind: a subnet checked by its list (whyListed), the type of
// a disk from the catalogue of the zone.
var placesWithoutKind = map[string]bool{"subnet_id": true, "nodes_subnet_id": true, "disk_type": true}

// An empty value of an argument that names where a resource lives inside
// the project — a subnet, a VPC, a security group, the type of a disk — is
// refused USAGE before the call, as the project and the zone are (D100):
// it was read as «not given», and the resource was made in the default
// place with exit 0 («Риск», 177; owner's decision of #1597, (а)). Every
// such argument of a changing tool is declared (Decl.Place): one of the
// kinds of a place, or a place without a kind.
func TestPlaceArgs_EmptyRefusedAndDeclared(t *testing.T) {
	r := New(nil)
	checked := 0
	for _, tl := range r.Tools() {
		if !tl.Mutating {
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name := range props {
			place := placesWithoutKind[name]
			for _, a := range decls[tl.Name].IDArgs {
				if a.name != name {
					continue
				}
				switch {
				case a.target:
					// The resource a call deletes or changes is not where
					// another one lives.
					place = false
				case slices.Contains(placeKinds, a.kind):
					place = true
				}
			}
			if !place {
				continue
			}
			if !slices.Contains(decls[tl.Name].Place, name) {
				t.Errorf("%s %s: names where the resource lives, not declared in Decl.Place", tl.Name, name)
				continue
			}
			checked++
			err := CheckArgs(tl, map[string]any{name: ""})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s %s: an empty value is not refused USAGE before the call: %v", tl.Name, name, err)
			}
		}
	}
	if checked < 13 {
		t.Fatalf("only %d places checked: the walk lost the tools", checked)
	}
}
