// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// twinOrder is how the order test lists things: every resource of a list
// gets a twin — the same resource under another id and name — and the
// second world lists them in reverse. A tool that picks by a rule picks the
// same in both worlds; one that takes the first of a list picks the twin in
// one of them (#234: k8s_create took the first flavor of the catalogue).
type twinOrder struct{ reversed bool }

// twinID is an id the fakes do not know, of the same form: a UUID stays a
// UUID, and twins of two ids stay two ids (the fakes' ids differ at the
// end).
func twinID(id string) string {
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return "77777777" + id[8:]
	}
	return id + "-twin"
}

// list is s with a twin after each item, in reverse when the world is.
func list[T any](o twinOrder, s []T, twin func(T) T) []T {
	out := make([]T, 0, 2*len(s))
	for _, e := range s {
		out = append(out, e, twin(e))
	}
	if o.reversed {
		slices.Reverse(out)
	}
	return out
}

// twinStruct is a copy of *T with its ID and Name fields renamed.
func twinStruct[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	v := reflect.ValueOf(&c).Elem()
	if f := v.FieldByName("ID"); f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
		f.SetString(twinID(f.String()))
	}
	if f := v.FieldByName("Name"); f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
		f.SetString(f.String() + "-twin")
	}
	return &c
}

// twinMap is a copy of an object of the API with its id and name renamed.
func twinMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	if id, ok := c["id"].(string); ok && id != "" {
		c["id"] = twinID(id)
	}
	if n, ok := c["name"].(string); ok && n != "" {
		c["name"] = n + "-twin"
	}
	return c
}

// twinJSON gives every list of objects with an id in an answer of the API
// its twins; a list of anything else is only reversed.
func (o twinOrder) twinJSON(v any) any {
	switch x := v.(type) {
	case []any:
		items := make([]any, len(x))
		for i, e := range x {
			items[i] = o.twinJSON(e)
		}
		return list(o, items, func(e any) any {
			if m, ok := e.(map[string]any); ok {
				if _, hasID := m["id"].(string); hasID {
					return twinMap(m)
				}
			}
			return nil
		})
	case map[string]any:
		for k, e := range x {
			x[k] = o.twinJSON(e)
		}
	}
	return v
}

// twinRT answers as its base, the JSON answers' lists given twins.
type twinRT struct {
	o    twinOrder
	base http.RoundTripper
}

func (r twinRT) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "" && mt != "application/json" {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var v any
	if json.Unmarshal(body, &v) == nil {
		if b, err := json.Marshal(dropNil(r.o.twinJSON(v))); err == nil {
			body = b
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// dropNil removes the nil twins of items that have none.
func dropNil(v any) any {
	switch x := v.(type) {
	case []any:
		out := x[:0]
		for _, e := range x {
			if e != nil {
				out = append(out, dropNil(e))
			}
		}
		return out
	case map[string]any:
		for k, e := range x {
			x[k] = dropNil(e)
		}
	}
	return v
}

// twinCloud is the compute fake with twins in every list.
type twinCloud struct {
	provision.CloudAPI
	o twinOrder
}

func twins[T any](o twinOrder, s []*T, err error) ([]*T, error) {
	return list(o, s, twinStruct[T]), err
}

func (c twinCloud) ListFlavors(ctx context.Context) ([]*provision.Flavor, error) {
	s, err := c.CloudAPI.ListFlavors(ctx)
	return twins(c.o, s, err)
}
func (c twinCloud) ListAvailabilityZones(ctx context.Context) ([]*provision.AvailabilityZone, error) {
	s, err := c.CloudAPI.ListAvailabilityZones(ctx)
	return twins(c.o, s, err)
}
func (c twinCloud) ListImages(ctx context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	s, err := c.CloudAPI.ListImages(ctx, f...)
	return twins(c.o, s, err)
}
func (c twinCloud) ListImagesInProject(ctx context.Context, p string) ([]*provision.Image, error) {
	s, err := c.CloudAPI.ListImagesInProject(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListDiskTypes(ctx context.Context, p string) ([]*provision.DiskType, error) {
	s, err := c.CloudAPI.ListDiskTypes(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListSubnets(ctx context.Context, p string) ([]*provision.Subnet, error) {
	s, err := c.CloudAPI.ListSubnets(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListSecurityGroups(ctx context.Context, p string) ([]*provision.SecurityGroup, error) {
	s, err := c.CloudAPI.ListSecurityGroups(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListFloatingIPs(ctx context.Context, p string) ([]*provision.FloatingIP, error) {
	s, err := c.CloudAPI.ListFloatingIPs(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListVMsInProject(ctx context.Context, p string) ([]*provision.VM, error) {
	s, err := c.CloudAPI.ListVMsInProject(ctx, p)
	return twins(c.o, s, err)
}
func (c twinCloud) ListSecurityGroupRules(ctx context.Context, sg string) ([]*provision.SGRule, error) {
	s, err := c.CloudAPI.ListSecurityGroupRules(ctx, sg)
	return twins(c.o, s, err)
}
func (c twinCloud) ListNATGateways(ctx context.Context, p string) ([]map[string]any, error) {
	s, err := c.CloudAPI.ListNATGateways(ctx, p)
	return list(c.o, s, twinMap), err
}
func (c twinCloud) ListDisks(ctx context.Context, p string) ([]map[string]any, error) {
	s, err := c.CloudAPI.ListDisks(ctx, p)
	return list(c.o, s, twinMap), err
}

// sortedArrays is v with every array in a canonical order: two answers
// that list the same things in another order compare equal.
func sortedArrays(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		keys := make([]string, len(x))
		for i, e := range x {
			out[i] = sortedArrays(e)
			b, _ := json.Marshal(out[i])
			keys[i] = string(b)
		}
		idx := make([]int, len(x))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
		sorted := make([]any, len(x))
		for i, j := range idx {
			sorted[i] = out[j]
		}
		return sorted
	case map[string]any:
		for k, e := range x {
			x[k] = sortedArrays(e)
		}
	}
	return v
}

// canonical is an answer as the order test compares it: lists sorted, the
// ids and times each call makes anew read as one (roSame).
func canonical(text string) string {
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		return roSame(text)
	}
	b, _ := json.Marshal(sortedArrays(v))
	return roSame(string(b))
}

// firstDiff is where two answers part: a little before and after the
// first byte that differs, in each.
func firstDiff(a, b string) (string, string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	cut := func(s string) string {
		from, to := max(0, i-80), min(len(s), i+120)
		return s[from:to]
	}
	return cut(a), cut(b)
}

// orderDependent are the tools whose answer may depend on the order the
// cloud lists things in, each with why.
var orderDependent = map[string]string{}

// The same call on the same cloud answers the same, whatever order the
// cloud lists things in (owner's decision 22.10: the same command leads to
// the same result, no surprises). Every tool is called without consent —
// a read answers, a change shows its preview — on a world whose every list
// has twins, and on the same world listing them in reverse; the answers
// must be equal up to the order of their own lists.
func TestDeterminism_ListOrderDoesNotChangeTheAnswer(t *testing.T) {
	run := func(o twinOrder) map[string]string {
		out := map[string]string{}
		t.Run(map[bool]string{false: "api-order", true: "reversed"}[o.reversed], func(t *testing.T) {
			w, fake := withJSONWorld(t)
			http.DefaultTransport = twinRT{o: o, base: w.rt}
			world := toolEngine
			toolEngine = func(opts map[string]any) (*engine.Engine, error) {
				e, err := world(opts)
				if err != nil {
					return nil, err
				}
				e.Cloud = twinCloud{CloudAPI: e.Cloud, o: o}
				return e, nil
			}
			t.Cleanup(func() { toolEngine = world })
			srv := newROServers(t)
			for _, tl := range newRegistry().Tools() {
				args := copyArgs(parityArgs[tl.Name])
				done := func() {}
				if setup := paritySetup[tl.Name]; setup != nil {
					extra, d := setup(t, w)
					for k, v := range extra {
						args[k] = v
					}
					done = d
				}
				fake.reset()
				a := first(srv.stdio(t, srv.plain, "tools/call", map[string]any{"name": tl.Name, "arguments": args}))
				done()
				out[tl.Name] = map[bool]string{true: "isError ", false: "result "}[a.isErr] + canonical(a.text)
			}
		})
		return out
	}
	plain, reversed := run(twinOrder{}), run(twinOrder{reversed: true})
	if len(plain) == 0 || len(plain) != len(reversed) {
		t.Fatalf("answers: %d in API order, %d reversed", len(plain), len(reversed))
	}

	var bad []string
	seen := map[string]bool{}
	for name, want := range plain {
		got := reversed[name]
		if got == want {
			continue
		}
		seen[name] = true
		if orderDependent[name] != "" {
			continue
		}
		a, r := firstDiff(want, got)
		bad = append(bad, name+":\n  API order: …"+a+"…\n  reversed:  …"+r+"…")
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d tools answer differently when the cloud lists things in another order; pick by a rule, not the first of a list, or say why in orderDependent:\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
	for name := range orderDependent {
		if !seen[name] {
			t.Errorf("orderDependent names %s, whose answer no longer depends on the order: remove the line", name)
		}
	}
}
