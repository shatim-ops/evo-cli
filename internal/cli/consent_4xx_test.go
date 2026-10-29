// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The reasons a 4xx a write handles is no rule of the consent world.
const (
	// The key or its rights: doctor and whoami say it before any call.
	ruleOfKey = "the key or its rights, before any call"
	// The platform's rate: no rule of the arguments.
	ruleOfRate = "the platform's rate, no rule of the call"
	// An id that is not there: consentCases calls every id of a tool as
	// an absent one, and D26 answers it.
	ruleAbsentID = "an absent id: consentCases and D26"
	// The call waits until the rule holds and writes once it does.
	ruleWaited = "the call waits until it holds"
	// A read: it asks for no consent.
	ruleRead = "a read, no consent"
	// A rule a read could tell before the question that the world does not
	// model yet: an axis 1 gap, with its work item.
	ruleGap = "gap"
)

// writeRules are the 4xx answers to a write that evo handles
// (scripts/api-contract.json, "statuses"), "METHOD path STATUS" → the
// variant of consentWorlds that models the rule, or why none does
// (CLAUDE.md §6, axis 1: what a read can tell is checked before the
// question). The list is closed both ways: a status the contract adds
// fails the test, and so does a line the contract no longer has, or a
// variant consentWorlds no longer has.
var writeRules = map[string]struct{ world, why, item string }{
	"POST /api/v1.1/vms 401": {why: ruleOfKey},
	"POST /api/v1.1/vms 403": {why: ruleOfKey},
	"POST /api/v1.1/vms 404": {world: "image not in the catalogue"},
	"POST /api/v1.1/vms 422": {world: "zone without subnet"},
	"POST /api/v1.1/vms 429": {why: ruleOfRate},

	"DELETE /api/v1/vms/{vm_id} 404": {why: ruleAbsentID},
	// A VM in creating (CMP-7): destroy reads the VM for its preview, and
	// asks for consent before it meets the refusal.
	"DELETE /api/v1/vms/{vm_id} 409": {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"DELETE /api/v1/vms/{vm_id} 422": {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},

	"POST /api/v1/images 409":                          {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"POST /api/v1/images 422":                          {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"DELETE /api/v1/images/{image_id} 404":             {why: ruleAbsentID},
	"POST /api/v1/floating-ips 422":                    {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"PUT /api/v1/floating-ips/{floating_ip_id} 404":    {why: ruleAbsentID},
	"PUT /api/v1/floating-ips/{floating_ip_id} 422":    {world: "floating IP in use"},
	"DELETE /api/v1/floating-ips/{floating_ip_id} 404": {why: ruleAbsentID},
	// error_deleting (NET-7): readable before the question.
	"DELETE /api/v1/floating-ips/{floating_ip_id} 422": {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},

	"DELETE /api/v1/security-groups/{security_group_id}/rules/{security_group_rule_id} 404": {why: ruleAbsentID},

	"POST /api/v1/disks 409":                  {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"POST /api/v1/disks 422":                  {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"POST /api/v1/disks/{disk_id}/attach 404": {why: ruleAbsentID},
	// «Disk has no openstack_id» (CMP-15): disk_attach waits for the disk.
	"POST /api/v1/disks/{disk_id}/attach 422": {why: ruleWaited},
	"POST /api/v1/disks/{disk_id}/detach 404": {why: ruleAbsentID},
	"POST /api/v1/disks/{disk_id}/detach 422": {world: "disk in a transient state"},
	"PUT /api/v1/disks/{disk_id} 404":         {why: ruleAbsentID},
	"PUT /api/v1/disks/{disk_id} 422":         {world: "disk larger than requested"},
	"DELETE /api/v1/disks/{disk_id} 404":      {why: ruleAbsentID},

	"POST /api/v1/subnets 409": {world: "subnet overlaps the VPC's"},
	// A closed zone, a quota: no read tells them all yet.
	"POST /api/v1/subnets 422":               {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"DELETE /api/v1/subnets/{subnet_id} 404": {why: ruleAbsentID},
	"DELETE /api/v1/subnets/{subnet_id} 405": {world: "subnet with resources"},

	"POST /api/v1/nat-gateways 422":                    {why: ruleGap, item: "release plan 1.5, «Миры 4xx (ось 1), пробелы consentWorlds» (#457)"},
	"DELETE /api/v1/nat-gateways/{nat_gateway_id} 404": {why: ruleAbsentID},

	"POST /api/v1/projects/{project_id}/price-calculation 422": {why: ruleRead},
	"POST /api/v1/auth/token 401":                              {why: ruleOfKey},
	"POST /api/v1/auth/token 403":                              {why: ruleOfKey},
}

// Each 4xx a write of evo handles is a rule of the consent world
// (TestConsent_RefusalWithoutWriteComesBeforeTheQuestion), or says why it
// is none: release plan 1.5, «миры 4xx из api-contract.json»; K1 (G13-3):
// a rule the world did not model asked for consent and then failed.
func TestConsent_EveryHandled4xxIsARuleOfTheWorld(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "api-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Services []struct {
			Requests []struct {
				Method   string `json:"method"`
				Path     string `json:"path"`
				Statuses []int  `json:"statuses"`
			} `json:"requests"`
		} `json:"services"`
	}
	if err := json.Unmarshal(b, &contract); err != nil {
		t.Fatal(err)
	}
	handled := map[string]bool{}
	for _, s := range contract.Services {
		for _, r := range s.Requests {
			if r.Method == "GET" {
				continue
			}
			for _, st := range r.Statuses {
				if st >= 400 && st < 500 {
					handled[fmt.Sprintf("%s %s %d", r.Method, r.Path, st)] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(handled))
	for k := range handled {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rule, ok := writeRules[k]
		switch {
		case !ok:
			t.Errorf("%s: a 4xx evo handles and no line in writeRules — model it in consentWorlds or say why it is no rule of the call", k)
		case rule.world != "":
			if _, ok := consentWorlds[rule.world]; !ok {
				t.Errorf("%s: writeRules names the world %q, which consentWorlds has not", k, rule.world)
			}
		case rule.why == ruleGap && rule.item == "":
			t.Errorf("%s: a gap without its work item", k)
		case rule.why == "":
			t.Errorf("%s: writeRules gives neither a world nor a reason", k)
		}
	}
	for k := range writeRules {
		if !handled[k] {
			t.Errorf("writeRules names %q, which scripts/api-contract.json no longer handles: remove the line", k)
		}
	}
}
