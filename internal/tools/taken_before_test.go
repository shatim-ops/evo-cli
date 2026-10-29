// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// takenBefore is the resource of the world that was there before the call:
// its name is the one the create is given, it was made long ago, and it is
// in every list.
const takenBefore = "0f0f0000-0000-4000-8000-0000000000f0"

// takenArgs are the arguments of each create of createdIDs that has a list
// (lostCreate looks its resource up by name there), its name the world's
// taken one ("web"), the rest as a valid call has them (parityArgs of
// internal/cli).
var takenArgs = map[string]map[string]any{
	"ar_create":         {"name": "web"},
	"ca_deploy":         {"name": "web", "image": "nginx:1.27"},
	"cert_import":       {"name": "web", "server_certificate": "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t", "private_key": "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t"},
	"cert_issue_le":     {"name": "web", "dns_names": []any{"web.example.com"}},
	"disk_create":       {"name": "web", "size_gb": float64(20)},
	"dns_record_create": {"zone_id": takenBefore, "name": "web", "record_type": "A", "value": "192.0.2.10"},
	"dns_zone_create":   {"name": "web", "domain": "web.example.com."},
	"image_create":      {"name": "web", "disk_id": contractID},
	"k8s_add_nodepool":  {"cluster_id": contractID, "name": "web", "zone": takenBefore},
	"k8s_create":        {"name": "web", "zone": contractID, "cluster_sa_id": contractID},
	"kafka_create":      {"name": "web"},
	"lb_create":         {"name": "web", "target_group_id": contractID, "port": float64(80), "vpc_id": contractID, "zone": takenBefore},
	"pg_backup_create":  {"cluster_id": contractID, "name": "web"},
	"pg_create":         {"name": "web"},
	"provision":         {"name": "web"},
	"redis_create":      {"name": "web"},
	"secret_create":     {"path": "web", "value": "v"},
	"snat_create":       {"name": "web", "availability_zone_id": contractID},
	"tg_create":         {"name": "web", "vm_id": contractID},
	"vpc_create":        {"name": "web"},
	"vpc_route_create":  {"vpc_id": contractID, "subnet": "192.0.2.0/24", "next_hop_vm_id": contractID, "next_hop_interface_id": contractID},
}

// takenAdopts are the creates known to take the resource that was there
// before them for the one they made, when their answer is lost: the work
// item that fixes each. The list only shrinks.
var takenAdopts = map[string]string{}

// takenNotReached are the creates the world cannot bring to their write
// yet, with why: each is a case the test does not see until the world
// models it. The list only shrinks.
var takenNotReached = map[string]string{}

// lostCreate answers a create whose answer was lost by the resource with
// its name, "free before the create" (namecheck.go). A resource that was
// there before the call is not the call's: taken as made, its id goes to a
// stack step and to the person, and stack_destroy or a cleanup deletes
// what someone else made (QA R3/R8; the VM of a stack:
// TestStackVMStep_ForeignVMWithSameNameIsNotAdopted). Every create of
// createdIDs with a list, its name taken long before and its write's answer
// lost: it refuses the taken name, or answers the error, and never names
// the resource that was there as the one it made. Release plan 1.5, «„ресурс
// был до нас“ для каждого create».
func TestCreate_NeverTakesWhatWasThereBefore(t *testing.T) {
	// The object is every list's: a zone the DNS record, the balancer and
	// the node pool are made in (zoneId: the zones of Managed Kubernetes),
	// a flavor of the node pool (cpu, ram, no GPU).
	obj := `{"id":"` + takenBefore + `","flavorId":"` + takenBefore + `","zoneId":"` + takenBefore + `","name":"web","path":"web","status":"ACTIVE","state":"active","cpu":2,"ram":4,"created_at":"2020-01-01T00:00:00Z"}`
	fields := map[string]bool{"items": true, "flavors": true, "zones": true}
	for _, c := range createdIDs {
		if c.field != "" {
			fields[c.field] = true
		}
	}
	var parts []string
	for f := range fields {
		parts = append(parts, fmt.Sprintf("%q:[%s]", f, obj))
	}
	sort.Strings(parts)
	// The answer to the write is cut short: the create cannot read what it
	// made, and lostCreate looks it up by name.
	rt := &contractRT{kind: "200",
		body:      "{" + strings.Join(parts, ",") + `,"total":1,"id":"` + takenBefore + `","name":"web","status":"ACTIVE","done":true}`,
		writeBody: `{"truncated`}
	reg := contractReg(t, rt)
	names := []string{}
	for n, c := range createdIDs {
		if c.list != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		tl, ok := reg.Lookup(n)
		if !ok {
			t.Errorf("createdIDs names %s, which is no tool", n)
			continue
		}
		a, ok := takenArgs[n]
		if !ok {
			t.Errorf("%s: a create with a list and no line in takenArgs", n)
			continue
		}
		args := copyArgsMap(a)
		args["confirmed"] = true
		text, isErr, pan := contractCall(tl, args, contractCallLimit)
		writes := contractWrites(rt.take())
		if pan != "" {
			t.Errorf("%s: panicked", n)
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		msg, _ := m["error"].(string)
		reached := writes > 0 || strings.Contains(msg, "already exists")
		takes := !isErr && strings.Contains(text, takenBefore)
		switch {
		case takes && takenAdopts[n] == "":
			t.Errorf("%s: its answer lost, it takes the resource that was there before for the one it made: %s", n, clip(text))
		case !takes && takenAdopts[n] != "":
			t.Errorf("takenAdopts names %s, which no longer takes what was there before: remove the line", n)
		}
		switch {
		case !reached && takenNotReached[n] == "":
			t.Errorf("%s: the world did not bring it to its write (%s): fix takenArgs, or say why in takenNotReached", n, clip(msg))
		case reached && takenNotReached[n] != "":
			t.Errorf("takenNotReached names %s, which the world brings to its write now: remove the line", n)
		}
	}
	for n := range takenArgs {
		if c, ok := createdIDs[n]; !ok || c.list == "" {
			t.Errorf("takenArgs names %s, which is no create of createdIDs with a list", n)
		}
	}
}
