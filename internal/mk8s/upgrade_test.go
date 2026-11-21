// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mk8s

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestUpgradeSendsOneExactRequest(t *testing.T) {
	for _, pool := range []bool{false, true} {
		group := "clusters"
		if pool {
			group = "nodePools"
		}
		for _, status := range []int{200, 307, 401, 403, 429, 503} {
			t.Run(group+"/"+http.StatusText(status), func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != "POST" || r.URL.Path != "/v3/"+group+"/resource:upgrade" || r.URL.RawQuery != "project_id=project" || r.Header.Get("Authorization") != "Bearer token" {
						t.Errorf("unexpected %s %s", r.Method, r.URL)
						w.WriteHeader(400)
						return
					}
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]any{"version": "v1.36.5"}) {
						t.Errorf("body: %v", body)
					}
					if status == 307 {
						w.Header().Set("Location", "/other")
					}
					w.WriteHeader(status)
					io.WriteString(w, `{"id":"operation","resourceId":"resource","done":false}`)
				}))
				defer srv.Close()
				c := New("token", "project", srv.URL)
				submit := c.UpgradeCluster
				if pool {
					submit = c.UpgradeNodePool
				}
				op, err := submit(t.Context(), "resource", "v1.36.5")
				if calls.Load() != 1 || (err == nil) != (status == 200) {
					t.Fatalf("calls=%d err=%v", calls.Load(), err)
				}
				if err == nil && (op.ID != "operation" || op.ResourceID != "resource" || op.Done) {
					t.Fatalf("receipt: %+v", op)
				}
			})
		}
	}
}
