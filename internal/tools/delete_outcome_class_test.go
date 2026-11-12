// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// noOperationOutcome are the deletes whose answer carries an operation_id
// but that have no operation to read, with the reason and the test that
// holds their outcome instead.
var noOperationOutcome = map[string]string{
	"ca_delete": "the Container Apps API publishes no operation; the delete reads the app until it is gone (TestDeleteOutcome_ContainerAppReadsTheApp)",
}

// D74 as a class (cloud-e2e run 151: an async delete answered deleting
// and read nothing back, so a failed operation looked like a delete under
// way, exit 0). Every delete of deleteTargets whose answer names the
// operation it started reads that operation through deleteOutcome: a
// failed operation is a refusal with Cloud.ru's text, not a success. The
// class is taken from the registry, not from a list: a new async delete
// that answers before its operation ends fails here, and one that reads it
// must be in deleteOutcomeTools (or be dns_zone_delete, D66), so that the
// tests of D74 — done is deleted, not done is deleting with its next step,
// wait=false answers at once — cover it too.
func TestDeleteOutcome_EveryAsyncDeleteReadsItsOperation(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	failed := `{"items":[],"total":0,"id":"` + contractID + `","clusterId":"` + contractID + `","name":"web","status":"ACTIVE","done":true,` +
		`"error":{"code":9,"message":"the platform kept it"}}`
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	async := 0
	for _, name := range names {
		if _, ok := noOperationOutcome[name]; ok {
			continue
		}
		reg := contractReg(t, &contractRT{kind: "200"}) // done: true, no error
		tl, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("deleteTargets names %s, which is not a tool of the registry", name)
			continue
		}
		args := contractArgs(tl)
		args["confirmed"] = true
		delete(args, "wait") // the default: wait for the outcome
		text, _, _ := contractCall(tl, args, 5*time.Second)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if _, ok := m["operation_id"]; !ok {
			continue // a delete the cloud ends in its answer: nothing to read
		}
		async++
		reg = contractReg(t, &contractRT{kind: "200", body: failed})
		tl, _ = reg.Lookup(name)
		text, isErr, _ := contractCall(tl, args, 5*time.Second)
		m = nil
		_ = json.Unmarshal([]byte(text), &m)
		if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(text, "the platform kept it") {
			t.Errorf("%s answers an operation_id but a failed operation of the delete is not a refusal with the platform's text (read it through deleteOutcome, D74): %.500s", name, text)
		}
		if name != "dns_zone_delete" && !slices.Contains(deleteOutcomeTools, name) {
			t.Errorf("%s reads the operation of its delete but is not in deleteOutcomeTools: the tests of D74 (done, not done, wait=false) do not cover it", name)
		}
	}
	for name := range noOperationOutcome {
		if _, ok := deleteTargets[name]; !ok {
			t.Errorf("noOperationOutcome names %s, which deleteTargets does not declare", name)
		}
	}
	if async == 0 {
		t.Error("no delete of deleteTargets answers an operation_id: the world of the contract does not reach the deletes")
	}
}
