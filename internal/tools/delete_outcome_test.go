// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// deleteOutcomeTools are the deletes of D74 that read the operation they
// start (as dns_zone_delete does, D66); ca_delete has no operation to read
// (the Container Apps API publishes none) and waits for its app instead.
var deleteOutcomeTools = []string{"ar_delete", "dns_record_delete", "k8s_delete", "k8s_nodepool_delete", "kafka_delete", "lb_delete",
	"tg_delete", "pg_delete", "redis_delete", "vpc_delete"}

// D74 (the owner's decision 03.11; cloud-e2e run 151, D66): an async delete
// answered deleting with its operation id and read nothing back — an
// operation that failed looked like a delete under way, exit 0. Each of
// the ten reads the outcome within the wait of an operation: an error is a
// refusal with Cloud.ru's text (API_ERROR), done is deleted.
func TestDeleteOutcome_OperationErrorIsARefusal(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	failed := `{"items":[],"total":0,"id":"` + contractID + `","clusterId":"` + contractID + `","name":"web","status":"ACTIVE","done":true,` +
		`"error":{"code":9,"message":"the platform kept it"}}`
	for _, name := range deleteOutcomeTools {
		t.Run(name, func(t *testing.T) {
			rt := &contractRT{kind: "200", body: failed}
			reg := contractReg(t, rt)
			tl, _ := reg.Lookup(name)
			args := contractArgs(tl)
			args["confirmed"] = true
			delete(args, "wait") // the default: wait for the outcome
			text, isErr, _ := contractCall(tl, args, 5*time.Second)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(text, "the platform kept it") {
				t.Errorf("a failed operation of the delete: want API_ERROR with the platform's text, got %.500s", text)
			}

			rt = &contractRT{kind: "200"} // done: true, no error
			reg = contractReg(t, rt)
			tl, _ = reg.Lookup(name)
			text, isErr, _ = contractCall(tl, args, 5*time.Second)
			m = nil
			_ = json.Unmarshal([]byte(text), &m)
			if isErr || m["status"] != "deleted" || m["operation_id"] == nil {
				t.Errorf("a done operation of the delete: want status deleted with operation_id, got %.500s", text)
			}

			// Not done in the wait: deleting, and the tool that shows the end.
			wait := deleteOutcomeWait
			deleteOutcomeWait = 0
			defer func() { deleteOutcomeWait = wait }()
			rt = &contractRT{kind: "200", body: `{"items":[],"total":0,"id":"` + contractID + `","clusterId":"` + contractID + `","name":"web","status":"ACTIVE","done":false}`}
			reg = contractReg(t, rt)
			tl, _ = reg.Lookup(name)
			text, isErr, _ = contractCall(tl, args, 5*time.Second)
			m = nil
			_ = json.Unmarshal([]byte(text), &m)
			next, _ := m["next_tool"].(string)
			if _, ok := reg.Lookup(next); isErr || m["status"] != "deleting" || !ok || m["next"] == nil {
				t.Errorf("an operation not done in the wait: want deleting with a next_tool of the registry, got %.500s", text)
			}
		})
	}
}

// ca_delete (D74): the Container Apps API has no operation to read, so the
// delete reads the app until it is gone — gone is deleted, still there after
// the wait is deleting with ca_get as the next step.
func TestDeleteOutcome_ContainerAppReadsTheApp(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	for _, c := range []struct {
		name      string
		goneAfter int32 // the GET of the app after the DELETE that is a 404; 0 for never
		wait      int
		status    string
	}{
		{"gone", 2, 30, "deleted"},
		{"still there", 0, 0, "deleting"},
	} {
		var deleted atomic.Bool
		var reads atomic.Int32
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Host != "containers.api.cloud.ru" || r.URL.Path != "/v2/containers/web" {
				return false
			}
			switch {
			case r.Method == http.MethodDelete:
				deleted.Store(true)
				_, _ = io.WriteString(w, `{"id":"`+contractID+`","done":false}`)
			case deleted.Load() && c.goneAfter > 0 && reads.Add(1) >= c.goneAfter:
				http.Error(w, `{"message":"container not found"}`, http.StatusNotFound)
			default:
				_, _ = io.WriteString(w, `{"name":"web","status":"DELETING"}`)
			}
			return true
		}}
		wait := deleteOutcomeWait
		deleteOutcomeWait = c.wait
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, "ca_delete", map[string]any{"name": "web", "confirmed": true})
		deleteOutcomeWait = wait
		if isErr || m["status"] != c.status || m["name"] != "web" || m["operation_id"] != contractID {
			t.Errorf("%s: want %s with name and operation_id, got %s", c.name, c.status, text)
		}
		if c.status == "deleting" && m["next_tool"] != "ca_get" {
			t.Errorf("%s: want ca_get as the next step, got %s", c.name, text)
		}
	}
}

// D74, the owner's decision 03.11 (a script deleting many must keep its
// speed): wait=false of the ten and of dns_zone_delete (D66, on the same
// point) answers deleting with the operation_id as soon as the
// cloud took the DELETE, reading nothing after it — the same argument wait
// as provision and deploy_apply, in the schema of each of the ten, so the
// CLI has --wait=false.
func TestDeleteOutcome_WaitFalseDoesNotWait(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	for _, name := range append([]string{"ca_delete", "dns_zone_delete"}, deleteOutcomeTools...) {
		t.Run(name, func(t *testing.T) {
			rt := &contractRT{kind: "200"} // the operation is done: a read would answer deleted
			reg := contractReg(t, rt)
			tl, _ := reg.Lookup(name)
			if _, ok := schemaProps(tl)["wait"]; !ok {
				t.Fatalf("%s: no argument wait in the schema", name)
			}
			args := contractArgs(tl)
			args["confirmed"], args["wait"] = true, false
			text, isErr, _ := contractCall(tl, args, 5*time.Second)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if isErr || m["status"] != "deleting" || m["operation_id"] == nil || m["next_tool"] == nil {
				t.Errorf("wait=false: want deleting with operation_id and the next step, got %.500s", text)
			}
			rt.mu.Lock()
			defer rt.mu.Unlock()
			for i, req := range rt.reqs {
				if strings.HasPrefix(req, "DELETE ") && len(rt.reqs) > i+1 {
					t.Errorf("wait=false: requests after the DELETE: %v", rt.reqs[i+1:])
				}
			}
		})
	}
}
