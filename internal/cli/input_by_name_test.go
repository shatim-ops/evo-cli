// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"testing"
)

// D57 (audit cli-ids-1.6, §4): a subnet and a security group are named
// by their name as well as by their UUID. In the parity world, every tool
// that takes one answers the call by name — in another case of letters —
// as it answers the call by id, with nothing written. A value that is no
// name of the list reaches the tool as before D57 (its own refusal).
func TestInputByName_SubnetAndSecurityGroup(t *testing.T) {
	world := withParityWorld(t)
	reg := newRegistry()
	const subnetID, subnetName = "00000000-0000-0000-0000-0000000000d1", "default_ru.az-1"
	const sgID, sgName = "00000000-0000-0000-0000-0000000000c1", "ssh-access_ru.az-1"
	for _, c := range []struct {
		tool, arg, id, name string
	}{
		{"provision", "subnet_id", subnetID, subnetName},
		{"pg_create", "subnet_id", subnetID, subnetName},
		{"kafka_create", "subnet_id", subnetID, subnetName},
		{"redis_create", "subnet_id", subnetID, subnetName},
		{"k8s_create", "subnet_id", subnetID, subnetName},
		{"k8s_plan", "subnet_id", subnetID, subnetName},
		{"lb_create", "subnet_id", subnetID, subnetName},
		{"k8s_add_nodepool", "nodes_subnet_id", subnetID, subnetName},
		{"k8s_add_nodepool", "security_group_id", sgID, sgName},
	} {
		tl, ok := reg.Lookup(c.tool)
		if !ok {
			t.Fatalf("no tool %s", c.tool)
		}
		call := func(v string) (string, bool) {
			args := map[string]any{}
			for k, a := range parityArgs[c.tool] {
				args[k] = a
			}
			args[c.arg] = v
			return tl.Handler(context.Background(), args)
		}
		byID, _ := call(c.id)
		byName, _ := call(c.name)
		if byName != byID {
			t.Errorf("%s %s=%s: the answer differs from the call by id:\nby id:   %.400s\nby name: %.400s", c.tool, c.arg, c.name, byID, byName)
		}
		if w := world.rt.takeWrites(); len(w) > 0 {
			t.Errorf("%s: wrote %v", c.tool, w)
		}
	}
}
