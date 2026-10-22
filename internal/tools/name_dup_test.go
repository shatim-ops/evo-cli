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

// dupAPI is mk8s and NLB with one cluster and one balancer named "evo-n"; a
// POST of either makes another and is counted.
type dupAPI struct {
	mu    sync.Mutex
	posts []string
}

const (
	dupClusterID = "00000000-0000-4000-8000-0000000000c1"
	dupLBID      = "00000000-0000-4000-8000-0000000000b1"
)

func (a *dupAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	if r.Method == http.MethodPost {
		a.mu.Lock()
		a.posts = append(a.posts, r.URL.Path)
		a.mu.Unlock()
		_, _ = io.WriteString(rec, `{"id":"op-1","resourceId":"new"}`)
		return rec.Result(), nil
	}
	switch r.URL.Path {
	case "/v3/clusters":
		_, _ = io.WriteString(rec, `{"clusters":[{"id":"`+dupClusterID+`","name":"evo-n","status":"RUNNING"}]}`)
	case "/v2/networkLoadBalancers":
		_, _ = io.WriteString(rec, `{"balancers":[{"id":"`+dupLBID+`","name":"evo-n","status":"ACTIVE"}]}`)
	default:
		http.Error(rec, `{"message":"not found"}`, 404)
	}
	return rec.Result(), nil
}

// The fault matrix (process audit 1.2.1, 6.1) found k8s_create and
// lb_create with no name check, as snat_create (D20): a repeated call made a
// second billed cluster or balancer, and the specifications of mk8s and NLB
// do not refuse one. A resource of the name refuses the create, with or
// without confirmed, and names it — no POST (CLAUDE.md §1; DECISIONS D21).
func TestCreate_TakenNameRefusedNoSecondClusterOrBalancer(t *testing.T) {
	api := &dupAPI{}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	for _, c := range []struct {
		tool, existing string
		args           map[string]any
	}{
		{"k8s_create", dupClusterID, map[string]any{"name": "evo-n"}},
		{"lb_create", dupLBID, map[string]any{"name": "evo-n", "target_group_id": "00000000-0000-4000-8000-0000000000d1", "port": float64(80)}},
	} {
		for _, confirmed := range []bool{false, true} {
			args := map[string]any{"confirmed": confirmed}
			for k, v := range c.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, r, c.tool, args)
			if !isErr || m["reason"] != "USAGE" || m["existing_id"] != c.existing {
				t.Errorf("%s confirmed=%v: %s; want the taken name refused with existing_id %s", c.tool, confirmed, text, c.existing)
			}
		}
	}
	if len(api.posts) != 0 {
		t.Errorf("created next to the resource of that name: POST %v", api.posts)
	}
}
