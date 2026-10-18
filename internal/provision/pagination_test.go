// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// pagedServer serves total items under path in documented pages (limit ≤
// 100) and records every limit it was sent.
func pagedServer(t *testing.T, path string, total int, limits *[]string) (*CloudRuAdapter, func()) {
	t.Helper()
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		*limits = append(*limits, r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 50
		}
		if limit > 100 {
			http.Error(w, `{"detail":"limit: less than or equal to 100"}`, http.StatusUnprocessableEntity)
			return
		}
		var items []map[string]any
		for i := offset; i < total && i < offset+limit; i++ {
			items = append(items, map[string]any{"id": fmt.Sprintf("x%d", i), "name": fmt.Sprintf("n%d", i)})
		}
		if items == nil {
			items = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": total})
	})
	return a, srv.Close
}

// Compute lists are read page by page with the documented maximum limit:
// 130 subnets, 230 flavors, 120 images all arrive (api-contract §4.5, §4.7).
func TestCompute_ListsReadEveryPage(t *testing.T) {
	cases := []struct {
		path  string
		total int
		list  func(*CloudRuAdapter) (int, error)
	}{
		{"/api/v1/subnets", 130, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListSubnets(context.Background(), "p")
			return len(l), err
		}},
		{"/api/v1/flavors", 230, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListFlavors(context.Background())
			return len(l), err
		}},
		{"/api/v1/images", 120, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListImages(context.Background())
			return len(l), err
		}},
		{"/api/v1/floating-ips", 101, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListFloatingIPs(context.Background(), "p")
			return len(l), err
		}},
		// Релизный QA (C-api 2): диски и NAT-шлюзы — тоже все страницы.
		{"/api/v1/disks", 130, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListDisks(context.Background(), "p")
			return len(l), err
		}},
		{"/api/v1/nat-gateways", 55, func(a *CloudRuAdapter) (int, error) {
			l, err := a.ListNATGateways(context.Background(), "p")
			return len(l), err
		}},
	}
	for _, c := range cases {
		var limits []string
		a, done := pagedServer(t, c.path, c.total, &limits)
		n, err := c.list(a)
		done()
		if err != nil || n != c.total {
			t.Errorf("%s: %d items, %v; want %d", c.path, n, err, c.total)
		}
		for _, l := range limits {
			if l != "100" {
				t.Errorf("%s: limit=%s, want 100", c.path, l)
			}
		}
	}
}

// disk-types has no limit/offset parameters: none are sent.
func TestCompute_DiskTypesNoLimit(t *testing.T) {
	var limits []string
	a, done := pagedServer(t, "/api/v1/disk-types", 3, &limits)
	defer done()
	l, err := a.ListDiskTypes(context.Background(), "p")
	if err != nil || len(l) != 3 {
		t.Fatalf("disk types = %d, %v", len(l), err)
	}
	if len(limits) != 1 || limits[0] != "" {
		t.Errorf("limits sent = %q, want none", limits)
	}
}

// The documented nested fields fill the flat ones evo reads (api-contract
// §4.3, §4.4, §4.6); old answers with flat fields still work.
func TestDecode_NestedSpecFields(t *testing.T) {
	var sn Subnet
	_ = json.Unmarshal([]byte(`{"id":"s","name":"backend-net","availability_zone":{"id":"z1","name":"ru.AZ-1"}}`), &sn)
	if sn.AvailabilityZoneID != "z1" {
		t.Errorf("subnet zone = %q, want z1 from availability_zone.id", sn.AvailabilityZoneID)
	}
	var sg SecurityGroup
	_ = json.Unmarshal([]byte(`{"id":"g","availability_zone":{"id":"z2"}}`), &sg)
	if sg.AvailabilityZoneID != "z2" {
		t.Errorf("security group zone = %q, want z2", sg.AvailabilityZoneID)
	}
	var flat Subnet
	_ = json.Unmarshal([]byte(`{"id":"s","availability_zone_id":"z9"}`), &flat)
	if flat.AvailabilityZoneID != "z9" {
		t.Errorf("flat zone lost: %q", flat.AvailabilityZoneID)
	}

	var vm VM
	_ = json.Unmarshal([]byte(`{"id":"v","state":"running","flavor":{"id":"f1"},"image":{"id":"i1"},
		"availability_zone":{"id":"z1"},
		"interfaces":[{"type":"regular","ip_address":"10.0.0.5","floating_ip":{"id":"fip","ip_address":"203.0.113.7"}}]}`), &vm)
	if vm.FlavorID != "f1" || vm.ImageID != "i1" || vm.FloatingIP != "203.0.113.7" || vm.PublicIP != "" || vm.ZoneID() != "z1" {
		t.Errorf("vm = %+v", vm)
	}
	var direct VM
	_ = json.Unmarshal([]byte(`{"id":"v","interfaces":[{"type":"direct_ip","ip_address":"198.51.100.4"}]}`), &direct)
	if direct.PublicIP != "198.51.100.4" {
		t.Errorf("direct_ip public address = %q", direct.PublicIP)
	}

	usable := func(js string) bool {
		var im Image
		_ = json.Unmarshal([]byte(js), &im)
		return im.Usable()
	}
	for js, want := range map[string]bool{
		`{"id":"i","availability_zones":[{"state":"created","enabled":true}]}`:  true,
		`{"id":"i","availability_zones":[{"state":"created","enabled":false}]}`: false,
		`{"id":"i","availability_zones":[{"state":"creating"}]}`:                false,
		`{"id":"i","availability_zones":[{"state":"created"}]}`:                 true,
		`{"id":"i","status":"deleted"}`:                                         false,
		`{"id":"i"}`:                                                            true,
	} {
		if got := usable(js); got != want {
			t.Errorf("Usable(%s) = %v, want %v", js, got, want)
		}
	}
}

// GetVM reads the state from a full answer with every key the live API
// sends (api-probe, 15.10.2026), whatever the timestamp format.
func TestGetVM_FullLiveShape(t *testing.T) {
	for _, created := range []string{"2026-10-15T20:48:44.123456Z", "2026-10-15T20:48:44.123456+03:00", "2026-10-15T20:48:44.123456", "2026-10-15T20:48:44"} {
		body := `{"additional_properties":{},"availability_zone":{"id":"z1","name":"ru.AZ-1"},"created_time":"` + created + `",
			"description":"","flavor":{"id":"f1","name":"lowcost10-2-4","cpu":2,"ram":4},"guest_agent_state":"ready","icon":"<svg/>",
			"id":"vm-1","image_id":"img-1","interfaces":[{"floating_ip":{"id":"fip-1","ip_address":"203.0.113.9","name":"fip","state":"in_use"},
			"id":"if-1","interface_security_enabled":true,"ip_address":"10.0.0.4","name":"eth0","primary":true,"security_groups":[{"id":"sg"}],
			"state":"created","subnet":{"id":"sn"},"type":"regular"}],"is_serial_ready":true,"locked":false,"metadata_fields":[],
			"modified_time":"` + created + `","name":"evo-e2e-app","project_id":"p","remote_console_protocol":"vnc","state":"running","tags":[]}`
		srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		vm, err := a.GetVM(context.Background(), "vm-1")
		srv.Close()
		if err != nil || vm.Status != "running" || vm.FloatingIP != "203.0.113.9" || vm.FlavorID != "f1" || vm.ZoneID() != "z1" {
			t.Errorf("created_time %q: vm %+v, %v", created, vm, err)
		}
		if vm != nil && vm.CreatedAt.IsZero() {
			t.Errorf("created_time %q not parsed", created)
		}
	}
}
