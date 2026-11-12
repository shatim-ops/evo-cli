// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// poolRow is what a tool of a node pool answers for a pool in a status —
// a row of its table «status → answer».
type poolRow string

const (
	// The preview and the question; one write after the yes.
	poolRowAsks poolRow = "asks"
	// Another change is under way: waited for, then USAGE «until the
	// status settles»; no question, no write.
	poolRowSettles poolRow = "waits for the status to settle"
	// Not running and not on its way to: USAGE at the first read — evo adds
	// no billable nodes to it; no question, no write.
	poolRowNotRunning poolRow = "refused: not running"
	// Being deleted or deleted: USAGE «nothing to scale»; no question, no
	// write.
	poolRowGoing poolRow = "refused: nothing to scale"
	// The platform is deleting it: deleting, no question, no DELETE (D61).
	poolRowUnderWay poolRow = "under way"
	// It is deleted: already_deleted, no question, no DELETE (D26).
	poolRowAlready poolRow = "already"
)

// poolStateRows is the table «status of the node pool → answer» of the
// tools that change a pool, a world per row (release plan 1.9, «мир несёт
// каждое состояние из спецификации»; D99). The statuses are the
// specification's, letter for letter
// (mk8s:cloudru.containers.mk8s.v3.NodePool.status,
// scripts/api-contract-states.json):
// TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec fails for a status
// with no row here. The rows are what evo answers; what Cloud.ru does with
// the write in the status is a run's to say, not this table's.
var poolStateRows = map[string]map[string]poolRow{
	"k8s_nodepool_scale": {
		"OBJECT_STATUS_PENDING":      poolRowSettles,
		"OBJECT_STATUS_PROVISIONING": poolRowSettles,
		"OBJECT_STATUS_RUNNING":      poolRowAsks,
		"OBJECT_STATUS_RESUMING":     poolRowSettles,
		"OBJECT_STATUS_DELETING":     poolRowGoing,
		"OBJECT_STATUS_ERROR":        poolRowNotRunning,
		"OBJECT_STATUS_SCALING_UP":   poolRowSettles,
		"OBJECT_STATUS_SCALING_DOWN": poolRowSettles,
		"OBJECT_STATUS_STOPPING":     poolRowSettles,
		"OBJECT_STATUS_STOPPED":      poolRowNotRunning,
		"OBJECT_STATUS_SUSPENDING":   poolRowSettles,
		"OBJECT_STATUS_SUSPENDED":    poolRowNotRunning,
		"OBJECT_STATUS_UPGRADING":    poolRowSettles,
		"OBJECT_STATUS_EDITING":      poolRowSettles,
		"OBJECT_STATUS_ADDING":       poolRowSettles,
		"OBJECT_STATUS_DELETED":      poolRowGoing,
	},
	"k8s_nodepool_delete": {
		"OBJECT_STATUS_PENDING":      poolRowAsks,
		"OBJECT_STATUS_PROVISIONING": poolRowAsks,
		"OBJECT_STATUS_RUNNING":      poolRowAsks,
		"OBJECT_STATUS_RESUMING":     poolRowAsks,
		"OBJECT_STATUS_DELETING":     poolRowUnderWay,
		"OBJECT_STATUS_ERROR":        poolRowAsks,
		"OBJECT_STATUS_SCALING_UP":   poolRowAsks,
		"OBJECT_STATUS_SCALING_DOWN": poolRowAsks,
		"OBJECT_STATUS_STOPPING":     poolRowAsks,
		"OBJECT_STATUS_STOPPED":      poolRowAsks,
		"OBJECT_STATUS_SUSPENDING":   poolRowAsks,
		"OBJECT_STATUS_SUSPENDED":    poolRowAsks,
		"OBJECT_STATUS_UPGRADING":    poolRowAsks,
		"OBJECT_STATUS_EDITING":      poolRowAsks,
		"OBJECT_STATUS_ADDING":       poolRowAsks,
		"OBJECT_STATUS_DELETED":      poolRowAlready,
	},
}

func TestK8sNodePool_EveryStatusOfTheTableHasAWorld(t *testing.T) {
	for tool, rows := range poolStateRows {
		for status, row := range rows {
			for _, confirmed := range []bool{false, true} {
				w := &poolWorld{status: status, others: []string{"OBJECT_STATUS_RUNNING"}}
				w.setCount(2)
				var m map[string]any
				var text string
				var isErr bool
				if tool == "k8s_nodepool_scale" {
					m, text, isErr = scale(t, w, 3, confirmed)
				} else {
					m, text, isErr = poolDelete(t, w, confirmed)
				}
				usage := func(words string) string {
					if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, status) || !strings.Contains(text, words) {
						return "not a USAGE that names the status and says «" + words + "»"
					}
					return ""
				}
				wantWrites := 0
				var problem string
				switch row {
				case poolRowAsks:
					if confirmed {
						wantWrites = 1
						if isErr {
							problem = "the write after the yes is an error"
						}
					} else if m["status"] != "confirmation_required" {
						problem = "no question"
					}
				case poolRowSettles:
					if problem = usage("until the status settles"); problem == "" && len(w.reqs) < 3 {
						problem = "did not wait for the status"
					}
				case poolRowNotRunning:
					problem = usage("not running")
				case poolRowGoing:
					problem = usage("nothing to scale")
				case poolRowUnderWay:
					if isErr || m["status"] != "deleting" {
						problem = "not deleting"
					}
				case poolRowAlready:
					if isErr || m["status"] != "already_deleted" {
						problem = "not already_deleted"
					}
				}
				if problem != "" {
					t.Errorf("%s of a pool in %s (%s), confirmed=%v: %s: %s", tool, status, row, confirmed, problem, text)
				}
				if n := len(w.writes()); n != wantWrites {
					t.Errorf("%s of a pool in %s (%s), confirmed=%v: %d writes, want %d: %v", tool, status, row, confirmed, n, wantWrites, w.writes())
				}
			}
		}
	}
}
