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
// whenever the answer has them, whatever the tool declares — an answer of
// success and an error alike; a tool whose answer is its service's body
// keeps its error otherwise evo's (owner's decision 17.11, #2254, option Б).
func TestUntrusted_AlwaysFieldsMarkedByTheOnePoint(t *testing.T) {
	answer := `{"status":"partial","error":"e","hint":"h","warnings":["w"],"failures":[{"x":1}],"failed":["a"],"preview":{"name":"n"},"note":"evo"}`
	for _, c := range []struct {
		name  string
		u     untrustedAnswer
		isErr bool
		want  []string
	}{
		{"no declaration", untrustedAnswer{}, false, alwaysUntrusted},
		{"no declaration, an error", untrustedAnswer{}, true, alwaysUntrusted},
		{"declared fields", untrustedAnswer{fields: []string{"note"}}, false, append([]string{"note"}, alwaysUntrusted...)},
		{"whole, an error", untrustedAnswer{whole: true}, true, alwaysUntrusted},
	} {
		h := markUntrusted(c.u, func(context.Context, map[string]any) (string, bool) { return answer, c.isErr })
		text, _ := h(context.Background(), nil)
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatal(err)
		}
		got := stringList(m["_untrusted"])
		for _, f := range c.want {
			if !slices.Contains(got, f) {
				t.Errorf("%s: _untrusted %v lacks %s", c.name, got, f)
			}
		}
		if c.name == "whole, an error" && slices.Contains(got, "status") {
			t.Errorf("%s: an error of a whole answer marks its status: %v", c.name, got)
		}
	}
	plain := markUntrusted(untrustedAnswer{}, func(context.Context, map[string]any) (string, bool) { return `{"status":"ok"}`, false })
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
