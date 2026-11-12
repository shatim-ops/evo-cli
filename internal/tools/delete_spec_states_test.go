// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// delRow is what a delete answers for a resource in a state — a row of
// its table «state → answer».
type delRow string

const (
	// The preview and the question; one DELETE after the yes.
	delRowAsks delRow = "asks"
	// The platform is deleting it: deleting, no question, no DELETE (D61).
	delRowUnderWay delRow = "under way"
	// It is deleted: already_deleted, no question, no DELETE (D26).
	delRowAlready delRow = "already"
)

// deleteSpecStateRows is the table «state → answer» of the deletes whose
// service writes the state with a prefix, a world per row. The states are
// the specification's, letter for letter (scripts/api-contract-states.json):
// the rule D61 did not hold for these three because the world of the test
// gave the state as the test's author wrote it, not as the service does
// (the journal of 1.9, the escaped defect 1; D103).
// TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec fails for a state
// of the specification with no row here. The rows are what evo answers; a
// state with no rule of its own yet — being created, failed — asks, as
// D103 leaves it until a run shows the platform's answer.
var deleteSpecStateRows = map[string]map[string]delRow{
	"k8s_delete": {
		"OBJECT_STATUS_PENDING":      delRowAsks,
		"OBJECT_STATUS_PROVISIONING": delRowAsks,
		"OBJECT_STATUS_RUNNING":      delRowAsks,
		"OBJECT_STATUS_RESUMING":     delRowAsks,
		"OBJECT_STATUS_DELETING":     delRowUnderWay,
		"OBJECT_STATUS_ERROR":        delRowAsks,
		"OBJECT_STATUS_SCALING_UP":   delRowAsks,
		"OBJECT_STATUS_SCALING_DOWN": delRowAsks,
		"OBJECT_STATUS_STOPPING":     delRowAsks,
		"OBJECT_STATUS_STOPPED":      delRowAsks,
		"OBJECT_STATUS_SUSPENDING":   delRowAsks,
		"OBJECT_STATUS_SUSPENDED":    delRowAsks,
		"OBJECT_STATUS_UPGRADING":    delRowAsks,
		"OBJECT_STATUS_EDITING":      delRowAsks,
		"OBJECT_STATUS_ADDING":       delRowAsks,
		"OBJECT_STATUS_DELETED":      delRowAlready,
	},
	"lb_delete": {
		"NLB_STATUS_UNSPECIFIED":      delRowAsks,
		"NLB_STATUS_CREATING":         delRowAsks,
		"NLB_STATUS_ERROR_CREATING":   delRowAsks,
		"NLB_STATUS_RUNNING":          delRowAsks,
		"NLB_STATUS_UPDATING":         delRowAsks,
		"NLB_STATUS_ERROR_UPDATING":   delRowAsks,
		"NLB_STATUS_DELETING":         delRowUnderWay,
		"NLB_STATUS_ERROR_DELETING":   delRowAsks,
		"NLB_STATUS_DELETED":          delRowAlready,
		"NLB_STATUS_SUSPENDING":       delRowAsks,
		"NLB_STATUS_SUSPENDED":        delRowAsks,
		"NLB_STATUS_ERROR_SUSPENDING": delRowAsks,
		"NLB_STATUS_RESUMING":         delRowAsks,
		"NLB_STATUS_ERROR_RESUMING":   delRowAsks,
	},
	"tg_delete": {
		"TARGET_GROUP_STATUS_UNSPECIFIED":    delRowAsks,
		"TARGET_GROUP_STATUS_CREATING":       delRowAsks,
		"TARGET_GROUP_STATUS_ERROR_CREATING": delRowAsks,
		"TARGET_GROUP_STATUS_CREATED":        delRowAsks,
		"TARGET_GROUP_STATUS_UPDATING":       delRowAsks,
		"TARGET_GROUP_STATUS_ERROR_UPDATING": delRowAsks,
		"TARGET_GROUP_STATUS_DELETING":       delRowUnderWay,
		"TARGET_GROUP_STATUS_ERROR_DELETING": delRowAsks,
		"TARGET_GROUP_STATUS_DELETED":        delRowAlready,
	},
}

func init() { addStateRows(stateRowsOf, deleteSpecStateRows) }

func TestDelete_EveryStateOfTheSpecHasAWorld(t *testing.T) {
	for name, rows := range deleteSpecStateRows {
		for status, row := range rows {
			for _, confirmed := range []bool{false, true} {
				rt := &contractRT{kind: "200", body: `{"items":[],"total":0,"id":"` + contractID + `","name":"web","status":"` + status + `","done":true}`}
				tl, ok := contractReg(t, rt).Lookup(name)
				if !ok {
					t.Fatalf("deleteSpecStateRows names %s, which is no tool", name)
				}
				args := contractArgs(tl)
				args["confirmed"] = confirmed
				text, isErr, _ := contractCall(tl, args, 5*time.Second)
				var m map[string]any
				_ = json.Unmarshal([]byte(text), &m)
				writes := 0
				for _, q := range rt.reqs {
					if !strings.HasPrefix(q, "GET ") {
						writes++
					}
				}
				wantWrites := 0
				var problem string
				switch row {
				case delRowAsks:
					if confirmed {
						wantWrites = 1
						if isErr {
							problem = "the delete after the yes is an error"
						}
					} else if m["status"] != "confirmation_required" {
						problem = "no question"
					}
				case delRowUnderWay:
					if isErr || m["status"] != "deleting" || m["already_deleting"] != true || m["resource_status"] != status {
						problem = "not deleting with already_deleting and the status as the service writes it"
					}
				case delRowAlready:
					if isErr || m["status"] != "already_deleted" {
						problem = "not already_deleted"
					}
				}
				if problem != "" {
					t.Errorf("%s of a resource in %s (%s), confirmed=%v: %s: %s", name, status, row, confirmed, problem, text)
				}
				if writes != wantWrites {
					t.Errorf("%s of a resource in %s (%s), confirmed=%v: %d writes, want %d: %v", name, status, row, confirmed, writes, wantWrites, rt.reqs)
				}
			}
		}
	}
}
