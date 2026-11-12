// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// prefixedDeletes are the deletes of the services that write a status
// with a prefix, and the prefix.
var prefixedDeletes = map[string]string{
	"k8s_delete": "OBJECT_STATUS_",
	"lb_delete":  "NLB_STATUS_",
	"tg_delete":  "TARGET_GROUP_STATUS_",
}

// The rules of every delete compare the names of states (DELETING,
// DELETED), and Managed Kubernetes and the load balancers write them with
// a prefix: a cluster, a balancer or a target group the platform was
// already deleting got the usual preview, and with the consent a second
// DELETE (release plan 1.9, «Правило D61 действует у k8s_delete,
// lb_delete, tg_delete»; D103). Being deleted is
// answered deleting and deleted is answered already_deleted, both with no
// question and no DELETE.
func TestDelete_PrefixedStateOfADeleteUnderWayOrDone(t *testing.T) {
	for name, prefix := range prefixedDeletes {
		for state, want := range map[string]string{"DELETING": "deleting", "DELETED": "already_deleted"} {
			for _, confirmed := range []bool{false, true} {
				status := prefix + state
				rt := &contractRT{kind: "200", body: `{"items":[],"total":0,"id":"` + contractID + `","name":"web","status":"` + status + `","done":true}`}
				reg := contractReg(t, rt)
				tl, _ := reg.Lookup(name)
				args := contractArgs(tl)
				args["confirmed"] = confirmed
				text, isErr, _ := contractCall(tl, args, 5*time.Second)
				var m map[string]any
				_ = json.Unmarshal([]byte(text), &m)
				if isErr || m["status"] != want {
					t.Errorf("%s in %s (confirmed=%v): want status %s with no question: %s", name, status, confirmed, want, text)
				}
				if state == "DELETING" && (m["already_deleting"] != true || m["resource_status"] != status) {
					t.Errorf("%s in %s: want already_deleting and the status as the service writes it: %s", name, status, text)
				}
				for _, q := range rt.reqs {
					if !strings.HasPrefix(q, "GET ") {
						t.Errorf("%s in %s (confirmed=%v): sent %s", name, status, confirmed, q)
					}
				}
			}
		}
	}
}

// What the decision leaves as it was until a run shows the platform's
// answer: a resource still being created and one in a failed state get
// the preview they got — no refusal before the question and no warning
// of a refusal no run has seen.
func TestDelete_PrefixedStateWithNoRuleYet(t *testing.T) {
	for name, prefix := range prefixedDeletes {
		for _, state := range []string{"CREATING", "PROVISIONING", "ERROR", "ERROR_DELETING"} {
			status := prefix + state
			p := New(nil).deletePlan(name, "x_delete deletes %s.", "x_delete удаляет %s.", "id-1", func() (map[string]any, error) {
				return map[string]any{"name": "c1", "status": status}, nil
			}, func(context.Context) (string, bool) { return "", false })
			if p.Execute == nil || p.Preview == nil {
				t.Errorf("%s in %s: answered with no question: %s", name, status, p.answer)
				continue
			}
			if _, warned := p.Preview["warning"]; warned {
				t.Errorf("%s in %s: a warning of a refusal no run has seen: %s", name, status, toJSON(p.Preview))
			}
		}
	}
}
