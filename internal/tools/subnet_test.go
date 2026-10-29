// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	subnetZone    = "00000000-0000-4000-8000-0000000000a1"
	subnetVPC     = "00000000-0000-4000-8000-0000000000b1"
	subnetOtherVP = "00000000-0000-4000-8000-0000000000b2"
	subnetOld     = "00000000-0000-4000-8000-0000000000c1"
	subnetNew     = "00000000-0000-4000-8000-0000000000c2"
)

// subnetAPI is compute's subnets and zones and the VPCs of vpc.api: the
// project has a default VPC with subnet "old" 10.0.0.0/24 and a second
// VPC; writes are recorded.
type subnetAPI struct {
	mu        sync.Mutex
	writes    []string
	body      map[string]any
	vpcs      string // the vpcs of /v1/vpcs
	canDelete bool
	gone      bool // GET and DELETE of the subnet answer 404
}

func newSubnetAPI() *subnetAPI {
	return &subnetAPI{canDelete: true,
		vpcs: fmt.Sprintf(`[{"id":%q,"name":"default","default":true},{"id":%q,"name":"other","default":false}]`, subnetVPC, subnetOtherVP)}
}

func (a *subnetAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	old := fmt.Sprintf(`{"id":%q,"name":"old","subnet_address":"10.0.0.0/24","vpc_id":%q,"state":"created","can_delete":%t,"availability_zone":{"id":%q,"name":"ru.AZ-1"}}`,
		subnetOld, subnetVPC, a.canDelete, subnetZone)
	switch {
	case r.URL.Path == "/v1/vpcs":
		_, _ = fmt.Fprintf(rec, `{"vpcs":%s,"total":2}`, a.vpcs)
	case r.URL.Path == "/api/v1/availability-zones":
		_, _ = fmt.Fprintf(rec, `[{"id":%q,"name":"ru.AZ-1"}]`, subnetZone)
	case r.URL.Path == "/api/v1/subnets" && r.Method == http.MethodGet:
		_, _ = fmt.Fprintf(rec, `{"items":[%s],"total":1}`, old)
	case r.URL.Path == "/api/v1/subnets" && r.Method == http.MethodPost:
		raw, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.writes = append(a.writes, "POST")
		_ = json.Unmarshal(raw, &a.body)
		a.mu.Unlock()
		rec.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(rec, `{"id":%q,"name":"web","state":"creating","subnet_address":"10.10.0.0/24","vpc_id":%q}`, subnetNew, subnetVPC)
	case r.URL.Path == "/api/v1/subnets/"+subnetOld && a.gone:
		rec.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rec, `{"message":"Subnet not found"}`)
	case r.URL.Path == "/api/v1/subnets/"+subnetOld && r.Method == http.MethodGet:
		_, _ = io.WriteString(rec, old)
	case r.URL.Path == "/api/v1/subnets/"+subnetOld && r.Method == http.MethodDelete:
		a.mu.Lock()
		a.writes = append(a.writes, "DELETE")
		a.mu.Unlock()
		rec.WriteHeader(http.StatusNoContent)
	default:
		rec.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rec, `{"message":"not found"}`)
	}
	return rec.Result(), nil
}

func subnetRegistry(t *testing.T, api *subnetAPI) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	return raRegistry(t, api, cloud, nil)
}

// G130-23: subnet_create makes a subnet the call describes. The preview
// names, before the question, everything the POST then sends (CLAUDE.md
// §6, axis 4): the CIDR, the VPC — the project's default when the call
// names none, resolved by evo and sent, not left to the platform — the
// zone by its name, and whether the subnet is routed. No write before
// consent; with it, one POST and the subnet's id in the answer.
func TestSubnetCreate_PreviewNamesWhatThePostSends(t *testing.T) {
	api := newSubnetAPI()
	r := subnetRegistry(t, api)
	args := map[string]any{"name": "web", "cidr": "10.10.0.0/24", "zone": "ru.AZ-1"}
	m, text, isErr := callNamed(t, r, "subnet_create", args)
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("no consent asked: %s", text)
	}
	if len(api.writes) != 0 {
		t.Fatalf("writes before consent: %v", api.writes)
	}
	for _, want := range []string{"10.10.0.0/24", subnetVPC, "ru.AZ-1", "routed_network"} {
		if !strings.Contains(text, want) {
			t.Errorf("preview does not name %s: %s", want, text)
		}
	}
	args["confirmed"] = true
	m, text, isErr = callNamed(t, r, "subnet_create", args)
	if isErr || m["status"] != "created" || m["subnet_id"] != subnetNew {
		t.Fatalf("create: %s", text)
	}
	want := map[string]any{"name": "web", "subnet_address": "10.10.0.0/24", "prefix_length": float64(24),
		"vpc_id": subnetVPC, "availability_zone_id": subnetZone, "project_id": raProject, "routed_network": true}
	for k, v := range want {
		if api.body[k] != v {
			t.Errorf("POST %s = %v, want %v (body %v)", k, api.body[k], v, api.body)
		}
	}
}

// Axis 1: what the platform would refuse, evo refuses before the question
// and without a write — the CIDR's form (private, base address, mask
// 8..29), a name of the project's subnets, a CIDR that overlaps a subnet
// of the same VPC, a VPC that is not the project's, a project without a
// default VPC and no vpc_id.
func TestSubnetCreate_RefusedBeforeTheQuestion(t *testing.T) {
	for _, c := range []struct {
		label string
		args  map[string]any
		vpcs  string
		want  string
	}{
		{"no cidr", map[string]any{"name": "web"}, "", "cidr"},
		{"not a cidr", map[string]any{"name": "web", "cidr": "10.10.0.0"}, "", "cidr"},
		{"public", map[string]any{"name": "web", "cidr": "8.8.8.0/24"}, "", "private"},
		{"not the base", map[string]any{"name": "web", "cidr": "10.10.0.5/24"}, "", "10.10.0.0/24"},
		{"mask 30", map[string]any{"name": "web", "cidr": "10.10.0.0/30"}, "", "29"},
		{"mask 7", map[string]any{"name": "web", "cidr": "10.0.0.0/7"}, "", "8"},
		{"bad name", map[string]any{"name": "1web", "cidr": "10.10.0.0/24"}, "", "name"},
		{"taken name", map[string]any{"name": "old", "cidr": "10.10.0.0/24"}, "", subnetOld},
		{"overlap", map[string]any{"name": "web", "cidr": "10.0.0.128/25"}, "", "old"},
		{"vpc of another project", map[string]any{"name": "web", "cidr": "10.10.0.0/24", "vpc_id": "00000000-0000-4000-8000-0000000000b9"}, "", "vpc_list"},
		{"no default vpc", map[string]any{"name": "web", "cidr": "10.10.0.0/24"}, fmt.Sprintf(`[{"id":%q,"name":"other","default":false}]`, subnetOtherVP), "vpc_id"},
		{"no vpc", map[string]any{"name": "web", "cidr": "10.10.0.0/24"}, `[]`, "vpc_create"},
		{"bad dns", map[string]any{"name": "web", "cidr": "10.10.0.0/24", "dns_servers": []any{"dns.example"}}, "", "dns_servers"},
	} {
		for _, confirmed := range []bool{false, true} {
			api := newSubnetAPI()
			if c.vpcs != "" {
				api.vpcs = c.vpcs
			}
			r := subnetRegistry(t, api)
			args := map[string]any{"zone": "ru.AZ-1", "confirmed": confirmed}
			for k, v := range c.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, r, "subnet_create", args)
			if !isErr || m["status"] == "confirmation_required" || !strings.Contains(text, c.want) {
				t.Errorf("%s (confirmed=%v): want a refusal naming %q: %s", c.label, confirmed, c.want, text)
			}
			if len(api.writes) != 0 {
				t.Errorf("%s (confirmed=%v): writes %v", c.label, confirmed, api.writes)
			}
		}
	}
	// An address without its mask in the list still overlaps the network
	// that holds it.
	if sn := subnetOverlap([]*provision.Subnet{{Name: "bare", SubnetAddress: "10.10.0.0", VPCID: subnetVPC}}, subnetVPC,
		netip.MustParsePrefix("10.10.0.0/16")); sn == nil {
		t.Error("an address without a mask inside the network: no overlap found")
	}
	// The same CIDR in another VPC does not overlap.
	api := newSubnetAPI()
	r := subnetRegistry(t, api)
	m, text, _ := callNamed(t, r, "subnet_create", map[string]any{"name": "web", "cidr": "10.0.0.0/24", "zone": "ru.AZ-1", "vpc_id": subnetOtherVP})
	if m["status"] != "confirmation_required" {
		t.Errorf("a CIDR of another VPC refused: %s", text)
	}
}

// Axis 3: subnet_delete deletes what subnet_create made. A subnet the
// platform says cannot be deleted (can_delete false: a VM, a balancer or a
// cluster is still in it — the DELETE would be 405) is refused before the
// question and names the way out; one that is not there is already_deleted
// (D26), with no DELETE.
func TestSubnetDelete(t *testing.T) {
	api := newSubnetAPI()
	r := subnetRegistry(t, api)
	m, text, isErr := callNamed(t, r, "subnet_delete", map[string]any{"subnet_id": subnetOld})
	if !isErr || m["status"] != "confirmation_required" || !strings.Contains(text, "old") {
		t.Fatalf("preview: %s", text)
	}
	m, text, isErr = callNamed(t, r, "subnet_delete", map[string]any{"subnet_id": subnetOld, "confirmed": true})
	if isErr || m["status"] != "deleted" || len(api.writes) != 1 {
		t.Fatalf("delete: %s (writes %v)", text, api.writes)
	}

	api = newSubnetAPI()
	api.canDelete = false
	r = subnetRegistry(t, api)
	for _, confirmed := range []bool{false, true} {
		m, text, isErr = callNamed(t, r, "subnet_delete", map[string]any{"subnet_id": subnetOld, "confirmed": confirmed})
		hint, _ := m["hint"].(string)
		if !isErr || m["status"] == "confirmation_required" || !strings.Contains(hint, "list_subnets") && !strings.Contains(hint, "vm_list") {
			t.Errorf("can_delete=false, confirmed=%v: %s", confirmed, text)
		}
	}
	if len(api.writes) != 0 {
		t.Errorf("can_delete=false: writes %v", api.writes)
	}

	api = newSubnetAPI()
	api.gone = true
	r = subnetRegistry(t, api)
	m, text, _ = callNamed(t, r, "subnet_delete", map[string]any{"subnet_id": subnetOld})
	if m["status"] != "already_deleted" || len(api.writes) != 0 {
		t.Errorf("gone: %s (writes %v)", text, api.writes)
	}
}
