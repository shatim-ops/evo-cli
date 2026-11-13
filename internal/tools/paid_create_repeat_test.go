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
	if ran < 6 {
		t.Errorf("%d creates billed at the price of a cluster are in the matrix, want the six of D107", ran)
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
	repeatMeasured = "one of the eight of the release plan 1.9, «Повтор платного создания…», (3): the measure of its volume decides — in 1.9, or a line of the backlog (listLagsPending names it)"
	repeatOutside  = "not a create billed at the price of a cluster (D107): its repeat is held by the check of the name and by the fault matrix"
)

// repeatNotAsked is the closed list of the creates that are not in
// everySendIsAChange, each with the reason. A new create joins one of the
// two by a decision: a create billed at the price of a cluster with no
// declaration of how its outcome is read fails the build («Риск»,
// conclusion 179, У8).
var repeatNotAsked = map[string]string{
	"ar_create": repeatMeasured, "cert_import": repeatMeasured, "dns_record_create": repeatMeasured, "pg_backup_create": repeatMeasured,
	"snat_create": repeatMeasured, "tg_create": repeatMeasured, "vpc_create": repeatMeasured, "vpc_route_create": repeatMeasured,
	"ca_deploy": repeatOutside, "cert_issue_le": repeatOutside, "deploy": repeatOutside, "deploy_apply": repeatOutside,
	"disk_create": repeatOutside, "dns_zone_create": repeatOutside, "image_create": repeatOutside, "k8s_add_addon": repeatOutside,
	"pg_db_create": repeatOutside, "pg_user_create": repeatOutside, "provision": repeatOutside, "s3_bucket_create": repeatOutside,
	"s3_put": repeatOutside, "secret_create": repeatOutside, "sg_rule_create": repeatOutside, "subnet_create": repeatOutside,
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
	// by, reads its own list, and counts its writes.
	for tool, rule := range everySendIsAChange {
		if !rule.made {
			continue
		}
		if rule.read != createdIDs[tool].list || rule.read == "" || !rule.viaHTTP || rule.target == nil {
			t.Errorf("%s: a create that asks reads %q (its list is %q), viaHTTP %v, target set %v", tool, rule.read, createdIDs[tool].list, rule.viaHTTP, rule.target != nil)
		}
	}
}
