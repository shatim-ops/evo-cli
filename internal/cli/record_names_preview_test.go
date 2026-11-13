// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The record of a cut call names what its preview named (release plan
// 1.9, process backlog: «цель записи и план зовут один разрешитель»; data
// of «Риск» on #1631 and «разработка C» on #1640). For a tool of
// everySendIsAChange the record is what the question after a cut is about:
// its name and its target are found by one function (paidCreate's name,
// the target of changeRule), and the preview by another — for pg, kafka,
// redis and lb the plan reads the argument name itself. Two ways to the
// same resource drift apart in silence: the question would ask about a
// resource the person was not shown. Every declared tool, in its case of
// the world: the record of the cut call has a name or a target, and the
// preview of the same call, without consent, names both.
//
// What it does not see: a call that names its resource in two spellings
// (a name and an id of the same VM) in a world where both resolve to one —
// the world of parity has one resource of each kind.
func TestCutCall_RecordNamesWhatThePreviewNamed(t *testing.T) {
	declared := everySendDeclared(t) // before the world moves the process
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	base := w.rt.extra
	want := map[string]bool{}
	for _, name := range declared {
		want[name] = true
	}
	ran := map[string]bool{}
	for _, c := range cases {
		if !c.tool.Mutating || !want[c.tool.Name] {
			continue
		}
		args, world, done := copyArgs(c.args), base, func() {}
		if c.setup != nil {
			extra, teardown := c.setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			world, done = w.rt.extra, teardown
		}
		call := func(confirmed bool) string {
			a := copyArgs(args)
			if confirmed {
				a["confirmed"] = true
			}
			fake.reset()
			newProcess()
			tl, _ := newRegistry().Lookup(c.tool.Name)
			text, _ := tl.Handler(context.Background(), a)
			return text
		}
		forgetRun(w)
		preview := call(false)
		w.rt.extra = func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				return nil, errors.New("read: connection reset by peer")
			}
			if world == nil {
				return nil, nil
			}
			return world(r)
		}
		call(true)
		w.rt.extra = world
		var records []intent.Intent
		for _, dir := range intent.Dirs(w.dir) {
			records = append(records, intent.Pending(dir)...)
		}
		forgetRun(w)
		done()
		if len(records) != 1 {
			t.Errorf("%s: the cut call left %d records, want one", c.path, len(records))
			continue
		}
		ran[c.tool.Name] = true
		rec := records[0]
		_, id, _ := strings.Cut(rec.Target, "=")
		if rec.Name == "" && id == "" {
			t.Errorf("%s: the record of the cut call names neither a resource nor a target: the question after it cannot say what it is about", c.path)
		}
		if rec.Name != "" && !strings.Contains(preview, strconv.Quote(rec.Name)) {
			t.Errorf("%s: the record names %q, the preview of the same call does not: %.400s", c.path, rec.Name, preview)
		}
		if id != "" && !strings.Contains(preview, id) {
			t.Errorf("%s: the record's target is %q, the preview of the same call does not name it: %.400s", c.path, rec.Target, preview)
		}
	}
	for _, name := range declared {
		if !ran[name] {
			t.Errorf("%s is declared in everySendIsAChange and no case of the world was cut and read", name)
		}
	}
}
