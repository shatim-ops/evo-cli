// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// natHostileAPI is natWaitAPI whose platform took the gateway under an id
// that is not an id: the cloud's text.
type natHostileAPI struct{ posts int }

func (a *natHostileAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/api/v1/nat-gateways" && r.Method == http.MethodPost:
		a.posts++
		b, _ := json.Marshal(map[string]any{"id": hostileName, "name": "evo-n", "status": "CREATING"})
		_, _ = rec.Write(b)
	case r.URL.Path == "/api/v1/nat-gateways":
		_, _ = io.WriteString(rec, `{"items":[],"total":0}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// The repeat of a create the platform took (repeat_of) names the id the
// cloud gave the first call: "_untrusted" names repeat_of, and the message
// — evo's text — carries the id quoted on one line (#2378, part 2; «Риск»,
// 357).
func TestRepeatOf_CloudIDMarkedAndQuoted(t *testing.T) {
	api := &natHostileAPI{}
	r := waitingRegistry(t, api)
	args := map[string]any{"name": "evo-n", "availability_zone_id": "00000000-0000-4000-8000-0000000000a1", "confirmed": true}
	callNamed(t, r, "snat_create", args)
	m, text, _ := callNamed(t, r, "snat_create", args)
	rep, _ := m["repeat_of"].(map[string]any)
	if api.posts != 1 || rep["id"] != hostileName {
		t.Fatalf("the world no longer reaches the repeat of a create taken: %d POSTs, %s", api.posts, text)
	}
	if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any("repeat_of")) {
		t.Errorf("repeat_of holds the cloud's id, \"_untrusted\" %v does not name it", list)
	}
	if msg, _ := m["message"].(string); strings.ContainsAny(msg, "\n\r") || !strings.Contains(msg, quoteCloud(hostileName)) {
		t.Errorf("the message carries the cloud's id unquoted: %q", msg)
	}
}

// The reads of one resource (vm_get, backup_get, k8s_nodepool_get) answer
// its id as the service has it — no check of its form on the way: the
// marker names id (#2378, part 2; «Риск», 357).
func TestReads_CloudIDsMarked(t *testing.T) {
	faultToolsIsolate(t)
	want := map[string]string{"vm_get": "id", "backup_get": "id", "k8s_nodepool_get": "id"}
	body := hostileBody(map[string]any{"id": hostileName, "operation_id": hostileName, "operationId": hostileName})
	api := &oddAPI{status: http.StatusOK, body: body}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	cloud.Backoff = 1
	r := raRegistry(t, api, cloud, nil)
	r.SetCallBudget(containerBudget)
	for tool, field := range want {
		tl, _ := r.Lookup(tool)
		reached := false
		for _, args := range requiredCalls(tl) {
			args = maps.Clone(args)
			args["confirmed"] = true
			_, text, _ := callNamed(t, r, tool, args)
			var m map[string]any
			if json.Unmarshal([]byte(text), &m) != nil {
				continue
			}
			b, _ := json.Marshal(m[field])
			if !strings.Contains(string(b), hostileMarker) {
				continue
			}
			reached = true
			if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any(field)) {
				t.Errorf("%s: %s holds the cloud's text, \"_untrusted\" %v does not name it", tool, field, list)
			}
		}
		if !reached {
			t.Errorf("%s: the world no longer reaches %s of the cloud", tool, field)
		}
	}
}
