// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The fields every answer marks (alwaysUntrusted) are in "_untrusted"
// whenever the answer has them, an answer of success and an error alike,
// beside what the tool and errAnswer mark (owner's decision 17.11, #2254,
// option Б).
func TestUntrusted_AlwaysFieldsMarkedByTheOnePoint(t *testing.T) {
	answer := `{"status":"partial","error":"e","hint":"h","warning":"w1","operation_id":"op","registry_id":"r","vpc_id":"v","zone_id":"z","record_id":"rc","route_id":"rt","backup_id":"b","vm_id":"vm","certificate_id":"c","tg_id":"tg","repeat_of":{"id":"x"},"existing_id":"existing","node_pools":[{"name":"np"}],"certificate":{"name":"c"},"operation":{"id":"op"},"database":{"name":"db"},"user":{"name":"u"},"disk_id":"d","project_id":"p","warnings":["w"],"failures":[{"x":1}],"failed":["a"],"preview":{"name":"n"},"note":"evo","_untrusted":["note"]}`
	for _, isErr := range []bool{false, true} {
		h := markAlways(func(context.Context, map[string]any) (string, bool) { return answer, isErr })
		text, gotErr := h(context.Background(), nil)
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil || gotErr != isErr {
			t.Fatal(err)
		}
		got := stringList(m["_untrusted"])
		for _, f := range append([]string{"note"}, alwaysUntrusted...) {
			if !slices.Contains(got, f) {
				t.Errorf("isErr=%v: _untrusted %v lacks %s", isErr, got, f)
			}
		}
		if slices.Contains(got, "status") {
			t.Errorf("isErr=%v: a field not of the list is marked: %v", isErr, got)
		}
	}
	plain := markAlways(func(context.Context, map[string]any) (string, bool) { return `{"status":"ok"}`, false })
	if text, _ := plain(context.Background(), nil); text != `{"status":"ok"}` {
		t.Errorf("an answer with none of the fields changed: %s", text)
	}
}

// Every tool of the registry, in the world of hostile names: an answer that
// has a field of alwaysUntrusted names it in "_untrusted" — no tool is left
// out of the one point (#2177, #2243–#2247).
func TestUntrusted_EveryToolMarksTheAlwaysFields(t *testing.T) {
	faultToolsIsolate(t)
	api := &oddAPI{status: http.StatusOK, body: hostileBody()}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)
	seen := 0
	for _, tl := range r.Tools() {
		for _, args := range requiredCalls(tl) {
			m, text, _ := callNamed(t, r, tl.Name, args)
			got := stringList(m["_untrusted"])
			for _, f := range alwaysUntrusted {
				if _, has := m[f]; has {
					seen++
					if !slices.Contains(got, f) {
						t.Errorf("%s %v: %s not in _untrusted %v: %s", tl.Name, args, f, got, text)
					}
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no answer of the world had a field of alwaysUntrusted: the test sees nothing")
	}
}
