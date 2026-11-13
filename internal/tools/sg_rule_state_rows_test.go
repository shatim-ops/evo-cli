// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// sgRuleRow is what a tool of the rules of a security group answers for a
// rule in a state — a row of its table «state → answer».
type sgRuleRow string

const (
	// The preview and the question; the write once, after the yes.
	sgRowAsks sgRuleRow = "asks"
	// As sgRowAsks, and the preview says the state: the warning of a
	// delete the platform may refuse, the failed rule a create names.
	sgRowAsksNamed sgRuleRow = "asks and names the state"
	// The change is made: already_exists with the rule's id, no question,
	// no write.
	sgRowAlready sgRuleRow = "already"
	// The change is under way: the state as the status, no question, no
	// write.
	sgRowUnderWay sgRuleRow = "under way"
	// USAGE that names the state, no question, no write.
	sgRowRefused sgRuleRow = "refused"
)

// sgRuleStateRows is the table «state of the rule → answer» of the tools
// that change the rules of a security group, a world per row (release
// plan 1.9, «мир несёт каждое состояние из спецификации»; the journal of
// 1.9, the escaped defect 4: the world had a rule only as there and gone).
// For sg_rule_delete the rule is the one the call names; for
// sg_rule_create it is a rule of the group with the fields of the call.
// The states are the specification's (compute:SecurityGroupRuleState,
// scripts/api-contract-states.json):
// TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec fails for a state
// with no row here. The rows are what evo answers; what Cloud.ru does with
// the write in the state is a run's to say, not this table's.
var sgRuleStateRows = map[string]map[string]sgRuleRow{
	"sg_rule_delete": {
		"created":        sgRowAsks,
		"updating":       sgRowAsks,
		"error_creating": sgRowAsksNamed,
		"error":          sgRowAsksNamed,
		"error_deleting": sgRowAsksNamed,
		"creating":       sgRowRefused,
		"deleting":       sgRowUnderWay,
	},
	"sg_rule_create": {
		"created":        sgRowAlready,
		"updating":       sgRowAlready,
		"creating":       sgRowUnderWay,
		"deleting":       sgRowAsksNamed,
		"error":          sgRowAsksNamed,
		"error_creating": sgRowAsksNamed,
		"error_deleting": sgRowAsksNamed,
	},
}

func init() { addStateRows(stateRowsOf, sgRuleStateRows) }

// sgRuleStateCalls are the calls of the table: each names the rule
// sgRuleWeb of the world, by its id or by its fields.
var sgRuleStateCalls = map[string]map[string]any{
	"sg_rule_delete": {"security_group": sgZoneGroup, "rule_id": sgRuleWeb},
	"sg_rule_create": {"security_group": sgZoneGroup, "direction": "ingress", "protocol": "tcp", "ports": "443:443",
		"cidr": "0.0.0.0/0", allowInternetArg: true},
}

func TestSGRules_EveryStateOfTheTableHasAWorld(t *testing.T) {
	for tool, rows := range sgRuleStateRows {
		for state, row := range rows {
			for _, confirmed := range []bool{false, true} {
				w := newSGMakeWorld()
				w.stuck = true // a rule being deleted stays in the group
				w.setRule(sgZoneGroup, sgRuleWeb, func(r *provision.SGRule) { r.State = state })
				m, text, isErr := callNamed(t, sgMakeRegistry(t, w), tool, withConfirmed(sgRuleStateCalls[tool], confirmed))
				preview, _ := m["preview"].(map[string]any)
				if preview == nil {
					preview = m
				}
				// A delete names the state in its warning, a create names the
				// failed rule by its id.
				warning, _ := preview["warning"].(string)
				failed, _ := json.Marshal(preview["failed_rules"])
				// A rule going away is named apart from a failed one: the
				// row of deleting is not kept green by failed_rules.
				going, _ := json.Marshal(preview["deleting_rules"])
				says := strings.Contains(warning, "in "+state+":")
				if state == "deleting" {
					says = says || strings.Contains(string(going), sgRuleWeb)
				} else {
					says = says || strings.Contains(string(failed), sgRuleWeb)
				}
				named := warning + " " + string(failed) + " " + string(going)
				asked := m["status"] == "confirmation_required"
				wantWrites := 0
				var problem string
				switch row {
				case sgRowAsks, sgRowAsksNamed:
					if confirmed {
						wantWrites = 1
						if isErr {
							problem = "the write after the yes is an error"
						}
					} else if !asked {
						problem = "no question"
					} else if (row == sgRowAsksNamed) != says {
						problem = "the state in the preview: " + named
					}
				case sgRowAlready:
					if isErr || m["status"] != "already_exists" || m["rule_id"] != sgRuleWeb {
						problem = "not already_exists with the rule's id"
					}
				case sgRowUnderWay:
					if isErr || m["status"] != state {
						problem = "not the state as the status"
					}
				case sgRowRefused:
					if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) {
						problem = "not a USAGE that names the state"
					}
				}
				if problem != "" {
					t.Errorf("%s of a rule in %s (%s), confirmed=%v: %s: %s", tool, state, row, confirmed, problem, text)
				}
				if n := w.added() + w.deleted(); n != wantWrites {
					t.Errorf("%s of a rule in %s (%s), confirmed=%v: %d writes, want %d: %s", tool, state, row, confirmed, n, wantWrites, text)
				}
			}
		}
	}
}
