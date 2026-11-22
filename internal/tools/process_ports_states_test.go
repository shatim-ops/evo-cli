// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// open_ports tests coverage; close_ports selects by protocol, source and port
// and acts on the matching ID. These are distinct from sg_rule_{create,delete}.
var processPortStateRows = map[string]map[string]string{
	"open_ports": {
		"created": "covered", "creating": "covered", "updating": "covered",
		"deleting": "add", "error": "add", "error_creating": "add", "error_deleting": "add",
	},
	"close_ports": {
		"created": "delete", "creating": "delete", "updating": "delete", "deleting": "delete",
		"error": "delete", "error_creating": "delete", "error_deleting": "delete",
	},
}

func init() { addStateRows(stateRowsOf, processPortStateRows) }

func TestProcessPortStates(t *testing.T) {
	for tool, rows := range processPortStateRows {
		for state, action := range rows {
			t.Run(tool+"/"+state, func(t *testing.T) {
				const source = "10.0.0.0/8"
				target := provision.SGRule{ID: faultID(0x71), Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: source, State: state}
				neighbor := target
				neighbor.ID, neighbor.IPProtocol, neighbor.State = faultID(0x72), "udp", "created"
				cloud := &faultCloud{rules: []provision.SGRule{target, neighbor}}
				r, _ := faultToolsRegistry(t, cloud)
				args := map[string]any{"security_group": faultSG, "ports": []any{float64(8080)}, "cidr": source}
				initial := cloudWrites(cloud)
				m, text, failed := callNamed(t, r, tool, args)
				if !failed || m["status"] != "confirmation_required" || cloudWrites(cloud) != initial {
					t.Fatalf("preview: %s, writes %d", text, cloudWrites(cloud)-initial)
				}
				if action == "delete" {
					rules, _ := m["rules"].([]any)
					if len(rules) != 1 {
						t.Fatalf("want one addressed rule: %s", text)
					}
					row, _ := rules[0].(map[string]any)
					if row["id"] != target.ID || row["cidr"] != source {
						t.Fatalf("wrong rule: %s", text)
					}
				} else {
					key := "will_open"
					if action == "covered" {
						key = "already_open"
					}
					ports, _ := m[key].([]any)
					if len(ports) != 1 || ports[0] != "8080" {
						t.Fatalf("expected port in %s: %s", key, text)
					}
				}
				args["confirmed"] = true
				m, text, failed = callNamed(t, r, tool, args)
				wantWrites := 1
				if action == "covered" {
					wantWrites = 0
				}
				if failed || m["status"] != "ok" || cloudWrites(cloud)-initial != wantWrites {
					t.Fatalf("after consent: %s, writes %d, want %d", text, cloudWrites(cloud)-initial, wantWrites)
				}
				field := "already_open"
				switch action {
				case "add":
					field = "opened"
				case "delete":
					field = "closed"
				}
				ports, _ := m[field].([]any)
				if len(ports) != 1 || ports[0] != "8080" {
					t.Fatalf("expected confirmed port in %s: %s", field, text)
				}
				final := cloud.liveRules()
				if !slices.Contains(final, neighbor) || slices.Contains(final, target) != (action != "delete") {
					t.Fatalf("target or neighboring rule changed unexpectedly: %+v", final)
				}
				if action == "add" {
					if len(final) != 3 || !slices.ContainsFunc(final, func(rule provision.SGRule) bool {
						return rule.ID != target.ID && rule.ID != neighbor.ID && rule.IPProtocol == "tcp" && rule.PortRange == target.PortRange && rule.RemoteIPPrefix == source
					}) {
						t.Fatalf("missing new rule: %+v", final)
					}
				}
			})
		}
	}
}
