// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Ids from MCP arguments go into request paths. "disk/../other" or "id?x"
// must stay one escaped segment: a confirmation preview names the id the
// agent passed, and the DELETE must not land on another resource.
func TestResourceIDsAreOnePathSegment(t *testing.T) {
	var mu sync.Mutex
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uris = append(uris, r.RequestURI)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a := NewCloudRuAdapter(srv.Client(), srv.URL)
	a.SetToken("t")
	ctx := context.Background()

	_ = a.DeleteDisk(ctx, "d1/../d2?force=true", "p&x=1")
	_ = a.DeleteVMInProject(ctx, "..", "p")
	_ = a.DeleteFloatingIP(ctx, "f#frag", "p")

	want := []string{
		"/api/v1/disks/d1%2F..%2Fd2%3Fforce=true?project_id=p%26x%3D1",
		"/api/v1/vms/%2E%2E?project_id=p",
		"/api/v1/floating-ips/f%23frag?project_id=p",
	}
	if len(uris) != len(want) {
		t.Fatalf("requests %v, want %v", uris, want)
	}
	for i := range want {
		if uris[i] != want[i] {
			t.Errorf("request %d: %s, want %s", i, uris[i], want[i])
		}
	}
}
