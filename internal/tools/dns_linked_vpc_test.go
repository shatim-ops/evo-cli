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
	"strings"
	"sync"
	"testing"
)

// dnsVPCAPI is the zones of DNS over the VPCs of subnetAPI: no zone yet; a
// zone create is recorded and answers an operation; the DNS servers are
// those of servers (nil: one in each VPC of subnetAPI).
type dnsVPCAPI struct {
	*subnetAPI
	mu      sync.Mutex
	zone    map[string]any // the body of the zone create
	servers []string       // the VPC of each DNS server
}

func (a *dnsVPCAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/dnsServers" {
		vpcs := a.servers
		if vpcs == nil {
			vpcs = []string{subnetVPC, subnetOtherVP}
		}
		var items []string
		for i, v := range vpcs {
			items = append(items, fmt.Sprintf(`{"id":"dns-%d","vpcId":%q,"subnetId":"sn-%d","ip":"10.0.0.%d"}`, i, v, i, i+2))
		}
		_, _ = fmt.Fprintf(rec, `{"servers":[%s]}`, strings.Join(items, ","))
		return rec.Result(), nil
	}
	if !strings.HasSuffix(r.URL.Path, "Zones") {
		return a.subnetAPI.RoundTrip(r)
	}
	if r.Method == http.MethodPost {
		raw, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		_ = json.Unmarshal(raw, &a.zone)
		a.mu.Unlock()
		_, _ = io.WriteString(rec, `{"id":"op-1","done":false}`)
		return rec.Result(), nil
	}
	_, _ = io.WriteString(rec, `{"zones":[],"total":0}`)
	return rec.Result(), nil
}

// Release plan 1.6 (#597, п. 15; axis 4): a private zone resolves only in
// the VPCs linked to it — one without them is a zone nothing resolves.
// dns_zone_create links the VPC of vpc_id, else the project's default VPC,
// names it in the preview and sends it (linkedVpcIds); a project without
// a default VPC and no vpc_id is refused before the question, without a
// write; a public zone takes no VPC.
func TestDNSZoneCreate_PrivateZoneLinksAVPC(t *testing.T) {
	api := &dnsVPCAPI{subnetAPI: newSubnetAPI()}
	r := raRegistry(t, api, nil, nil)
	args := map[string]any{"domain": "corp.internal.", "type": "private"}
	m, text, _ := callNamed(t, r, "dns_zone_create", args)
	if m["status"] != "confirmation_required" || !strings.Contains(text, subnetVPC) || !strings.Contains(text, `"vpc_name"`) {
		t.Fatalf("the preview does not name the VPC the zone is linked to: %s", text)
	}
	args["confirmed"] = true
	if _, text, isErr := callNamed(t, r, "dns_zone_create", args); isErr {
		t.Fatalf("create: %s", text)
	}
	if got := fmt.Sprint(api.zone["linkedVpcIds"]); got != "["+subnetVPC+"]" {
		t.Errorf("POST linkedVpcIds = %s, want [%s] (body %v)", got, subnetVPC, api.zone)
	}

	api.zone = nil
	args = map[string]any{"domain": "corp.internal.", "type": "private", "vpc_id": subnetOtherVP, "confirmed": true}
	if _, text, isErr := callNamed(t, r, "dns_zone_create", args); isErr || fmt.Sprint(api.zone["linkedVpcIds"]) != "["+subnetOtherVP+"]" {
		t.Errorf("vpc_id %s: %v, %s", subnetOtherVP, api.zone["linkedVpcIds"], text)
	}

	for _, c := range []struct {
		name, reason string
		args         map[string]any
		vpcs         string
		servers      []string
	}{
		{"no default VPC", "USAGE", map[string]any{"domain": "corp.internal.", "type": "private"}, fmt.Sprintf(`[{"id":%q,"name":"other","default":false}]`, subnetOtherVP), nil},
		{"a VPC not of the project", "NOT_FOUND", map[string]any{"domain": "corp.internal.", "type": "private", "vpc_id": "00000000-0000-4000-8000-0000000000ff"}, "", nil},
		{"a public zone with a VPC", "USAGE", map[string]any{"domain": "example.com.", "vpc_id": subnetVPC}, "", nil},
		// A zone links to a VPC through its DNS server: «предварительно
		// создайте в этой сети хотя бы один DNS-сервер» (Evolution DNS,
		// «Приватные зоны»); without one the platform fails the create
		// (cloud-e2e run 150, code 13).
		{"the default VPC without a DNS server", "USAGE", map[string]any{"domain": "corp.internal.", "type": "private"}, "", []string{subnetOtherVP}},
		{"vpc_id without a DNS server", "USAGE", map[string]any{"domain": "corp.internal.", "type": "private", "vpc_id": subnetOtherVP}, "", []string{subnetVPC}},
		{"no DNS server in the project", "USAGE", map[string]any{"domain": "corp.internal.", "type": "private"}, "", []string{}},
	} {
		api.zone = nil
		api.servers = c.servers
		if c.vpcs != "" {
			api.vpcs = c.vpcs
		}
		m, text, isErr := callNamed(t, r, "dns_zone_create", c.args)
		if !isErr || m["reason"] != c.reason || api.zone != nil {
			t.Errorf("%s: want %s before the question, no write: %s", c.name, c.reason, text)
		}
		api.subnetAPI = newSubnetAPI()
	}
}
