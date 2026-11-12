// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The source of a rule of a security group (the journal of 1.9, the
// escaped defect 2): sgrules.Coverage read a rule whose source is another
// group as «from anywhere», and open_ports and deploy called its port
// open. The worlds of the rules had no such rule: every rule of every
// test had a prefix. The kinds of source are the specification's — the
// fields remote_* of a rule in the answer of the list of rules, as the
// baseline of the fields holds them (scripts/api-contract-fields.json; the
// job contract fails for a new one until evo reads it) — and a rule with
// none of them. Every tool that decides by the rules of a group has a row
// for each kind, and a world runs it; deploy, provision and deploy_apply
// have theirs in the decision they share (sgOpenOfDeploy).

// sgRulesRequest is the request of the baseline whose answer is the rules.
const sgRulesRequest = "compute GET /api/v1/security-groups/{security_group_id}/rules"

// sgSourceNone is the kind of a rule that names no source.
const sgSourceNone = "no source"

// sgSourceInWorld puts a source of the kind on a rule of the world: the
// prefix the calls of the table ask for, or the zone's group.
var sgSourceInWorld = map[string]func(*provision.SGRule){
	"remote_ip_prefix": func(r *provision.SGRule) { r.RemoteIPPrefix = sgSourcePrefix },
	"remote_security_group": func(r *provision.SGRule) {
		r.RemoteSecurityGroup = &provision.SGRuleGroup{ID: sgZoneGroup, Name: sgSourceGroupName}
	},
	sgSourceNone: func(*provision.SGRule) {},
}

const (
	sgSourcePrefix    = "10.0.0.0/8"
	sgSourceGroupName = "SSH-access_ru.AZ-1"
)

// sgSourceRow is what a tool says of a port, or of a rule, whose only rule
// has a source of a kind.
type sgSourceRow string

const (
	// open_ports: the port is open to the source asked for; no rule added.
	srcOpen sgSourceRow = "open"
	// open_ports: the port is not called open — only the group gets
	// through; named in open_only_to_groups; no rule added (owner's
	// decision 12.11, variant «б»).
	srcOnlyGroups sgSourceRow = "only from groups"
	// sg_rule_create of the rule with the prefix: it is there —
	// already_exists, no write.
	srcSameRule sgSourceRow = "the same rule"
	// sg_rule_create of the rule with the prefix: the rule there is
	// another one — the question, one write.
	srcAnotherRule sgSourceRow = "another rule"
	// sg_rule_delete: the preview names the source as the prefix, as the
	// group, as anywhere.
	srcNamesPrefix   sgSourceRow = "names the prefix"
	srcNamesGroup    sgSourceRow = "names the group"
	srcNamesAnywhere sgSourceRow = "names anywhere"
)

// sgSourceRows is the table «kind of source → answer». A closed list by
// the kinds of the specification: a kind with no row fails the test.
// The rows of sgSourceNone are no decision: the three tools read such a
// rule three ways, and each way rests on what the platform answers for a
// rule «from anywhere» — not verified live. They wait for the runs of Г-11
// (release plan 1.9), and change with the one reading of a rule's source.
var sgSourceRows = map[string]map[string]sgSourceRow{
	"open_ports": {
		"remote_ip_prefix":      srcOpen,
		"remote_security_group": srcOnlyGroups,
		// A rule that names no source counts as before D52
		// (sgrules.Coverage): what the API sends for «from anywhere» is
		// not verified live.
		sgSourceNone: srcOpen,
	},
	"sg_rule_create": {
		"remote_ip_prefix":      srcSameRule,
		"remote_security_group": srcAnotherRule,
		sgSourceNone:            srcAnotherRule,
	},
	"sg_rule_delete": {
		"remote_ip_prefix":      srcNamesPrefix,
		"remote_security_group": srcNamesGroup,
		sgSourceNone:            srcNamesAnywhere,
	},
	sgOpenOfDeploy: {
		"remote_ip_prefix":      srcOpen,
		"remote_security_group": srcOnlyGroups,
		sgSourceNone:            srcOpen,
	},
}

// sgOpenOfDeploy stands for deploy, provision and deploy_apply: the three
// open their ports by sgrules.Open, the one decision of what a rule there
// already lets in — the row runs it, not a deployment.
const sgOpenOfDeploy = "deploy (sgrules.Open)"

// sgSourceCalls are the calls of the table: the port 8080 of the group
// sgOtherGroup, whose one rule for it is sgRuleNone.
var sgSourceCalls = map[string]map[string]any{
	"open_ports":     {"security_group": sgOtherGroup, "ports": []any{float64(8080)}, "cidr": sgSourcePrefix},
	"sg_rule_create": {"security_group": sgOtherGroup, "direction": "ingress", "protocol": "tcp", "ports": "8080", "cidr": sgSourcePrefix},
	"sg_rule_delete": {"security_group": sgOtherGroup, "rule_id": sgRuleNone},
}

// sgSourceKindsOfTheSpec are the fields remote_* of a rule in the answer
// of the list of rules, and sgSourceNone.
func sgSourceKindsOfTheSpec(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "api-contract-fields.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Requests map[string][]string `json:"requests"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	kinds := []string{sgSourceNone}
	for _, f := range file.Requests[sgRulesRequest] {
		if name, ok := strings.CutPrefix(f, "items[].remote_"); ok && !strings.ContainsAny(name, ".[") {
			kinds = append(kinds, "remote_"+name)
		}
	}
	if len(kinds) < 3 {
		t.Fatalf("scripts/api-contract-fields.json: %q names %d kinds of source, want the prefix and the group at least: the request or its fields were renamed", sgRulesRequest, len(kinds)-1)
	}
	sort.Strings(kinds)
	return kinds
}

func TestSGRules_EveryKindOfSourceHasAWorld(t *testing.T) {
	kinds := sgSourceKindsOfTheSpec(t)
	known := map[string]bool{}
	for _, kind := range kinds {
		known[kind] = true
		if sgSourceInWorld[kind] == nil {
			t.Errorf("the specification's rule names its source in %s, and the world cannot carry it: add it to sgSourceInWorld, and a row to every tool of sgSourceRows", kind)
		}
	}
	for kind := range sgSourceInWorld {
		if !known[kind] {
			t.Errorf("sgSourceInWorld has %s, which the baseline of the fields no longer names: remove it", kind)
		}
	}
	for tool, rows := range sgSourceRows {
		for kind := range rows {
			if !known[kind] {
				t.Errorf("%s: a row for %s, which the baseline of the fields no longer names: remove it", tool, kind)
			}
		}
		for _, kind := range kinds {
			row, set := rows[kind], sgSourceInWorld[kind]
			if row == "" {
				t.Errorf("%s: no row for a rule whose source is %s — say what the tool answers", tool, kind)
				continue
			}
			if set == nil {
				continue
			}
			for _, confirmed := range []bool{false, true} {
				w := newSGMakeWorld()
				rule := provision.SGRule{ID: sgRuleNone, Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "8080:8080", State: "created"}
				set(&rule)
				w.rules[sgOtherGroup] = append(w.rules[sgOtherGroup], rule)
				var m map[string]any
				var text string
				var isErr bool
				if tool == sgOpenOfDeploy {
					// No consent of its own: the deployment's. Read as the
					// answer of a tool, by the names of its fields.
					res := sgrules.Open(context.Background(), w, faultProject, sgOtherGroup, []int{8080}, nil, sgSourcePrefix, "vm-1", &state.DeploymentRecord{})
					b, _ := json.Marshal(res)
					text = string(b)
					_ = json.Unmarshal(b, &m)
				} else {
					m, text, isErr = callNamed(t, sgMakeRegistry(t, w), tool, withConfirmed(sgSourceCalls[tool], confirmed))
				}
				preview, _ := m["preview"].(map[string]any)
				if preview == nil {
					preview = m
				}
				open, _ := json.Marshal(m["already_open"])
				only, _ := json.Marshal(m["open_only_to_groups"])
				shown, _ := preview["rule"].(map[string]any)
				source, _ := shown["source"].(string)
				wantWrites := 0
				var problem string
				switch row {
				case srcOpen:
					if !strings.Contains(string(open), "8080") || m["open_only_to_groups"] != nil {
						problem = "the port is not called open"
					}
				case srcOnlyGroups:
					if strings.Contains(string(open), "8080") || !strings.Contains(string(only), `"8080"`) || !strings.Contains(string(only), sgSourceGroupName) {
						problem = "the port is called open, or the group is not named in open_only_to_groups"
					}
				case srcSameRule:
					if isErr || m["status"] != "already_exists" || m["rule_id"] != sgRuleNone {
						problem = "not already_exists with the rule's id"
					}
				case srcAnotherRule:
					if confirmed {
						wantWrites = 1
					} else if m["status"] != "confirmation_required" {
						problem = "no question"
					}
				case srcNamesPrefix, srcNamesGroup, srcNamesAnywhere:
					want := map[sgSourceRow]string{srcNamesPrefix: sgSourcePrefix, srcNamesGroup: "security group " + sgSourceGroupName, srcNamesAnywhere: "0.0.0.0/0"}[row]
					if confirmed {
						wantWrites = 1
					} else if source != want {
						problem = "the preview names the source as " + source + ", want " + want
					}
				}
				if problem != "" {
					t.Errorf("%s, a rule whose source is %s (%s), confirmed=%v: %s: %s", tool, kind, row, confirmed, problem, text)
				}
				if n := w.added() + w.deleted(); n != wantWrites {
					t.Errorf("%s, a rule whose source is %s (%s), confirmed=%v: %d writes, want %d: %s", tool, kind, row, confirmed, n, wantWrites, text)
				}
			}
		}
	}
}
