// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// natAPI is compute's SNAT gateways: one named "evo-n" exists; a POST
// makes another and is counted.
type natAPI struct {
	mu    sync.Mutex
	posts int
}

func (a *natAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/api/v1/nat-gateways" && r.Method == http.MethodPost:
		a.mu.Lock()
		a.posts++
		a.mu.Unlock()
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e2","name":"evo-n","status":"CREATING"}`)
	case r.URL.Path == "/api/v1/nat-gateways":
		_, _ = io.WriteString(rec, `{"items":[{"id":"00000000-0000-4000-8000-0000000000e1","name":"evo-n","status":"ACTIVE"}],"total":1}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// The fault matrix (process audit 1.2.1, 6.1) found it: snat_create had no
// name check, and every repeat made one more billed gateway. A gateway of
// the name refuses the create, with or without confirmed, and names it —
// no POST (CLAUDE.md §1: a repeated create makes no duplicates).
func TestSnatCreate_TakenNameRefusedNoSecondGateway(t *testing.T) {
	api := &natAPI{}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	for _, confirmed := range []bool{false, true} {
		m, text, isErr := callNamed(t, r, "snat_create", map[string]any{
			"name": "evo-n", "availability_zone_id": "00000000-0000-4000-8000-0000000000a1", "confirmed": confirmed,
		})
		if !isErr || m["reason"] != "USAGE" || m["existing_id"] != "00000000-0000-4000-8000-0000000000e1" {
			t.Errorf("confirmed=%v: %s; want the taken name refused with existing_id", confirmed, text)
		}
	}
	if api.posts != 0 {
		t.Errorf("snat_create made %d gateways next to the one of that name", api.posts)
	}
}
