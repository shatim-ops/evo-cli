// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// ListImages sends the filter as the server-side query and reads the
// fields of the catalogue entry (type, display_name, min_*, created_time,
// public); a bare call sends no filter.
func TestListImages_FilterAndFields(t *testing.T) {
	var queries []url.Values
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		_ = json.NewEncoder(w).Encode(map[string]any{"total": 1, "items": []map[string]any{{
			"id": "00000000-0000-0000-0000-000000000001", "name": "astra_1.8.1",
			"display_name": "Astra Linux 1.8.1", "description": "ОС", "type": "marketplace",
			"min_cpu": 2, "min_ram": 4, "min_disk": 20, "created_time": "2026-01-01T00:00:00Z",
			"public": true, "icon": "<svg/>",
			"availability_zones": []map[string]any{
				{"availability_zone_id": "z1", "availability_zone_name": "ru.AZ-1", "state": "created", "enabled": true},
				{"availability_zone_id": "z2", "availability_zone_name": "ru.AZ-2", "state": "created", "enabled": false},
				{"availability_zone_id": "z3", "availability_zone_name": "ru.AZ-3", "state": "creating"},
			},
		}}})
	})
	defer srv.Close()

	on, off := true, false
	imgs, err := a.ListImages(context.Background(), ImageFilter{
		Name: "astra", Type: ImageTypeMarketplace, Enabled: &on, NeedGPU: &off, AvailabilityZoneID: "z1",
		ProjectID: "00000000-0000-0000-0000-0000000000f1",
	})
	if err != nil {
		t.Fatal(err)
	}
	q := queries[0]
	for k, want := range map[string]string{"name": "astra", "type": "marketplace", "enabled": "true", "need_gpu": "false", "availability_zone_id": "z1",
		"project_id": "00000000-0000-0000-0000-0000000000f1"} {
		if q.Get(k) != want {
			t.Errorf("query %s = %q, want %q (%v)", k, q.Get(k), want, q)
		}
	}
	if q.Has("display_name") {
		t.Errorf("empty display_name sent: %v", q)
	}
	img := imgs[0]
	if img.Type != "marketplace" || img.DisplayName != "Astra Linux 1.8.1" || img.MinDisk != 20 || img.MinCPU != 2 ||
		img.MinRAM != 4 || !img.Public || img.CreatedTime == "" || img.Description != "ОС" {
		t.Errorf("fields not parsed: %+v", img)
	}
	if rz := img.ReadyZones(); len(rz) != 1 || rz[0].AvailabilityZoneName != "ru.AZ-1" {
		t.Errorf("ready zones: %+v", rz)
	}

	if _, err := a.ListImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "display_name", "type", "enabled", "need_gpu", "availability_zone_id", "project_id"} {
		if queries[1].Has(k) {
			t.Errorf("bare call sent %s: %v", k, queries[1])
		}
	}
}
