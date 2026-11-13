// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

func poolDelete(t *testing.T, w *poolWorld, confirmed bool) (map[string]any, string, bool) {
	t.Helper()
	return callNamed(t, poolRegistry(t, w), "k8s_nodepool_delete",
		map[string]any{"nodepool_id": poolFixed, "confirmed": confirmed})
}

// A node pool made by k8s_add_nodepool could not be deleted by evo: the
// only way to stop paying for its nodes was to delete the cluster or to go
// to the console (CLAUDE.md §6, axis 3; D99). k8s_nodepool_delete answers
// everything a read of the pool shows before the question, the same with
// the consent given, and sends nothing.
func TestK8sNodePoolDelete_AnsweredBeforeTheQuestion(t *testing.T) {
	cases := []struct {
		name   string
		world  func() *poolWorld
		isErr  bool
		status string // of the answer, when it is not an error
		reason string
		says   string
	}{
		{name: "the pool is not there: a repeated delete", world: func() *poolWorld { return &poolWorld{gone: true} }, status: "already_deleted"},
		{name: "the platform is deleting it", world: func() *poolWorld { return &poolWorld{status: "OBJECT_STATUS_DELETING"} }, status: "deleting", says: "OBJECT_STATUS_DELETING"},
		{name: "readable after its delete", world: func() *poolWorld { return &poolWorld{status: "OBJECT_STATUS_DELETED"} }, status: "already_deleted"},
		{name: "the pools of its cluster are not read", world: func() *poolWorld { return &poolWorld{status: poolRunning, listFails: true} }, isErr: true, reason: "API_ERROR"},
	}
	for _, c := range cases {
		for _, confirmed := range []bool{false, true} {
			w := c.world()
			w.setCount(2)
			m, text, isErr := poolDelete(t, w, confirmed)
			if isErr != c.isErr || (c.status != "" && m["status"] != c.status) || (c.reason != "" && m["reason"] != c.reason) || !strings.Contains(text, c.says) {
				t.Errorf("%s (confirmed=%v): want isErr=%v status=%q reason=%q naming %q: %s", c.name, confirmed, c.isErr, c.status, c.reason, c.says, text)
			}
			if m["status"] == "confirmation_required" {
				t.Errorf("%s: a question with nothing to consent to: %s", c.name, text)
			}
			if len(w.writes()) != 0 {
				t.Errorf("%s (confirmed=%v): sent %v", c.name, confirmed, w.writes())
			}
		}
	}
}

// The preview names what goes with the pool, as it was read before the
// question: its cluster, its nodes, their flavor and disk (axis 4).
func TestK8sNodePoolDelete_Preview(t *testing.T) {
	w := &poolWorld{status: poolRunning, others: []string{poolRunning}}
	w.setCount(3)
	m, text, _ := poolDelete(t, w, false)
	if m["status"] != "confirmation_required" {
		t.Fatalf("want the preview: %s", text)
	}
	msg, _ := m["message"].(string)
	if !strings.Contains(msg, `"pool-1" (`+poolFixed+`)`) || !strings.Contains(msg, "data on their disks") || !strings.Contains(msg, "evicted") {
		t.Errorf("the message does not name the pool and what goes with it: %s", text)
	}
	will, _ := m["preview"].(map[string]any)
	for k, want := range map[string]any{
		"cluster_id": addonCluster, "cluster_name": "k8s-test", "nodes": float64(3),
		"flavor": "k8s-2-4", "cpu": float64(2), "ram_gb": float64(4), "disk_gb": float64(20), "zone": "ru.AZ-1", "last_pool": false,
	} {
		if will[k] != want {
			t.Errorf("preview.%s is %v, want %v: %s", k, will[k], want, text)
		}
	}
	if _, warned := m["warning"]; warned {
		t.Errorf("a running pool with another one next to it needs no warning: %s", text)
	}
	if len(w.writes()) != 0 {
		t.Errorf("the preview sent %v", w.writes())
	}

	// An autoscaled pool: the platform chooses the number of its nodes, the
	// preview names the bounds.
	auto := &poolWorld{status: poolRunning, policy: `{"autoScale":{"minCount":1,"maxCount":5}}`, others: []string{poolRunning}}
	m, text, _ = poolDelete(t, auto, false)
	will, _ = m["preview"].(map[string]any)
	if will["min_count"] != float64(1) || will["max_count"] != float64(5) || will["nodes"] != nil {
		t.Errorf("an autoscaled pool: want min_count and max_count, no count evo does not know: %s", text)
	}
}

// The last working pool of a cluster: the preview says what the cluster is
// left with. A pool that is going is not one the cluster keeps.
func TestK8sNodePoolDelete_LastPoolOfTheCluster(t *testing.T) {
	for _, others := range [][]string{nil, {"OBJECT_STATUS_DELETING"}} {
		w := &poolWorld{status: poolRunning, others: others}
		w.setCount(1)
		m, text, _ := poolDelete(t, w, false)
		warn, _ := m["warning"].(string)
		will, _ := m["preview"].(map[string]any)
		if m["status"] != "confirmation_required" || will["last_pool"] != true || !strings.Contains(warn, "no node pool") || !strings.Contains(warn, "cluster itself stays") {
			t.Errorf("others %v: want the preview warning the cluster is left with no pool: %s", others, text)
		}
		if strings.Contains(text, "k8s_delete") {
			t.Errorf("others %v: whether to delete the cluster is the person's decision, not a step evo names: %s", others, text)
		}
	}
}

// A pool in error, or in a status evo does not know, is what a cleanup
// meets: not refused — the preview names the status and that the outcome
// is unknown (П4, У4). The warning of a pool in error is the one of every
// delete (engine.DeleteWayOut), by the state without the service's prefix.
func TestK8sNodePoolDelete_StatusWithAnUnknownOutcome(t *testing.T) {
	for st, says := range map[string]string{
		// No run has deleted a pool in error (probe Г-12): the preview says
		// the outcome is unknown, not that Cloud.ru may refuse (FB19I-7).
		poolError:     "the outcome of the delete is unknown",
		poolScalingUp: `"` + poolScalingUp + `", not running: whether the platform accepts the delete is unknown`,
		// Whether the platform deletes a pool it is still creating is not
		// known for Managed Kubernetes: no refusal before the question, the
		// warning of every delete in a state no run has shown.
		"OBJECT_STATUS_PROVISIONING":  `it is "OBJECT_STATUS_PROVISIONING": whether Cloud.ru deletes a node pool in this state no run has shown`,
		"OBJECT_STATUS_SOMETHING_NEW": `"OBJECT_STATUS_SOMETHING_NEW", not running: whether the platform accepts the delete is unknown`,
	} {
		w := &poolWorld{status: st, others: []string{poolRunning}}
		w.setCount(2)
		m, text, _ := poolDelete(t, w, false)
		warn, _ := m["warning"].(string)
		target, _ := m["target"].(map[string]any)
		if m["status"] != "confirmation_required" || !strings.Contains(warn, says) || target["status"] != st {
			t.Errorf("%s: want the preview naming the status and the unknown outcome (%q): %s", st, says, text)
		}
	}
}

// With the consent: one DELETE, and the answer is how its operation ended
// (D74) — deleted, or deleting with k8s_operation as the next step (Н3).
func TestK8sNodePoolDelete_AnswersTheOperation(t *testing.T) {
	w := &poolWorld{status: poolRunning, others: []string{poolRunning}}
	w.setCount(2)
	m, text, isErr := poolDelete(t, w, true)
	if isErr || m["status"] != "deleted" || m["nodepool_id"] != poolFixed || m["operation_id"] != "op-scale" {
		t.Fatalf("want deleted with the pool and the operation: %s", text)
	}
	if got := w.writes(); len(got) != 1 || got[0] != "DELETE /v3/nodePools/"+poolFixed {
		t.Errorf("want one DELETE of the pool, got %v", got)
	}

	// An operation that does not end within the wait of a delete (D74).
	wait := deleteOutcomeWait
	deleteOutcomeWait = 0
	t.Cleanup(func() { deleteOutcomeWait = wait })
	running := &poolWorld{status: poolRunning, others: []string{poolRunning}, opRuns: true}
	running.setCount(2)
	m, text, isErr = poolDelete(t, running, true)
	next, _ := m["next_args"].(map[string]any)
	if isErr || m["status"] != "deleting" || m["next_tool"] != "k8s_operation" || next["operation_id"] != "op-scale" {
		t.Errorf("an operation still running: want deleting and k8s_operation as the next step: %s", text)
	}

	failed := &poolWorld{status: poolRunning, others: []string{poolRunning}, opError: `{"code":9,"message":"the platform kept it"}`}
	failed.setCount(2)
	m, text, isErr = poolDelete(t, failed, true)
	if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(text, "the platform kept it") || m["next_tool"] != nil {
		t.Errorf("a failed operation: want API_ERROR with the platform's text and no next step: %s", text)
	}
}

// The platform refuses the DELETE after the consent: the error with the
// status the pool was read in, and no step of a retry (П6). A pool in
// error: whether Cloud.ru deletes one no run has shown (probe Г-12) — the
// answer does not say «it does not delete it through its API, use the
// console», a word of the platform with no run, on a 500, a 503 or a 409
// alike (FB19I-7).
func TestK8sNodePoolDelete_RequestRefused(t *testing.T) {
	for _, code := range []int{409, 500, 503} {
		w := &poolWorld{status: poolError, others: []string{poolRunning}, del: code}
		w.setCount(2)
		m, text, isErr := poolDelete(t, w, true)
		hint, _ := m["hint"].(string)
		if !isErr || m["reason"] == nil || m["resource_status"] != poolError || m["next_tool"] != nil {
			t.Errorf("%d: want the refusal with the status of the pool, no next step: %s", code, text)
		}
		if strings.Contains(hint, "does not delete") || strings.Contains(hint, "console") {
			t.Errorf("%d: the hint says what the platform does with a pool in error, which no run has shown: %s", code, text)
		}
		if m["status"] == "deleted" || m["status"] == "deleting" {
			t.Errorf("%d: a refusal reads as a success: %s", code, text)
		}
		if got := w.writes(); len(got) != 1 {
			t.Errorf("%d: want one DELETE, got %v", code, got)
		}
		// A 4xx is the platform's refusal of this request: the pool stays,
		// the same call gets the same refusal, the way is support. A 5xx
		// may follow a delete taken: a read tells («Риск», 181, У1).
		refused := code < 500
		if refused && (!strings.Contains(hint, "Cloud.ru refused the delete") || !strings.Contains(hint, "support") || !strings.Contains(hint, poolFixed) || !strings.Contains(hint, "the same call")) {
			t.Errorf("%d: want the refusal said, the same call refused again and support as the way: %s", code, text)
		}
		if !refused && (!strings.Contains(hint, "outcome of the delete is unknown") || !strings.Contains(hint, "k8s_nodepool_get")) {
			t.Errorf("%d: want the outcome unknown and the read that tells: %s", code, text)
		}
		marked := false
		for _, f := range anySlice(m["_untrusted"]) {
			marked = marked || f == "resource_status"
		}
		if !marked {
			t.Errorf("%d: resource_status is the cloud's word and is not marked untrusted: %s", code, text)
		}
	}
}
