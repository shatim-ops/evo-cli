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

// mk8sPaths answers the GETs of Managed Kubernetes it has a body for, by
// path, 404 for the others under its prefixes, and leaves the rest to
// fakeMK8s.
type mk8sPaths struct {
	*fakeMK8s
	bodies map[string]string
}

func (f mk8sPaths) RoundTrip(r *http.Request) (*http.Response, error) {
	own := strings.HasPrefix(r.URL.Path, "/v3/operations/") || strings.HasPrefix(r.URL.Path, "/v3/nodePools/")
	if !own || r.Method != http.MethodGet {
		return f.fakeMK8s.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	if body := f.bodies[r.URL.Path]; body != "" {
		_, _ = io.WriteString(rec, body)
	} else {
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// mk8sRegistry is addonRegistry with the paths of f in front of its fake.
func mk8sRegistry(t *testing.T, f mk8sPaths) *Registry {
	t.Helper()
	r := addonRegistry(t, f.fakeMK8s)
	bare := httpretry.DefaultBase
	httpretry.DefaultBase = f
	t.Cleanup(func() { httpretry.DefaultBase = bare })
	return r
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
	r := mk8sRegistry(t, mk8sPaths{fakeMK8s: &fakeMK8s{}, bodies: map[string]string{
		"/v3/operations/op-cluster": `{"id":"op-cluster","resourceId":"` + addonCluster + `","done":false,"createdAt":"2026-11-12T08:58:00Z"}`,
		"/v3/operations/op-pool":    `{"id":"op-pool","resourceId":"` + pool + `","done":true,"createdAt":"2026-11-12T08:50:00Z","updatedAt":"2026-11-12T08:51:30Z"}`,
		"/v3/operations/op-failed":  `{"id":"op-failed","resourceId":"` + addonCluster + `","done":true,"error":{"code":9,"message":"quota exceeded"},"createdAt":"2026-11-12T08:50:00Z","updatedAt":"2026-11-12T08:50:05Z"}`,
	}})

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
	// The error is the service's text, not evo's: the answer marks it.
	if u, _ := m["_untrusted"].([]any); len(u) != 1 || u[0] != "error" {
		t.Errorf("a failed operation: _untrusted %v, want [error]: %s", m["_untrusted"], text)
	}
	if _, text, isErr = callNamed(t, r, "k8s_operation", map[string]any{"operation_id": "op-none"}); !isErr || !strings.Contains(text, "NOT_FOUND") {
		t.Errorf("an operation that is not there: want NOT_FOUND: %s", text)
	}
	if _, text, isErr = callNamed(t, r, "k8s_operation", nil); !isErr || !strings.Contains(text, "USAGE") {
		t.Errorf("no operation_id: want USAGE: %s", text)
	}
}
