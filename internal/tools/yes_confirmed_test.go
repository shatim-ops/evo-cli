// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
)

// yesDiffers are the calls whose preview answers finally while the same
// call with confirmed=true answers otherwise — "tool: world" → the work
// item that fixes it. The list only shrinks: a call not here fails the
// test, and so does a line whose call answers alike now.
var yesDiffers = map[string]string{
	// The preview of a destroyed job is already_destroyed; confirmed=true
	// destroys it again and answers destroyed (found by this test, 1.4.0).
	"stack_destroy: destroyed job": "«1.4 — разработка»: confirmed=true on a destroyed job answers already_destroyed (D26)",
}

// finalPreviewKinds are the answers of the contract world a preview can
// take as final: the cloud's success, refusals and failures.
var finalPreviewKinds = []string{"200", "400", "403", "404", "409", "422", "429", "500", "502", "html", "empty"}

// yesDiff compares a call's preview with the same call confirmed: "" when
// the preview asks (the CLI then makes the confirmed call itself) or both
// answer alike, else what differs.
func yesDiff(preview, confirmed func() (string, bool)) string {
	ptext, perr := preview()
	pr, ps := contractReason(ptext)
	if ps == "confirmation_required" {
		return ""
	}
	ctext, cerr := confirmed()
	cr, cs := contractReason(ctext)
	if perr == cerr && pr == cr && ps == cs {
		return ""
	}
	return fmt.Sprintf("preview isError=%v status %q reason %q, confirmed isError=%v status %q reason %q", perr, ps, pr, cerr, cs, cr)
}

// The CLI's --yes calls a tool without confirmed first and makes the
// confirmed call only when that answer is confirmation_required
// (internal/cli/tools.go, invokeTool); MCP with confirmed=true makes the
// confirmed call at once. A preview that answers finally — already gone,
// a refusal, an error — is the CLI's answer, so the confirmed call must
// answer the same (isError, status, reason), or the two surfaces answer
// one call differently (CLAUDE.md §1). Escaped: cloud-e2e 1.3.1, live
// stack — the preview of a destroyed job answered already_destroyed while
// deletions were still in progress, and only MCP re-checked them (#396);
// the parity tests compared a preview with a preview. Every gated tool in
// every answer of the contract world, the deletes of D26 on what is not
// there and on ERASED, and the stack jobs in each of their states.
func TestParity_YesEqualsConfirmed(t *testing.T) {
	found := map[string]string{}
	gated := func(tl Tool) bool {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		_, ok := props["confirmed"]
		return ok
	}

	rt := &contractRT{}
	reg := contractReg(t, rt)
	for _, tl := range reg.Tools() {
		if !gated(tl) || contractHTTPSkip[tl.Name] {
			continue
		}
		for _, kind := range finalPreviewKinds {
			rt.kind, rt.body = kind, ""
			args := contractArgs(tl)
			call := func(confirmed bool) func() (string, bool) {
				return func() (string, bool) {
					a := copyArgsMap(args)
					delete(a, "confirmed")
					if confirmed {
						a["confirmed"] = true
					}
					text, isErr, _ := contractCall(tl, a, contractCallLimit)
					rt.take()
					return text, isErr
				}
			}
			if d := yesDiff(call(false), call(true)); d != "" {
				found[tl.Name+": cloud "+kind] = d
			}
		}
	}

	gone := goneRegistry(t)
	for name, d := range deleteTargets {
		tl, _ := gone.Lookup(name)
		args := goneArgs(name, d)
		if diff := yesDiff(handlerCall(tl, args, false), handlerCall(tl, args, true)); diff != "" {
			found[name+": not there"] = diff
		}
	}
	for _, name := range erasedDeletes {
		var writes atomic.Int32
		tl, _ := erasedRegistry(t, erasedWorld(&writes)).Lookup(name)
		args := map[string]any{deleteTargets[name].arg: liveID}
		if diff := yesDiff(handlerCall(tl, args, false), handlerCall(tl, args, true)); diff != "" {
			found[name+": ERASED"] = diff
		}
	}

	stackHome(t)
	for _, state := range []string{"planned job", "destroyed job", "destroyed job, still deleting", "absent job"} {
		for _, name := range []string{"stack_apply", "stack_destroy"} {
			r := newTestServer()
			r.handlers["secret_create"] = fakeOK(map[string]any{"status": "created", "id": "s1"})
			r.handlers["secret_delete"] = fakeOK(map[string]any{"status": "already_deleted"})
			if state == "destroyed job, still deleting" {
				// The cloud is still finishing the deletion: the incident
				// of #396.
				r.handlers["secret_delete"] = fakeOK(map[string]any{"status": "deleting"})
			}
			id := "stk-0123456789"
			if state != "absent job" {
				j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
				if state != "planned job" {
					// The secret was created: there is something to delete.
					j.Steps[0].Status = "done"
					j.Outputs[j.Steps[0].Key] = "ok"
					if err := j.Save(); err != nil {
						t.Fatal(err)
					}
				}
				id = j.ID
			}
			if state == "destroyed job" || state == "destroyed job, still deleting" {
				if _, isErr := r.handleStackDestroy(context.Background(), map[string]any{"job_id": id, "confirmed": true}); isErr {
					t.Fatalf("destroy of the planned job failed")
				}
			}
			h := r.handlers[name]
			call := func(confirmed bool) func() (string, bool) {
				return func() (string, bool) {
					return h(context.Background(), map[string]any{"job_id": id, "confirmed": confirmed})
				}
			}
			// Each comparison starts from the state: a confirmed call
			// changes the job, so the preview is read first, as the CLI
			// does, and the state is made anew for the next tool.
			if diff := yesDiff(call(false), call(true)); diff != "" {
				found[name+": "+state] = diff
			}
		}
	}

	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if yesDiffers[k] == "" {
			t.Errorf("%s: %s — the CLI's --yes stops at the preview's answer, MCP's confirmed=true gives the other", k, found[k])
		}
	}
	for k := range yesDiffers {
		if found[k] == "" {
			t.Errorf("yesDiffers names %s, which answers alike now: remove the line", k)
		}
	}
}

// handlerCall calls tl with args, with confirmed=true or without it.
func handlerCall(tl Tool, args map[string]any, confirmed bool) func() (string, bool) {
	return func() (string, bool) {
		a := copyArgsMap(args)
		delete(a, "confirmed")
		if confirmed {
			a["confirmed"] = true
		}
		return tl.Handler(context.Background(), a)
	}
}

func copyArgsMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
