// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// A list read under Narrowed carries the parameters on every page, and
// only the list of that path: the same list read without it — as a write
// or a cleanup reads it — is asked whole (briefs/list-filters.md,
// «Механизм»; the risk conclusion 104, condition 1).
func TestNarrowed_OnlyTheListOfItsPath(t *testing.T) {
	var queries []string
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.Query().Encode())
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}, "total": 0})
	})
	defer srv.Close()

	ctx := Narrowed(context.Background(), "/api/v1/vms", "availability_zone_ids", "z1")
	ctx = Narrowed(ctx, "/api/v1/vms", "statuses", "running")
	if _, err := a.ListVMsInProject(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListSubnets(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListVMsInProject(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/v1/vms?availability_zone_ids=z1&limit=100&project_id=p&statuses=running",
		"/api/v1/subnets?limit=100&project_id=p",
		"/api/v1/vms?limit=100&project_id=p",
	}
	if len(queries) != len(want) {
		t.Fatalf("requests %v, want %v", queries, want)
	}
	for i := range want {
		if queries[i] != want[i] {
			t.Errorf("request %d: %s, want %s", i, queries[i], want[i])
		}
	}
}
