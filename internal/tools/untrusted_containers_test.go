// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"sort"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// untrustedContainers are the fields of an answer into which evo carries
// the words of the cloud along with its own: an error, warnings, the
// failures of a part, a preview, a hint. Any answer of any tool that has
// one marks it in "_untrusted", with no declaration of the tool (owner's
// decision 17.11 on #2254, variant B: a forgotten declaration falls on the
// safe side; #2177, #2243–#2247).
var untrustedContainers = []string{"error", "warnings", "failures", "failed", "preview", "hint"}

// unmarkedContainers are the fields of untrustedContainers answer m has
// and its "_untrusted" does not name.
func unmarkedContainers(m map[string]any) []string {
	var marked []string
	switch u := m["_untrusted"].(type) {
	case []any:
		for _, f := range u {
			if s, ok := f.(string); ok {
				marked = append(marked, s)
			}
		}
	case []string:
		marked = u
	}
	var out []string
	for _, f := range untrustedContainers {
		if v, has := m[f]; has && v != nil && !slices.Contains(marked, f) {
			out = append(out, f)
		}
	}
	return out
}

// containerWorld is a world a tool answers in: the cloud's answers, and
// whether the call carries the consent.
type containerWorld struct {
	name      string
	api       http.RoundTripper
	confirmed bool
}

// Every answer of every tool that has a field of untrustedContainers
// names it in "_untrusted", in each world an answer comes from: a preview
// before the consent, a success and a partial success after it, a refusal
// of the API, a connection that breaks after a read. The worlds give the
// cloud's names with a marker (hostileBody); the check is on the fields,
// whatever text is in them: evo's own words in them are marked too
// (#2254, the cost of variant B).
func TestUntrustedContainers_MarkedInEveryAnswer(t *testing.T) {
	faultToolsIsolate(t)
	body := hostileBody()
	var worlds []containerWorld
	for _, confirmed := range []bool{false, true} {
		worlds = append(worlds,
			containerWorld{"answers of the cloud", &oddAPI{status: http.StatusOK, body: body}, confirmed},
			containerWorld{"refusals of the API (422)", &oddAPI{status: http.StatusUnprocessableEntity, body: body}, confirmed},
			containerWorld{"not found (404)", &oddAPI{status: http.StatusNotFound, body: body}, confirmed})
		for from := 2; from <= 4; from++ {
			worlds = append(worlds, containerWorld{"a connection broken from a later request", &brokenAfterAPI{body: body, from: from}, confirmed})
		}
	}
	seen := map[string]int{}
	for _, w := range worlds {
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w.api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, w.api, cloud, nil)
		for _, tl := range r.Tools() {
			for _, args := range requiredCalls(tl) {
				args = maps.Clone(args)
				if w.confirmed && tl.Mutating {
					args["confirmed"] = true
				}
				_, text, _ := callNamed(t, r, tl.Name, args)
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil {
					continue
				}
				for _, f := range untrustedContainers {
					if _, has := m[f]; has {
						seen[f]++
					}
				}
				if bad := unmarkedContainers(m); len(bad) > 0 {
					t.Errorf("%s %v, %s (confirmed %v): %v not in _untrusted %v", tl.Name, args, w.name, w.confirmed, bad, m["_untrusted"])
				}
			}
		}
	}
	// The worlds must reach each field: a field no answer has is no check.
	var missing []string
	for _, f := range untrustedContainers {
		if seen[f] == 0 {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no answer of the worlds has %v: the check does not see them", missing)
	}
	t.Logf("answers with a field of untrustedContainers: %v", seen)
}

// The check of a field: named — passes; not named, or a marker of another
// field — fails; absent or null — nothing to mark.
func TestUntrustedContainers_Check(t *testing.T) {
	for _, c := range []struct {
		m    map[string]any
		want []string
	}{
		{map[string]any{"error": "x", "_untrusted": []any{"error"}}, nil},
		{map[string]any{"error": "x", "_untrusted": []string{"error"}}, nil},
		{map[string]any{"error": "x"}, []string{"error"}},
		{map[string]any{"warnings": []any{"w"}, "preview": map[string]any{}, "_untrusted": []any{"preview"}}, []string{"warnings"}},
		{map[string]any{"hint": nil}, nil},
		{map[string]any{"message": "evo's"}, nil},
	} {
		if got := unmarkedContainers(c.m); !slices.Equal(got, c.want) {
			t.Errorf("%v: %v, want %v", c.m, got, c.want)
		}
	}
}
