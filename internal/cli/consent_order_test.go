// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// lateRefusals are the calls known to ask for consent and then fail
// without a write (G13-3, class K1): "tool: case" → the work item of the
// release plan that moves the check before the question (W3…W6). The list
// only shrinks: a case not here fails the test, and so does a line whose
// call no longer fails that way.
var lateRefusals = map[string]string{
	"cert_private_key: certificate_id=absent id": "W6: the certificate looked up before the question",
	"dns_record_create: zone_id=absent id":       "W6: the zone looked up before the question",
	"fip_attach: fip=absent id":                  "W6: the floating IP looked up before the question",
}

// consentAbsentID is an id no resource of the world has: every API answers
// it 404.
const consentAbsentID = "00000000-0000-4000-8000-0000000000ad"

// consentWorld is a variant of the JSON world that breaks one precondition
// of a write; nil fields leave the world as it is. The bucket of the world
// already holds an object (s3Parity), so s3_bucket_delete always runs on a
// non-empty bucket and needs no variant.
type consentWorld struct {
	cloud  func(provision.CloudAPI) provision.CloudAPI
	answer func(r *http.Request) *http.Response
}

var consentWorlds = map[string]consentWorld{
	// provision, deploy and the creates that pick the zone's subnet.
	"zone without subnet": {cloud: func(c provision.CloudAPI) provision.CloudAPI { return noSubnetCloud{c} }},
	// disk_resize asks for 50 GB: the disk has 100, a shrink the API
	// refuses. No tool reads the size before the write yet: the variant
	// keeps the check of W5 before the question once it is there.
	"disk larger than requested": {answer: func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Host != "compute.api.cloud.ru" || !strings.HasPrefix(r.URL.Path, "/api/v1/disks/") {
			return nil
		}
		return consentJSON(http.StatusOK, `{"id":"`+parityVM+`","name":"old","size":100,"state":"available"}`)
	}},
}

type noSubnetCloud struct{ provision.CloudAPI }

func (noSubnetCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return nil, nil
}

func consentJSON(status int, body string) *http.Response {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
	return w.Result()
}

// consentCase is one call of a tool that needs consent, its arguments
// broken one way, or its world.
type consentCase struct {
	name  string
	key   string // the argument broken, "" for a variant of the world
	args  map[string]any
	world consentWorld
}

// consentCases: each integer argument of the schema as -1, 0 and 10⁹ (in a
// list too: ports), each id of parityArgs as an id the world has not, each
// string with a "format" as "bad", and the base arguments in each variant
// of the world.
func consentCases(tl tools.Tool, base map[string]any) []consentCase {
	var out []consentCase
	with := func(name, key string, v any) {
		a := copyArgs(base)
		a[key] = v
		out = append(out, consentCase{name: fmt.Sprintf("%s=%v", key, name), key: key, args: a})
	}
	props, _ := tl.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "confirmed" {
			continue
		}
		p, _ := props[k].(map[string]any)
		typ, _ := p["type"].(string)
		if typ == "array" {
			if items, _ := p["items"].(map[string]any); items != nil {
				typ = "[]" + fmt.Sprint(items["type"])
			}
		}
		for _, n := range []float64{-1, 0, 1e9} {
			switch typ {
			case "integer", "number":
				with(fmt.Sprintf("%g", n), k, n)
			case "[]integer", "[]number":
				with(fmt.Sprintf("[%g]", n), k, []any{n})
			}
		}
		if typ != "string" {
			continue
		}
		if v, _ := base[k].(string); looksParityID(v) {
			with("absent id", k, consentAbsentID)
		}
		if _, ok := p["format"]; ok {
			with("bad", k, "bad")
		}
	}
	names := make([]string, 0, len(consentWorlds))
	for n := range consentWorlds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, consentCase{name: "world: " + n, args: copyArgs(base), world: consentWorlds[n]})
	}
	return out
}

// looksParityID: an id of the world (parityVM, the zone, the cluster).
func looksParityID(v string) bool {
	return uuidInPath.MatchString(v) || v == addonClusterID
}

// CLAUDE.md §4: a call that is bound to fail is refused before the
// question of consent. In the JSON world every tool that needs consent is
// called with confirmed=true and each of its consentCases while the world
// refuses every write; a call that fails without trying one failed on a
// check the preview could have made, so the same call without confirmed
// must give that refusal (the same reason), not confirmation_required.
// It sees only what the world models: the rules of the platform it does
// not know (4xx after a write) are the world of 4xx of 1.4 (G13-3, K1;
// release plan 1.3, W8).
func TestConsent_RefusalWithoutWriteComesBeforeTheQuestion(t *testing.T) {
	w, fake := withJSONWorld(t)
	srv := newROServers(t)
	var (
		mu       sync.Mutex
		variant  consentWorld
		refuse   bool
		attempts int
	)
	world := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		v, refusing := variant, refuse
		write := r.Method != http.MethodGet && r.Method != http.MethodHead &&
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") // the token of a kubeconfig
		if write && refusing {
			attempts++
		}
		mu.Unlock()
		if write && refusing {
			return consentJSON(http.StatusForbidden, `{"code":"FORBIDDEN","message":"the test refuses every write"}`), nil
		}
		if strings.Contains(r.URL.Path, consentAbsentID) || strings.Contains(r.URL.RawQuery, consentAbsentID) {
			return consentJSON(http.StatusNotFound, `{"code":"NOT_FOUND","message":"not found"}`), nil
		}
		if v.answer != nil {
			if resp := v.answer(r); resp != nil {
				return resp, nil
			}
		}
		return world(r)
	}
	jsonEngine := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := jsonEngine(o)
		mu.Lock()
		v := variant
		mu.Unlock()
		if err == nil && v.cloud != nil {
			e.Cloud = v.cloud(e.Cloud)
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = jsonEngine })

	call := func(tl tools.Tool, c consentCase, confirmed bool) (map[string]any, bool, int) {
		a := copyArgs(c.args)
		done := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, teardown := setup(t, w)
			for k, v := range extra {
				if k != c.key {
					a[k] = v
				}
			}
			done = teardown
		}
		if confirmed {
			a["confirmed"] = true
		}
		fake.reset()
		w.rt.takeWrites()
		mu.Lock()
		variant, refuse, attempts = c.world, confirmed, 0
		mu.Unlock()
		ans := first(srv.stdio(t, srv.plain, "tools/call", map[string]any{"name": tl.Name, "arguments": a}))
		mu.Lock()
		n := attempts
		variant, refuse = consentWorld{}, false
		mu.Unlock()
		done()
		if c.key != "" {
			// A broken argument reaches paths the fixtures need not
			// answer (a version -1 of a certificate): withParityWorld
			// checks the requests of the base arguments only.
			w.rt.takeUnknown()
		}
		return parseObject(ans.text), ans.isErr, n
	}

	found := map[string]string{}
	for _, tl := range newRegistry().Tools() {
		base, ok := parityArgs[tl.Name]
		if !ok || !tools.NeedsConsent(tl, base) {
			continue
		}
		for _, c := range consentCases(tl, base) {
			after, failed, writes := call(tl, c, true)
			if !failed || writes > 0 {
				continue
			}
			reason, _ := after["reason"].(string)
			before, _, _ := call(tl, c, false)
			key := tl.Name + ": " + c.name
			switch {
			case before["status"] == "confirmation_required":
				found[key] = fmt.Sprintf("asks for consent, then fails with %s: %s", reason, clipLine(fmt.Sprint(after["error"])))
			case before["reason"] != reason:
				found[key] = fmt.Sprintf("refuses with %v before the question, with %s after it", before["reason"], reason)
			}
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if lateRefusals[k] == "" {
			t.Errorf("%s: %s — check it before the question, or, while the plan moves it, add it to lateRefusals with the work item", k, found[k])
		}
	}
	for k := range lateRefusals {
		if found[k] == "" {
			t.Errorf("lateRefusals names %s, which no longer fails after consent without a write: remove the line", k)
		}
	}
}
