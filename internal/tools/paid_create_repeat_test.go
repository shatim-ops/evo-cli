// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// paidCreateWorld is the world of a create of the fault matrix with its
// create request found: k is the number of that call in a clean run.
func paidCreateWorld(t *testing.T, c faultMatrixCase, lag int) (api *faultAPI, r *Registry, dir string, k int, ctx context.Context) {
	t.Helper()
	happy, _, _, _ := faultRun(t, c, 0, "")
	for i, call := range happy.callLog() {
		if strings.HasPrefix(call, http.MethodPost+" ") || strings.HasPrefix(call, http.MethodPut+" ") {
			k = i + 1
			break
		}
	}
	if k == 0 {
		t.Fatalf("%s sends no create request in a clean run", c.tool)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w, r, _ := c.world(t, cancel)
	api = w.(*faultAPI)
	api.lag = lag
	root, _ := r.stateDir()
	return api, r, intent.Dir(root), k, ctx
}

// creates counts the create requests of the world's log.
func creates(w faultWorld) int {
	n := 0
	for _, call := range w.callLog() {
		if strings.HasPrefix(call, http.MethodPost+" ") || strings.HasPrefix(call, http.MethodPut+" ") {
			n++
		}
	}
	return n
}

// A create billed at the price of a cluster whose answer was lost, while
// the list does not show what it made yet: the repeat with the consent of
// the first call made a second cluster (the fault matrix, #1604; release
// plan 1.9, «Повтор платного создания после потерянного ответа спрашивает
// заново»; D107; «Риск», conclusion 179). The repeat asks instead and
// sends nothing — whatever its other arguments are; a call by another
// name is another call; the consented call after the question is sent.
func TestPaidCreate_TheRepeatAfterALostAnswerAsks(t *testing.T) {
	ran := 0
	for _, c := range faultMatrixCases {
		if !everySendIsAChange[c.tool].made {
			continue
		}
		ran++
		t.Run(c.tool, func(t *testing.T) {
			api, r, dir, k, ctx := paidCreateWorld(t, c, 1000)
			api.failAt(k, faultLost)
			r.handlers[c.tool](ctx, maps.Clone(c.args))
			api.disarm()
			sent := creates(api)
			if left := intent.Pending(dir); len(left) != 1 || !left[0].Ended {
				t.Fatalf("the record of the call whose answer was lost: %+v", left)
			}
			// The same call with one more argument that does not matter.
			other := maps.Clone(c.args)
			tl, _ := r.Lookup(c.tool)
			if props, _ := tl.InputSchema["properties"].(map[string]any); props["description"] != nil {
				other["description"] = "another"
			}
			text, isErr := r.handlers[c.tool](context.Background(), other)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if !isErr || m["status"] != "confirmation_required" || m["interrupted"] == nil || creates(api) != sent {
				t.Fatalf("the repeat after a lost answer: create requests %d (were %d), want the question\n%s", creates(api), sent, text)
			}
			msg, _ := m["message"].(string)
			for _, want := range []string{"may have been accepted", "may not be listed yet", "a second", everySendIsAChange[c.tool].read} {
				if !strings.Contains(msg, want) {
					t.Errorf("the question does not say %q: %s", want, msg)
				}
			}
			if strings.Contains(msg, "lags") || strings.Contains(msg, "delay") {
				t.Errorf("the question states of the platform what no run has shown: %s", msg)
			}
			// The person was asked (MCP): the next consented call is sent.
			r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if creates(api) != sent+1 {
				t.Errorf("the consented call after the question: create requests %d, want %d", creates(api), sent+1)
			}
		})
	}
	if ran < 11 {
		t.Errorf("%d creates billed at the price of a cluster are in the matrix, want the eleven of D107 (the six of #1640 and the five of the owner's decision of 13.11)", ran)
	}
}

// One call of a create that asks after a cut sends one create request
// (Ут3 of «Риск», conclusion 179): the question names one resource that
// may be there; a call that sent two would leave a second unnamed. Every
// declared create, in a clean run of the matrix.
func TestPaidCreate_OneCallOneCreateRequest(t *testing.T) {
	ran := 0
	for _, c := range faultMatrixCases {
		if !everySendIsAChange[c.tool].made {
			continue
		}
		ran++
		t.Run(c.tool, func(t *testing.T) {
			happy, _, _, _ := faultRun(t, c, 0, "")
			if n := creates(happy); n != 1 {
				t.Errorf("a clean call sent %d create requests, want 1:\n%s", n, strings.Join(happy.callLog(), "\n"))
			}
		})
	}
	if ran < 11 {
		t.Errorf("%d declared creates checked, want 11", ran)
	}
}

// The question is asked only while the outcome cannot be read: once the
// list shows what the cut call made, the record closes and the repeat is
// answered as any call on a name that is taken — no question, no second
// request («Риск», conclusion 179, У1).
func TestPaidCreate_WhatIsListedIsNotAskedAbout(t *testing.T) {
	for _, c := range faultMatrixCases {
		if !everySendIsAChange[c.tool].made {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			api, r, dir, k, ctx := paidCreateWorld(t, c, 1000)
			api.failAt(k, faultLost)
			r.handlers[c.tool](ctx, maps.Clone(c.args))
			api.disarm()
			sent := creates(api)
			if len(intent.Pending(dir)) != 1 {
				t.Fatalf("no record of the call whose answer was lost: %+v", intent.Pending(dir))
			}
			// The list shows it now.
			api.mu.Lock()
			api.unlisted = nil
			api.mu.Unlock()
			text, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if strings.Contains(text, `"interrupted"`) || creates(api) != sent {
				t.Errorf("the repeat once the resource is listed: create requests %d (were %d)\n%s", creates(api), sent, text)
			}
			if left := intent.Pending(dir); len(left) != 0 {
				t.Errorf("the record of a call whose resource is listed stays: %+v", left)
			}
		})
	}
}

// A call that sent no create request leaves no record, whatever ended it
// without an outcome — the client gone at a read before the write: the
// repeat is
// not told that «the first request may have been accepted» when none went
// out («Риск», conclusion 179, У2).
func TestPaidCreate_NoRequestSentLeavesNoRecord(t *testing.T) {
	for _, c := range faultMatrixCases {
		if !everySendIsAChange[c.tool].made {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			// The client is gone at a read before the create: the call
			// ends with no outcome, and nothing was sent. The read is the
			// last one before the create whose loss ends the call.
			var api *faultAPI
			var r *Registry
			var dir, text string
			cut := false
			for at := 1; !cut; at++ {
				var k int
				var ctx context.Context
				api, r, dir, k, ctx = paidCreateWorld(t, c, 0)
				if at >= k {
					t.Fatalf("no read before the create request (call %d) ends the call when the client is gone at it", k)
				}
				api.failAt(k-at, faultCancel)
				var isErr bool
				text, isErr = r.handlers[c.tool](ctx, maps.Clone(c.args))
				api.disarm()
				cut = isErr && creates(api) == 0
			}
			if left := intent.Pending(dir); len(left) != 0 {
				t.Errorf("a call that sent nothing left a record: %+v\n%s", left, text)
			}
			again, _ := r.handlers[c.tool](context.Background(), maps.Clone(c.args))
			if strings.Contains(again, `"interrupted"`) || creates(api) != 1 {
				t.Errorf("the repeat of a call that sent nothing: create requests %d, want 1 and no question\n%s", creates(api), again)
			}
		})
	}
}

// Why the repeat of a create is not asked about after a lost answer.
const (
	repeatMeasured = "one of the three of the eight the owner left to 1.10 (release plan 1.9, «Правило повтора платного создания (D107) — ещё на пять созданий»): a known limit of 1.9, a line of the backlog (listLagsPending names it)"
	repeatOutside  = "not a create billed at the price of a cluster (D107): its repeat is held by the check of the name and by the fault matrix"
)

// repeatNotAsked is the closed list of the creates that are not in
// everySendIsAChange, each with the reason. A new create joins one of the
// two by a decision: a create billed at the price of a cluster with no
// declaration of how its outcome is read fails the build («Риск»,
// conclusion 179, У8).
var repeatNotAsked = map[string]string{
	"dns_record_create": repeatMeasured, "snat_create": repeatMeasured, "vpc_route_create": repeatMeasured,
	"ca_deploy": repeatOutside, "cert_issue_le": repeatOutside, "deploy": repeatOutside, "deploy_apply": repeatOutside,
	"disk_create": repeatOutside, "dns_zone_create": repeatOutside, "image_create": repeatOutside, "k8s_add_addon": repeatOutside,
	"pg_db_create": repeatOutside, "pg_user_create": repeatOutside, "provision": repeatOutside, "s3_bucket_create": repeatOutside,
	"s3_put": repeatOutside, "secret_create": repeatOutside, "subnet_create": repeatOutside,
}

func TestPaidCreate_EveryCreateIsDeclaredOrNamedWithAReason(t *testing.T) {
	for tool := range createdIDs {
		_, asked := everySendIsAChange[tool]
		reason := repeatNotAsked[tool]
		switch {
		case !asked && reason == "":
			t.Errorf("%s creates a resource and declares nothing of its repeat after a lost answer: add it to everySendIsAChange (paidCreate, when it is billed at the price of a cluster) or to repeatNotAsked with the reason", tool)
		case asked && reason != "":
			t.Errorf("%s is in everySendIsAChange and in repeatNotAsked: remove the line of the list", tool)
		}
	}
	for tool := range repeatNotAsked {
		if _, ok := createdIDs[tool]; !ok {
			t.Errorf("repeatNotAsked names %s, which creates nothing (createdIDs)", tool)
		}
	}
	// The measured eight are the ones the fault matrix still finds.
	for tool, reason := range repeatNotAsked {
		if _, pending := listLagsPending[tool]; pending != (reason == repeatMeasured) {
			t.Errorf("%s: in listLagsPending %v, its reason here %q — the two lists tell the same eight", tool, pending, reason)
		}
	}
	// A create that asks tells its resource by the name its plan makes it
	// by and reads its own list.
	for tool, rule := range everySendIsAChange {
		if !rule.made {
			continue
		}
		if rule.read != createdIDs[tool].list || rule.read == "" || rule.target == nil {
			t.Errorf("%s: a create that asks reads %q (its list is %q), target set %v", tool, rule.read, createdIDs[tool].list, rule.target != nil)
		}
	}
}

// The cut call of a repeat is looked for in the project the repeat goes
// to: a resource of the same name in another project is another resource
// — no question there, and the record of the first stays for its own
// project. On a server bound to projects without its own (--allow-project),
// a call with project_id is recorded and asked about as any other
// («Риск», the conclusion on D107, Ут4; FB19I-32).
func TestIntent_ACutCallIsOfTheProjectOfTheCall(t *testing.T) {
	const a, b = "00000000-0000-0000-0000-0000000000a2", "00000000-0000-0000-0000-0000000000b2"
	const tool = "disk_create" // stands for a create of the list, with project_id
	everySendIsAChange[tool] = changeRule{read: "disk_list"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	r := ownerRegistry(t, &ownerFake{}, true, a, b) // the profile's own project is not allowed
	if eng, err := r.newEngine(nil); err == nil || eng != nil {
		t.Fatal("the world builds an engine without a project: it is not a server without its own project")
	}
	root, _ := r.stateDir()
	dir := intent.Dir(root)
	sent := 0
	tl, _ := r.Lookup(tool)
	call := func(project, answer string) string {
		text, _ := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
			// The layer asks the handler for the preview of its question
			// without the consent: only a consented call sends.
			if confirmed, _ := args["confirmed"].(bool); !confirmed {
				return `{"status":"error","reason":"USAGE"}`, true
			}
			_ = httpretry.NoteWrite(ctx)
			sent++
			return answer, strings.Contains(answer, `"error"`)
		})(context.Background(), map[string]any{"name": "db", "size_gb": 10.0, "project_id": project, "confirmed": true})
		return text
	}
	call(a, `{"status":"error","reason":"API_UNREACHABLE"}`)
	if left := intent.Pending(dir); len(left) != 1 || left[0].ProjectID != a {
		t.Fatalf("the record of the cut call in project A: %+v", left)
	}
	if text := call(b, `{"status":"created"}`); sent != 2 || strings.Contains(text, `"interrupted"`) {
		t.Errorf("the same name in project B after a cut call in A: sent %d, want 2 and no question\n%s", sent, text)
	}
	if left := intent.Pending(dir); len(left) != 1 || left[0].ProjectID != a {
		t.Errorf("the call in project B touched the record of project A: %+v", left)
	}
	if text := call(a, `{"status":"created"}`); sent != 2 || !strings.Contains(text, `"interrupted"`) {
		t.Errorf("the repeat in project A: sent %d, want 2 and the question\n%s", sent, text)
	}
}

// The question a consented repeat gets after a lost answer carries the
// preview of the create — its price, or that the resource is billable —
// as the question of a call without consent does: the person asked about
// «one more» sees what it costs («Риск», the conclusion on D107, У9). The
// plan is read for it no more than for a call without consent.
func TestPaidCreate_TheQuestionOfAConsentedRepeatCarriesThePreview(t *testing.T) {
	gets := func(w faultWorld) int { return len(w.callLog()) - creates(w) }
	for _, c := range faultMatrixCases {
		if !everySendIsAChange[c.tool].made {
			continue
		}
		t.Run(c.tool, func(t *testing.T) {
			// The repeat of each kind in a world of its own, after the
			// same lost answer.
			repeat := func(consented bool) (map[string]any, int) {
				api, r, _, k, ctx := paidCreateWorld(t, c, 1000)
				api.failAt(k, faultLost)
				r.handlers[c.tool](ctx, maps.Clone(c.args))
				api.disarm()
				args := maps.Clone(c.args)
				if !consented {
					delete(args, "confirmed")
				}
				before := gets(api)
				text, _ := r.handlers[c.tool](context.Background(), args)
				var m map[string]any
				_ = json.Unmarshal([]byte(text), &m)
				if m["status"] != "confirmation_required" || m["interrupted"] == nil || creates(api) != 1 {
					t.Fatalf("the repeat (consented %v) after a lost answer: create requests %d\n%s", consented, creates(api), text)
				}
				return m, gets(api) - before
			}
			with, readsWith := repeat(true)
			without, readsWithout := repeat(false)
			for field, want := range without {
				if field == "interrupted" || field == "message" || field == "pending_operations" || field == "_untrusted" {
					continue
				}
				got, _ := json.Marshal(with[field])
				exp, _ := json.Marshal(want)
				if string(got) != string(exp) {
					t.Errorf("the question of the consented repeat: %s is %s, the question without consent has %s", field, got, exp)
				}
			}
			if len(without) < 4 {
				t.Errorf("the question without consent has no preview to compare: %v", without)
			}
			if readsWith > readsWithout {
				t.Errorf("the consented repeat read the cloud %d times for its question, a call without consent %d", readsWith, readsWithout)
			}
		})
	}
}

// A plan that does not answer a question — a read of it failed — leaves
// the question after a cut call as it is, with no preview: the failure of
// the preview is neither a refusal nor a send.
func TestIntent_TheQuestionStaysWhenThePreviewCannotBeMade(t *testing.T) {
	const tool = "cert_renew"
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	if _, err := intent.Open(intent.Dir(eng.StateDir), intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: eng.Maker()}); err != nil {
		t.Fatal(err)
	}
	sent := 0
	tl, _ := r.Lookup(tool)
	text, isErr := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
		if confirmed, _ := args["confirmed"].(bool); confirmed {
			_ = httpretry.NoteWrite(ctx)
			sent++
			return `{"status":"ok"}`, false
		}
		return `{"status":"error","reason":"API_UNREACHABLE","error":"read the certificate: no route"}`, true
	})(context.Background(), map[string]any{"certificate_id": resMine, "confirmed": true})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if !isErr || sent != 0 || m["status"] != "confirmation_required" || m["interrupted"] == nil || m["reason"] != nil {
		t.Errorf("the preview could not be made: sent %d, want the question about the cut call alone\n%s", sent, text)
	}
}
