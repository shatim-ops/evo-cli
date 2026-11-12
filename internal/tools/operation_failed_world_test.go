// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

// Every *_operation has a world whose operation ended with an error
// (release plan 1.9, «Отказ по квоте в операции создания базы называет
// причину и путь»; escaped to cloud-e2e runs 203 and 208: the worlds of
// the database operations carried no finished operation with an error, so
// no test read what the tool says of one). The class is the registry's:
// every tool named *_operation is called in the world of the contract
// with an operation that is done and failed. Its answer says the
// operation is done, carries the service's error and marks it untrusted
// (R-83) — and a failure by quota names its cause, so that an agent does
// not send the create again into the same quota (one point of the answer
// of *_operation since #1522; a new *_operation that answers past it
// fails here).

// failedOperation is the body of an operation that is done with an error
// of the service's wording.
func failedOperation(message string) string {
	return `{"items":[],"total":0,"id":"` + contractID + `","name":"web","status":"ACTIVE","done":true,` +
		`"error":{"code":8,"message":"` + message + `"}}`
}

func TestOperation_EveryOneHasAWorldOfAFailedOperation(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	var names []string
	for _, tl := range contractReg(t, &contractRT{kind: "200"}).Tools() {
		if strings.HasSuffix(tl.Name, "_operation") {
			names = append(names, tl.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("the registry has no *_operation: the test sees nothing")
	}
	call := func(name, message string) (map[string]any, string, bool) {
		reg := contractReg(t, &contractRT{kind: "200", body: failedOperation(message)})
		tl, _ := reg.Lookup(name)
		text, isErr, _ := contractCall(tl, contractArgs(tl), 5*time.Second)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m, text, isErr
	}
	for _, name := range names {
		if _, ok := operationReads[name]; !ok {
			t.Errorf("%s is a *_operation of the registry that operationReads does not declare: the answer of a finished operation names no read of its resource", name)
		}
		// Any failure: done, the service's text, marked untrusted.
		m, text, _ := call(name, "the platform kept it")
		marked := false
		if list, _ := m["_untrusted"].([]any); list != nil {
			for _, f := range list {
				marked = marked || f == "error"
			}
		}
		if m["done"] != true || !strings.Contains(text, "the platform kept it") || !marked {
			t.Errorf("%s of an operation that is done with an error: want done, the service's text and the error marked untrusted — the world of the contract does not reach the tool, or the answer drops the failure: %.400s", name, text)
			continue
		}
		if d, _ := m["details"].(map[string]any); d["cause"] == "quota" {
			t.Errorf("%s names quota as the cause of a failure that says nothing of one: %.400s", name, text)
		}
		// A failure by quota names its cause.
		m, text, _ = call(name, "acquire_quota failed: quota exceeded")
		details, _ := m["details"].(map[string]any)
		if details["cause"] != "quota" {
			t.Errorf("%s of an operation failed by quota does not name the cause (details.cause \"quota\"): an agent sends the create again into the same quota: %.400s", name, text)
		}
	}
}
