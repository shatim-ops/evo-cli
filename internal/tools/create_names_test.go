// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// createLike are the tools that make a resource: every *_create and the
// creates named otherwise.
func createLike(t Tool) bool {
	return t.Mutating && (strings.HasSuffix(t.Name, "_create") || slices.Contains([]string{
		"provision", "deploy", "deploy_apply", "ca_deploy", "k8s_add_nodepool", "k8s_add_addon",
		"cert_import", "cert_issue_le", "s3_put",
	}, t.Name))
}

// Every create has a rule for the name of what it makes (D30): the
// service's own, which its handler checks, or plainName, which the registry
// checks before the handler; a create that names nothing new says why.
func TestCreateNames_EveryCreateDeclared(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range New(nil).Tools() {
		c, declared := createNames[tl.Name]
		_, nameless := namelessCreates[tl.Name]
		switch {
		case createLike(tl) && !declared && !nameless:
			t.Errorf("%s makes a resource but is in neither createNames nor namelessCreates (D30: one rule for the name of a new resource)", tl.Name)
		case declared && nameless:
			t.Errorf("%s is in createNames and in namelessCreates", tl.Name)
		}
		if !declared {
			continue
		}
		seen[tl.Name] = true
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range c.args {
			if _, ok := props[a]; !ok {
				t.Errorf("%s: createNames names argument %q, which its schema has not", tl.Name, a)
			}
		}
	}
	for name := range createNames {
		if !seen[name] {
			t.Errorf("createNames names %s, which is not a tool", name)
		}
	}
	for name, why := range namelessCreates {
		if _, ok := New(nil).Lookup(name); !ok || why == "" {
			t.Errorf("namelessCreates: %s is not a tool, or no reason is given", name)
		}
	}
}

// A name with a space at either end, or blank, is refused before the
// cloud, confirmed or not, by every create: "web " passed by a taken "web"
// and made a second resource (Cursor 1.2.3; lb_create, ar_create and the
// others had no rule). So is one with a control character or longer than
// 255 characters (D30). The name is quoted in the refusal.
func TestCreateNames_SpacesRefusedBeforeTheCloud(t *testing.T) {
	var reqs atomic.Int64
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		reqs.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		return true
	}}
	r := apiFakeRegistry(t, f)
	names := make([]string, 0, len(createNames))
	for name := range createNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tl, _ := r.Lookup(name)
		for _, arg := range createNames[name].args {
			for _, bad := range []string{"web ", " web", "  ", "we\x00b", "web\n", strings.Repeat("w", 256)} {
				for _, confirmed := range []bool{false, true} {
					args := contractArgs(tl)
					if name == "k8s_create" {
						args["master_count"] = float64(1) // the sweep's 2 is refused first
					}
					args[arg] = bad
					args["confirmed"] = confirmed
					before := reqs.Load()
					m, text, isErr := callNamed(t, r, name, args)
					msg, _ := m["error"].(string)
					if !isErr || m["reason"] != "USAGE" || !strings.Contains(msg, strconv.Quote(bad)) {
						t.Errorf("%s %s=%q confirmed=%v: want USAGE naming the name: %.300s", name, arg, bad, confirmed, text)
					}
					if n := reqs.Load() - before; n != 0 {
						t.Errorf("%s %s=%q: %d requests before the refusal", name, arg, bad, n)
					}
				}
			}
		}
	}
}
