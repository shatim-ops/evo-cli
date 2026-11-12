// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// mk8sOps answers GET /v3/operations/{id} with the operation op has for
// the id and leaves the rest to fakeMK8s.
type mk8sOps struct {
	*fakeMK8s
	ops map[string]string
}

func (f mk8sOps) RoundTrip(r *http.Request) (*http.Response, error) {
	if id, ok := strings.CutPrefix(r.URL.Path, "/v3/operations/"); ok && r.Method == http.MethodGet {
		rec := httptest.NewRecorder()
		if body := f.ops[id]; body != "" {
			_, _ = io.WriteString(rec, body)
		} else {
			http.Error(rec, `{"message":"not found"}`, 404)
		}
		return rec.Result(), nil
	}
	return f.fakeMK8s.RoundTrip(r)
}

// Managed Kubernetes had no read of its operations: k8s_create,
// k8s_add_nodepool and k8s_delete return operation_id, and the agent could
// only poll the lists (CLAUDE.md §6, axis 2). k8s_operation answers as the
// other *_operation do: the operation, how long it has run by its own
// times, and the status of its resource — the cluster's from k8s_get; of a
// resource k8s_get does not read (a node pool), not_found with the note.
func TestK8sOperation_AnswersTheOperationAndItsResource(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = old })

	const pool = "00000000-0000-0000-0000-0000000000d1"
	f := mk8sOps{fakeMK8s: &fakeMK8s{}, ops: map[string]string{
		"op-cluster": `{"id":"op-cluster","resourceId":"` + addonCluster + `","done":false,"createdAt":"2026-11-12T08:58:00Z"}`,
		"op-pool":    `{"id":"op-pool","resourceId":"` + pool + `","done":true,"createdAt":"2026-11-12T08:50:00Z","updatedAt":"2026-11-12T08:51:30Z"}`,
		"op-failed":  `{"id":"op-failed","resourceId":"` + addonCluster + `","done":true,"error":{"code":9,"message":"quota exceeded"},"createdAt":"2026-11-12T08:50:00Z","updatedAt":"2026-11-12T08:50:05Z"}`,
	}}
	r := addonRegistry(t, f.fakeMK8s)
	// addonRegistry put the bare fake in; the operations go in front of it.
	bare := httpretry.DefaultBase
	httpretry.DefaultBase = f
	t.Cleanup(func() { httpretry.DefaultBase = bare })

	m, text, isErr := callNamed(t, r, "k8s_operation", map[string]any{"operation_id": "op-cluster"})
	if isErr || m["done"] != false || m["elapsed_sec"] != float64(120) || m["resource_status"] != "RUNNING" {
		t.Errorf("a running operation of a cluster: want done false, elapsed_sec 120, resource_status RUNNING: %s", text)
	}
	m, text, isErr = callNamed(t, r, "k8s_operation", map[string]any{"operation_id": "op-pool"})
	if isErr || m["done"] != true || m["elapsed_sec"] != float64(90) || m["resource_status"] != "not_found" || m["resource_status_note"] == nil {
		t.Errorf("a done operation of a node pool: want done true, elapsed_sec 90, resource_status not_found with its note: %s", text)
	}
	m, text, isErr = callNamed(t, r, "k8s_operation", map[string]any{"operation_id": "op-failed"})
	if e, _ := m["error"].(map[string]any); isErr || m["done"] != true || e["message"] != "quota exceeded" {
		t.Errorf("a failed operation: want done true and the service's error: %s", text)
	}
	if _, text, isErr = callNamed(t, r, "k8s_operation", map[string]any{"operation_id": "op-none"}); !isErr || !strings.Contains(text, "NOT_FOUND") {
		t.Errorf("an operation that is not there: want NOT_FOUND: %s", text)
	}
	if _, text, isErr = callNamed(t, r, "k8s_operation", nil); !isErr || !strings.Contains(text, "USAGE") {
		t.Errorf("no operation_id: want USAGE: %s", text)
	}
}
