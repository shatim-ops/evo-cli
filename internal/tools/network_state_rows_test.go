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
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A create sees a listed resource of the requested name (destination for
// routes). A delete sees the requested ID. These are client decisions;
// an accepted simulated write does not establish provider transition rules.
var networkOccupiedCompute = map[string]string{"created": "occupied", "error": "occupied", "error_creating": "occupied", "error_deleting": "occupied", "creating": "occupied", "deleting": "occupied", "updating": "occupied"}
var networkDeleteCompute = map[string]string{"created": "send", "error": "send", "error_creating": "send", "error_deleting": "send", "creating": "refuse", "deleting": "underway", "updating": "send"}
var networkStateRows = map[string]map[string]string{
	"fip_attach":          {"available": "send", "in_use": "refuse", "error": "refuse", "error_creating": "refuse", "error_deleting": "refuse", "creating": "refuse", "deleting": "refuse", "updating": "refuse", "attaching": "refuse", "detaching": "refuse"},
	"release_floating_ip": {"available": "send", "in_use": "refuse", "error": "send", "error_creating": "send", "error_deleting": "send", "creating": "refuse", "deleting": "send", "updating": "send", "attaching": "send", "detaching": "send"},
	"snat_create":         networkOccupiedCompute, "subnet_create": networkOccupiedCompute,
	"snat_delete": networkDeleteCompute, "subnet_delete": networkDeleteCompute,
	"vpc_route_create": {"ROUTE_STATUS_UNSPECIFIED": "occupied", "ROUTE_STATUS_NEW": "occupied", "ROUTE_STATUS_CREATING": "occupied", "ROUTE_STATUS_CREATED": "occupied", "ROUTE_STATUS_FAILED_CREATE": "occupied", "ROUTE_STATUS_DELETING": "occupied", "ROUTE_STATUS_DELETED": "occupied", "ROUTE_STATUS_FAILED_DELETE": "occupied"},
	"vpc_route_delete": {"ROUTE_STATUS_UNSPECIFIED": "send", "ROUTE_STATUS_NEW": "send", "ROUTE_STATUS_CREATING": "send", "ROUTE_STATUS_CREATED": "send", "ROUTE_STATUS_FAILED_CREATE": "send", "ROUTE_STATUS_DELETING": "send", "ROUTE_STATUS_DELETED": "send", "ROUTE_STATUS_FAILED_DELETE": "send"},
}

func init() { addStateRows(stateRowsOf, networkStateRows) }

const networkRoutePath = "/v1/vpcs/" + subnetVPC + "/routes/static"
const networkDest = "192.0.2.0/24"

type networkStateCall struct {
	args                  map[string]any
	method, path, success string
	body                  map[string]any
}

var networkStateCalls = map[string]networkStateCall{
	"fip_attach":          {map[string]any{"fip": subnetOld, "vm": "web"}, "PUT", "/api/v1/floating-ips/" + subnetOld, "attached", map[string]any{"name": "old", "interface_id": subnetNew}},
	"release_floating_ip": {map[string]any{"id": subnetOld}, "DELETE", "/api/v1/floating-ips/" + subnetOld, "ok", nil},
	"snat_create":         {map[string]any{"name": "old", "availability_zone_id": subnetZone}, "POST", "/api/v1/nat-gateways", "created", map[string]any{"name": "old", "availability_zone_id": subnetZone, "project_id": raProject, "nat_type": provision.NATTypePublicSNAT}},
	"snat_delete":         {map[string]any{"id": subnetOld}, "DELETE", "/api/v1/nat-gateways/" + subnetOld, "deleted", nil},
	"subnet_create":       {map[string]any{"name": "old", "availability_zone_id": subnetZone, "vpc_id": subnetVPC, "cidr": "10.10.0.0/24"}, "POST", "/api/v1/subnets", "created", map[string]any{"name": "old", "availability_zone_id": subnetZone, "project_id": raProject, "vpc_id": subnetVPC, "subnet_address": "10.10.0.0/24", "routed_network": true}},
	"subnet_delete":       {map[string]any{"subnet_id": subnetOld}, "DELETE", "/api/v1/subnets/" + subnetOld, "deleted", nil},
	"vpc_route_create":    {map[string]any{"subnet": networkDest, "vpc_id": subnetVPC, "next_hop_vip_id": subnetZone}, "POST", networkRoutePath, "creating", map[string]any{"subnet": networkDest, "vpcId": subnetVPC, "projectId": raProject, "nextHop": map[string]any{"vip": map[string]any{"vips": []any{map[string]any{"id": subnetZone}}}}}},
	"vpc_route_delete":    {map[string]any{"vpc_id": subnetVPC, "route_id": subnetOld}, "DELETE", networkRoutePath + "/" + subnetOld, "deleting", nil},
}

func testNetworkState(t *testing.T, tool, state, want string) {
	t.Helper()
	faultToolsIsolate(t)
	base := newSubnetAPI()
	call := networkStateCalls[tool]
	row := map[string]any{"id": subnetOld, "name": "old", "state": state, "project_id": raProject}
	made := false
	writes, stateReads := 0, 0
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		reply := func(v any) (*http.Response, error) {
			if err := json.NewEncoder(rec).Encode(v); err != nil {
				return nil, err
			}
			return rec.Result(), nil
		}
		path := r.URL.Path
		if r.Method != http.MethodGet {
			writes++
			var body map[string]any
			if r.Body != nil {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				if len(data) > 0 {
					if err := json.Unmarshal(data, &body); err != nil {
						return nil, err
					}
				}
			}
			if r.Method != call.method || path != call.path || !reflect.DeepEqual(body, call.body) {
				t.Errorf("write %s %s body %#v; want %s %s %#v", r.Method, path, body, call.method, call.path, call.body)
			}
			if (strings.HasPrefix(path, "/api/v1/floating-ips/") || strings.HasPrefix(path, "/api/v1/nat-gateways")) && r.URL.Query().Get("project_id") != raProject {
				t.Errorf("unscoped write %s", r.URL)
			}
			if strings.HasPrefix(path, networkRoutePath) {
				return reply(map[string]any{"id": faultID(32), "resourceId": subnetNew, "done": false})
			}
			made = true
			return reply(map[string]any{"id": subnetNew, "name": "old", "state": "created"})
		}
		switch path {
		case "/api/v1/vms":
			return reply(map[string]any{"items": []any{map[string]any{"id": subnetVPC, "name": "web", "state": "running"}}, "total": 1})
		case "/api/v1/vms/" + subnetVPC:
			return reply(map[string]any{"id": subnetVPC, "name": "web", "interfaces": []any{map[string]any{"primary": true, "id": subnetNew}}})
		case "/v1/vpcs/" + subnetVPC:
			return reply(map[string]any{"id": subnetVPC, "name": "default", "projectId": raProject})
		case "/api/v1/floating-ips", "/api/v1/floating-ips/" + subnetOld:
			row["ip_address"] = "192.0.2.40"
		case "/api/v1/subnets", "/api/v1/subnets/" + subnetOld:
			row["can_delete"], row["vpc_id"], row["subnet_address"] = true, subnetVPC, "10.0.0.0/24"
		case "/api/v1/nat-gateways":
			row["availability_zone_id"] = subnetZone
		case networkRoutePath:
			row["subnet"], row["status"], row["vpcId"] = networkDest, state, subnetVPC
		default:
			return base.RoundTrip(r)
		}
		stateReads++
		if path == "/api/v1/floating-ips/"+subnetOld || path == "/api/v1/subnets/"+subnetOld {
			return reply(row)
		}
		rows := []any{row}
		if state == "" && !made {
			rows = []any{}
		}
		if made {
			rows = []any{map[string]any{"id": subnetNew, "name": "old", "state": "created", "availability_zone_id": subnetZone, "project_id": raProject}}
		}
		key := "items"
		if path == networkRoutePath {
			key = "routes"
		}
		return reply(map[string]any{key: rows, "total": len(rows)})
	})
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: tr}, "")
	cloud.SetToken("t")
	r := raRegistry(t, tr, cloud, nil)
	root, _ := r.stateDir()
	hdir := history.DirOf(intent.Dir(root))
	for _, yes := range []bool{false, true} {
		before := stateReads
		out, text, failed := callNamed(t, r, tool, withConfirmed(call.args, yes))
		expectedWrites := 0
		switch want {
		case "send":
			if yes {
				expectedWrites = 1
				if failed || out["status"] != call.success {
					t.Fatal(text)
				}
			} else if out["status"] != "confirmation_required" {
				t.Fatal(text)
			}
		case "occupied":
			if !failed || out["reason"] != "USAGE" || out["existing_id"] != subnetOld {
				t.Fatal(text)
			}
		case "refuse":
			if !failed || out["reason"] != "USAGE" {
				t.Fatal(text)
			}
		case "underway":
			if failed || out["already_deleting"] != true || out["resource_status"] != state {
				t.Fatal(text)
			}
		default:
			t.Fatal(want)
		}
		if writes != expectedWrites || stateReads == before {
			t.Fatalf("confirmed=%v reads %d, writes %d; %s", yes, stateReads-before, writes, text)
		}
		j, err := history.ReadAll(hdir, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range j.Lines {
			if expectedWrites == 0 && (line.Outcome == history.Created || line.Outcome == history.Deleted || line.Outcome == history.Changed) {
				t.Fatalf("recorded a write without dispatch: %+v", line)
			}
			if line.CreateReceipt && (expectedWrites == 0 || line.Tool != tool || line.ResourceID != subnetNew) {
				t.Fatalf("unearned create receipt %+v", line)
			}
		}
		if want == "send" && yes && strings.HasSuffix(tool, "_create") {
			if len(j.Lines) != 1 || j.Lines[0].Outcome != history.Created || j.Lines[0].CreateReceipt != (tool == "subnet_create") || j.Lines[0].ResourceID != subnetNew {
				t.Fatalf("expected create receipt for %s: %+v", subnetNew, j.Lines)
			}
		}
	}
}

func TestNetworkStates_PerSpec(t *testing.T) {
	for tool, rows := range networkStateRows {
		for state, want := range rows {
			t.Run(tool+"/"+state, func(t *testing.T) { testNetworkState(t, tool, state, want) })
		}
	}
	// With the same arguments but a free name/destination the create can reach
	// its write. The bound UUID, network and project are checked above.
	for _, tool := range []string{"snat_create", "subnet_create", "vpc_route_create"} {
		t.Run(fmt.Sprintf("%s/free", tool), func(t *testing.T) { testNetworkState(t, tool, "", "send") })
	}
}
