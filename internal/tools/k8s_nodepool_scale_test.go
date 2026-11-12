// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// poolWorld is Managed Kubernetes with one node pool (poolFixed) of
// cluster addonCluster: its status and scale policy, what a PATCH of it
// answers, and the requests it got. A PATCH that is accepted sets the
// count and leaves the pool in after (the status the platform puts it in);
// its operation is done unless opRuns. statuses, when set, are the
// statuses the reads of the pool give in turn, the last one for good.
type poolWorld struct {
	mu       sync.Mutex
	status   string
	policy   string // scalePolicy of the pool, JSON
	gone     bool   // the pool is not there
	goneIn   int    // the pool goes after this many reads of it; 0 — it stays
	after    string // status after an accepted PATCH; "" leaves status
	patch    int    // HTTP status of the PATCH; 0 accepts
	patchErr bool   // the answer to the PATCH is lost; the PATCH is applied when applied
	applied  bool
	opRuns   bool   // the operation of the PATCH is not done
	opError  string // the error of the operation, JSON
	statuses []string
	patches  []string // bodies of the PATCHes
	reqs     []string
	// The delete of the pool (k8s_nodepool_delete): others are the other
	// pools of the cluster, by their statuses; listFails fails the list of
	// the cluster's pools; del is the HTTP status of the DELETE, 0 accepts.
	others    []string
	listFails bool
	del       int
}

func (w *poolWorld) setCount(n int) {
	w.policy = fmt.Sprintf(`{"fixedScale":{"count":%d}}`, n)
}

func (w *poolWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reqs = append(w.reqs, r.Method+" "+r.URL.Path)
	rec := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/v3/nodePools/"+poolFixed && r.Method == http.MethodGet:
		if w.goneIn > 0 {
			if w.goneIn--; w.goneIn == 0 {
				w.gone = true
			}
		}
		if w.gone {
			http.Error(rec, `{"message":"node pool not found"}`, 404)
			break
		}
		st := w.status
		if len(w.statuses) > 0 {
			st = w.statuses[0]
			if len(w.statuses) > 1 {
				w.statuses = w.statuses[1:]
			}
			w.status = st
		}
		_, _ = io.WriteString(rec, poolBody(poolFixed, st, w.policy))
	case r.URL.Path == "/v3/nodePools/"+poolFixed && r.Method == http.MethodPatch:
		b, _ := io.ReadAll(r.Body)
		w.patches = append(w.patches, string(b))
		var body struct {
			ScalePolicy struct {
				FixedScale struct {
					Count int `json:"count"`
				} `json:"fixedScale"`
			} `json:"scalePolicy"`
		}
		_ = json.Unmarshal(b, &body)
		apply := func() {
			w.setCount(body.ScalePolicy.FixedScale.Count)
			if w.after != "" {
				w.status = w.after
			}
		}
		switch {
		case w.patchErr:
			if w.applied {
				apply()
			}
			return nil, errors.New("read tcp: connection reset by peer")
		case w.patch != 0:
			http.Error(rec, `{"code":9,"message":"the platform refused"}`, w.patch)
		default:
			apply()
			_, _ = io.WriteString(rec, `{"id":"op-scale","resourceId":"`+poolFixed+`","done":false}`)
		}
	case r.URL.Path == "/v3/nodePools/"+poolFixed && r.Method == http.MethodDelete:
		if w.del != 0 {
			http.Error(rec, `{"code":9,"message":"the platform refused"}`, w.del)
			break
		}
		_, _ = io.WriteString(rec, `{"id":"op-scale","resourceId":"`+poolFixed+`","done":false}`)
	case r.URL.Path == "/v3/nodePools" && r.Method == http.MethodGet:
		if w.listFails {
			http.Error(rec, `{"code":13,"message":"internal"}`, 500)
			break
		}
		pools := []string{poolBody(poolFixed, w.status, w.policy)}
		for i, st := range w.others {
			pools = append(pools, fmt.Sprintf(`{"id":"00000000-0000-4000-8000-0000000000%02d","clusterId":%q,"name":"other-%d","status":%q}`, i+1, addonCluster, i, st))
		}
		_, _ = io.WriteString(rec, `{"nodePools":[`+strings.Join(pools, ",")+`]}`)
	case r.URL.Path == "/v3/operations/op-scale":
		op := map[string]any{"id": "op-scale", "resourceId": poolFixed, "done": !w.opRuns,
			"createdAt": "2026-11-12T09:00:00Z", "updatedAt": "2026-11-12T09:00:30Z"}
		if w.opError != "" {
			op["error"] = json.RawMessage(w.opError)
		}
		_ = json.NewEncoder(rec).Encode(op)
	case r.URL.Path == "/v3/clusters/"+addonCluster:
		_, _ = io.WriteString(rec, `{"id":"`+addonCluster+`","name":"k8s-test","status":"RUNNING","projectId":"`+addonProject+`"}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

func (w *poolWorld) writes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, q := range w.reqs {
		if !strings.HasPrefix(q, "GET ") {
			out = append(out, q)
		}
	}
	return out
}

// poolRegistry is a registry over w whose waits are a test's: polls of a
// millisecond, a wait of 30 ms at most.
func poolRegistry(t *testing.T, w *poolWorld) *Registry {
	t.Helper()
	return poolRegistryWaiting(t, w, 30*time.Millisecond)
}

// poolRegistryWaiting is poolRegistry whose wait on a status lasts limit.
func poolRegistryWaiting(t *testing.T, w *poolWorld, limit time.Duration) *Registry {
	t.Helper()
	old := httpretry.DefaultBase
	httpretry.DefaultBase = w
	t.Cleanup(func() { httpretry.DefaultBase = old })
	oldDelay := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = oldDelay })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: limit},
		}, nil
	})
}

func scale(t *testing.T, w *poolWorld, count int, confirmed bool) (map[string]any, string, bool) {
	t.Helper()
	return callNamed(t, poolRegistry(t, w), "k8s_nodepool_scale",
		map[string]any{"nodepool_id": poolFixed, "count": count, "confirmed": confirmed})
}

// The table «what the pool is → what the call answers» of
// docs/internal/briefs/k8s-nodepools.md, a world for each line (D99, П2):
// everything a read shows is answered before the question, and with the
// consent given the answer is the same — nothing is sent.
func TestK8sNodePoolScale_AnsweredBeforeTheQuestion(t *testing.T) {
	for _, c := range []struct {
		name   string
		world  func() *poolWorld
		count  int
		reason string // "" — not an error
		status string
		says   string
	}{
		{"the pool is not there", func() *poolWorld { return &poolWorld{gone: true} }, 3, clerrors.CodeNotFound, "error", ""},
		{"an autoscaled pool", func() *poolWorld {
			return &poolWorld{status: poolRunning, policy: `{"autoScale":{"minCount":1,"maxCount":5}}`}
		}, 3, clerrors.CodeUsage, "error", "autoscaled"},
		{"the pool has the count", func() *poolWorld { w := &poolWorld{status: poolRunning}; w.setCount(3); return w }, 3, "", "already_scaled", ""},
		{"on its way to the count", func() *poolWorld { w := &poolWorld{status: poolScalingUp}; w.setCount(3); return w }, 3, "", "scaling", ""},
		{"being deleted", func() *poolWorld { w := &poolWorld{status: "OBJECT_STATUS_DELETING"}; w.setCount(2); return w }, 3, clerrors.CodeUsage, "error", "nothing to scale"},
		{"upgrading and not settling", func() *poolWorld { w := &poolWorld{status: "OBJECT_STATUS_UPGRADING"}; w.setCount(2); return w }, 3, clerrors.CodeUsage, "error", "settles"},
		{"scaling to another count", func() *poolWorld { w := &poolWorld{status: poolScalingDown}; w.setCount(1); return w }, 3, clerrors.CodeUsage, "error", "settles"},
		{"more nodes for a pool in error", func() *poolWorld { w := &poolWorld{status: poolError}; w.setCount(2); return w }, 3, clerrors.CodeUsage, "error", "does not add billable nodes"},
	} {
		for _, confirmed := range []bool{false, true} {
			w := c.world()
			m, text, isErr := scale(t, w, c.count, confirmed)
			if got := len(w.writes()); got != 0 {
				t.Errorf("%s, confirmed=%v: sent %v", c.name, confirmed, w.writes())
			}
			if m["status"] == "confirmation_required" {
				t.Errorf("%s, confirmed=%v: asked the question: %s", c.name, confirmed, text)
			}
			if isErr != (c.reason != "") || m["status"] != c.status || (c.reason != "" && m["reason"] != c.reason) {
				t.Errorf("%s, confirmed=%v: want status %s, reason %q: %s", c.name, confirmed, c.status, c.reason, text)
			}
			if c.says != "" && !strings.Contains(text, c.says) {
				t.Errorf("%s, confirmed=%v: the answer does not say %q: %s", c.name, confirmed, c.says, text)
			}
			if strings.Contains(text, "delete") && c.reason != "" {
				t.Errorf("%s: the refusal names a delete as the way on: %s", c.name, text)
			}
		}
	}
}

// A pool on its way to the count answers the read of the pool as the next
// step, without an operation id it does not have and without the consent
// (Н3; R-85): the step waits and sends nothing.
func TestK8sNodePoolScale_OnItsWayNamesTheRead(t *testing.T) {
	w := &poolWorld{status: poolScalingUp}
	w.setCount(3)
	m, text, _ := scale(t, w, 3, true)
	args, _ := m["next_args"].(map[string]any)
	if m["next_tool"] != "k8s_nodepool_get" || args["nodepool_id"] != poolFixed || args["confirmed"] != nil || m["operation_id"] != nil {
		t.Errorf("want next_tool k8s_nodepool_get with the pool's id, no confirmed and no operation_id: %s", text)
	}
}

// A status another change put the pool in settles first (П4): the call
// waits, then decides by what the pool is then.
func TestK8sNodePoolScale_WaitsForAnotherChangeToSettle(t *testing.T) {
	w := &poolWorld{statuses: []string{"OBJECT_STATUS_EDITING", "OBJECT_STATUS_EDITING", poolRunning}}
	w.setCount(2)
	m, text, _ := scale(t, w, 3, false)
	if m["status"] != "confirmation_required" {
		t.Fatalf("a pool that settles: want the preview: %s", text)
	}
}

// A status of another change that has not settled by the limit of an MCP
// call (D69) is not an error and not a question: the call answers waiting
// with the status and the same call as the next step — without the
// consent, since nothing was asked yet (R-85).
func TestK8sNodePoolScale_SettlingAtTheLimitOfTheCall(t *testing.T) {
	w := &poolWorld{status: "OBJECT_STATUS_EDITING"}
	w.setCount(2)
	r := budgeted(poolRegistryWaiting(t, w, time.Minute))
	m, text, isErr := callNamed(t, r, "k8s_nodepool_scale", map[string]any{"nodepool_id": poolFixed, "count": 3, "confirmed": true})
	args, _ := m["next_args"].(map[string]any)
	if isErr || m["status"] != "waiting" || m["state"] != "OBJECT_STATUS_EDITING" || m["next_tool"] != "k8s_nodepool_scale" ||
		args["nodepool_id"] != poolFixed || args["count"] == nil || args["confirmed"] != nil {
		t.Errorf("want waiting with the status, the same call as the next step and no confirmed in it: %s", text)
	}
	if len(w.writes()) != 0 {
		t.Errorf("sent %v", w.writes())
	}
}

// A pool deleted while the call waits for another change to settle is not
// found — the wait ends at once and does not answer «still settling» for
// what is gone (D99, П5; audit of the reference).
func TestK8sNodePoolScale_GoneWhileSettling(t *testing.T) {
	w := &poolWorld{status: "OBJECT_STATUS_EDITING", goneIn: 3}
	w.setCount(2)
	r := poolRegistryWaiting(t, w, time.Minute)
	started := time.Now()
	m, text, isErr := callNamed(t, r, "k8s_nodepool_scale", map[string]any{"nodepool_id": poolFixed, "count": 3, "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeNotFound || m["status"] == "waiting" {
		t.Errorf("want NOT_FOUND, not a wait: %s", text)
	}
	// wall-clock: the wait is a minute in this world; a call that ends on the 404 takes milliseconds.
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("the call waited %s for a pool that is gone", took)
	}
	if len(w.writes()) != 0 {
		t.Errorf("sent %v", w.writes())
	}
}

// A pool whose answer names no scale policy is not taken for an autoscaled
// one, and its count is not guessed: an error before the question.
func TestK8sNodePoolScale_NoScalePolicyInTheAnswer(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		w := &poolWorld{status: poolRunning, policy: `{}`}
		m, text, isErr := scale(t, w, 3, confirmed)
		if !isErr || m["reason"] != clerrors.CodeAPIError || !strings.Contains(text, "no scale policy") || len(w.writes()) != 0 {
			t.Errorf("confirmed=%v: want API_ERROR naming the missing scale policy and no write: %s", confirmed, text)
		}
	}
}

// The preview names what the request sends and what it changes (axis 4):
// the pool, its cluster, the count it has and the count it gets, the
// flavor and the disk of a node; more nodes — how many, their vCPU and
// RAM, and that they are billed, with no number for a price evo has no
// probe of; fewer — that nodes are deleted with their data; none — that
// the pool is left empty; a pool that is not running — that the outcome
// is not known.
func TestK8sNodePoolScale_Preview(t *testing.T) {
	up := &poolWorld{status: poolRunning}
	up.setCount(2)
	m, text, _ := scale(t, up, 5, false)
	if m["status"] != "confirmation_required" {
		t.Fatalf("more nodes: want the preview: %s", text)
	}
	will, _ := m["preview"].(map[string]any)
	for k, want := range map[string]any{
		"nodepool_id": poolFixed, "name": "pool-1", "cluster_id": addonCluster, "cluster_name": "k8s-test", "pool_status": poolRunning,
		"count": float64(5), "count_now": float64(2), "nodes_added": float64(3), "cpu_added": float64(6), "ram_gb_added": float64(12),
		"flavor": "k8s-2-4", "disk_gb": float64(20), "zone": "ru.AZ-1",
	} {
		if will[k] != want {
			t.Errorf("more nodes: preview.%s is %v, want %v: %s", k, will[k], want, text)
		}
	}
	if pc, _ := will["price_classes"].([]any); len(pc) != 1 || !strings.Contains(text, "paid_unpriced") || will["tariff_url"] == nil {
		t.Errorf("more nodes: want the price class paid_unpriced and tariff_url, no number: %s", text)
	}
	if len(up.writes()) != 0 {
		t.Errorf("the preview sent %v", up.writes())
	}

	down := &poolWorld{status: poolRunning}
	down.setCount(3)
	m, text, _ = scale(t, down, 1, false)
	will, _ = m["preview"].(map[string]any)
	if msg, _ := m["message"].(string); !strings.Contains(msg, "deleted with the data on their disks") || !strings.Contains(msg, "evicted") || will["nodes_removed"] != float64(2) {
		t.Errorf("fewer nodes: the preview does not say nodes are deleted with their data and pods evicted: %s", text)
	}
	if will["price_classes"] != nil {
		t.Errorf("fewer nodes start no billing: %s", text)
	}

	m, text, _ = scale(t, down, 0, false)
	if warn, _ := m["warning"].(string); !strings.Contains(warn, "no nodes") {
		t.Errorf("no nodes: the preview does not warn the pool is left empty: %s", text)
	}

	broken := &poolWorld{status: poolError}
	broken.setCount(3)
	m, text, _ = scale(t, broken, 1, false)
	if warn, _ := m["warning"].(string); m["status"] != "confirmation_required" || !strings.Contains(warn, "unknown") {
		t.Errorf("fewer nodes for a pool in error: want the preview with the outcome unknown (П4): %s", text)
	}
}

// The body of the request is the count and nothing else: the
// specification of mk8s removes every label and taint of a pool for an
// empty labels or taints object in an update.
func TestK8sNodePoolScale_SendsTheCountOnly(t *testing.T) {
	for _, count := range []int{5, 0} {
		w := &poolWorld{status: poolRunning}
		w.setCount(2)
		if _, text, isErr := scale(t, w, count, true); isErr {
			t.Fatalf("count %d: %s", count, text)
		}
		want := fmt.Sprintf(`{"scalePolicy":{"fixedScale":{"count":%d}}}`, count)
		if len(w.patches) != 1 || w.patches[0] != want {
			t.Errorf("count %d: sent %v, want one PATCH %s", count, w.patches, want)
		}
	}
}

// After the consent: the operation ended — scaled; it runs on at the
// limit of the wait — scaling with its id and k8s_operation as the next
// step, with no consent in it (Н3; R-85); it failed — an error that names
// the platform's words and no success.
func TestK8sNodePoolScale_AnswersTheOperation(t *testing.T) {
	done := &poolWorld{status: poolRunning, after: poolRunning}
	done.setCount(2)
	m, text, isErr := scale(t, done, 3, true)
	if isErr || m["status"] != "scaled" || m["operation_id"] != "op-scale" || m["count"] != float64(3) || m["next_tool"] != nil {
		t.Errorf("an operation that ended: want scaled with its id and no next step: %s", text)
	}

	running := &poolWorld{status: poolRunning, after: poolScalingUp, opRuns: true}
	running.setCount(2)
	r := poolRegistry(t, running)
	oldMax := scaleWait
	scaleWait = 0
	t.Cleanup(func() { scaleWait = oldMax })
	m, text, isErr = callNamed(t, r, "k8s_nodepool_scale", map[string]any{"nodepool_id": poolFixed, "count": 3, "confirmed": true})
	args, _ := m["next_args"].(map[string]any)
	if isErr || m["status"] != "scaling" || m["operation_id"] != "op-scale" || m["next_tool"] != "k8s_operation" ||
		args["operation_id"] != "op-scale" || args["confirmed"] != nil {
		t.Errorf("an operation that runs on: want scaling, its id and k8s_operation with that id, no confirmed: %s", text)
	}
	scaleWait = oldMax

	failed := &poolWorld{status: poolRunning, opError: `{"code":8,"message":"quota exceeded"}`}
	failed.setCount(2)
	m, text, isErr = scale(t, failed, 3, true)
	if !isErr || m["reason"] != clerrors.CodeAPIError || !strings.Contains(text, "quota exceeded") || m["operation_id"] != "op-scale" {
		t.Errorf("an operation that failed: want API_ERROR with the platform's words and the operation's id: %s", text)
	}
}

// A request that failed (П6): the pool is read again. A lost answer whose
// request was taken answers as if it were — no second request; a refusal
// is the error with the status the pool is in; a pool that is gone is not
// found.
func TestK8sNodePoolScale_RequestFailed(t *testing.T) {
	lost := &poolWorld{status: poolRunning, after: poolScalingUp, patchErr: true, applied: true}
	lost.setCount(2)
	m, text, isErr := scale(t, lost, 3, true)
	if isErr || m["status"] != "scaling" || m["next_tool"] != "k8s_nodepool_get" {
		t.Errorf("a lost answer, the request taken: want scaling with the read of the pool as the next step: %s", text)
	}
	// The repeat sends nothing: the pool is on its way to the count.
	before := len(lost.patches)
	lost.patchErr = false
	if m, text, _ := scale(t, lost, 3, true); m["status"] != "scaling" || len(lost.patches) != before {
		t.Errorf("the repeat after a lost answer sent a second request or lost its way: %s (%v)", text, lost.patches)
	}

	dropped := &poolWorld{status: poolRunning, patchErr: true}
	dropped.setCount(2)
	if m, text, isErr := scale(t, dropped, 3, true); !isErr || m["status"] == "scaled" || m["status"] == "scaling" {
		t.Errorf("a lost answer, the request not taken: want the error, not a success: %s", text)
	}

	refused := &poolWorld{status: poolRunning, patch: 400}
	refused.setCount(2)
	m, text, isErr = scale(t, refused, 3, true)
	if !isErr || m["state"] != poolRunning || m["nodepool_id"] != poolFixed || !strings.Contains(text, "the platform refused") {
		t.Errorf("a refusal: want the error with the platform's words and the status of the pool: %s", text)
	}
}
