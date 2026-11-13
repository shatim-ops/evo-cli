// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"strings"
	"testing"
)

// A cluster that goes FAILED seconds after an accepted create names its
// cause only in the create operation (run 203 for PostgreSQL, run 214 for
// Kafka: the card had no cause field). dbaas-ready.sh reads that operation
// by $OP_ID, so every job of cloud-e2e that waits for a DBaaS cluster with
// it exports the create's operation_id before the wait; without it the
// step printed «id операции нет — не читаю» and the cause stayed a guess.
func TestWorkflows_DBaaSReadyKnowsTheCreateOperation(t *testing.T) {
	jobs := readWorkflows(t)["cloud-e2e.yml"].Jobs
	found := 0
	for name, j := range jobs {
		exported := false
		for _, s := range j.Steps {
			// The id is the create's own answer, read in the same step.
			if strings.Contains(s.Run, `"OP_ID=$op" >> "$GITHUB_ENV"`) && strings.Contains(s.Run, `op=$(jq -r '.operation_id // empty' "$RUNNER_TEMP/create.json"`) {
				exported = true
			}
			if !strings.Contains(s.Run, "dbaas-ready.sh") {
				continue
			}
			found++
			if !exported {
				t.Errorf("cloud-e2e job %s: step %q waits for the cluster, but no step before it exports OP_ID of the create", name, s.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("no step of cloud-e2e.yml calls dbaas-ready.sh: the test checks nothing")
	}
}
