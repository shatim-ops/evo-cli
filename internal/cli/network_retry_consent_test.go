// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const retryNewID = "00000000-0000-4000-8000-000000000712"
const retryResourceID = "00000000-0000-4000-8000-000000000711"

type networkRetryCase struct {
	tool, host, path, read string
	readArgs               map[string]any
	field, visibleList     string
}

// Both surfaces use the same directory and the same in-memory cloud.
func TestContract_NetworkRetryConsent(t *testing.T) {
	cases := []networkRetryCase{
		{"dns_record_create", "dns.api.cloud.ru", "/v1/publicRecordsSole", "dns_records", map[string]any{"zone_id": parityVM, "type": "public"}, "records", `{"records":[{"id":"` + retryResourceID + `","name":"www","type":"A"}],"total":1}`},
		{"vpc_route_create", "vpc.api.cloud.ru", "/v1/vpcs/" + parityVM + "/routes/static", "vpc_routes", map[string]any{"vpc_id": parityVM}, "routes", `{"routes":[{"id":"` + retryResourceID + `","subnet":"192.0.2.0/24"}],"total":1}`},
		{"snat_create", "compute.api.cloud.ru", "/api/v1/nat-gateways", "snat_list", map[string]any{}, "nat_gateways", `{"items":[{"id":"` + retryResourceID + `","name":"nat-new","availability_zone_id":"` + pvZone + `","status":"ACTIVE"}],"total":1}`},
	}
	for _, tc := range cases {
		for _, first := range []string{"CLI", "stdio"} {
			for _, result := range []string{"still-unknown", "now-visible"} {
				t.Run(tc.tool+"/"+first+"/"+result, func(t *testing.T) { tc.exercise(t, first, result == "now-visible") })
			}
		}
	}
}

func (tc networkRetryCase) exercise(t *testing.T, first string, appeared bool) {
	w, _ := withJSONWorld(t)
	factory := toolEngine
	toolEngine = func(args map[string]any) (*engine.Engine, error) {
		eng, err := factory(args)
		if err == nil {
			eng.Config.ServiceAccountID = "retry-fixture-account"
		}
		return eng, err
	}
	t.Cleanup(func() { toolEngine = factory })
	old := w.rt.extra
	posts, reads := 0, 0
	approved, visible := false, false
	unstableRead := 0
	foreignPage, contradictoryTotal := false, false
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == tc.host && r.URL.Path == tc.path {
			switch r.Method {
			case http.MethodGet:
				reads++
				if visible {
					body := tc.visibleList
					if contradictoryTotal {
						body = strings.Replace(body, `"total":1`, `"total":0`, 1)
					}
					if foreignPage {
						body = strings.Replace(body, `"id":`, `"project_id":"another-project","id":`, 1)
					}
					if posts == 2 || (unstableRead > 0 && reads == unstableRead) {
						body = strings.ReplaceAll(body, retryResourceID, retryNewID)
					}
					return contractRT{status: 200, body: body}.RoundTrip(r)
				}
			case http.MethodPost:
				posts++
				if posts == 1 {
					return nil, io.ErrUnexpectedEOF
				}
				if !approved || posts != 2 {
					t.Fatalf("unexpected POST %d before or beyond fresh approval", posts)
				}
				return contractRT{status: 200, body: `{"id":"` + retryNewID + `","operationId":"` + retryNewID + `","name":"nat-new","status":"CREATING"}`}.RoundTrip(r)
			}
		}
		return old(r)
	}
	args := maps.Clone(parityArgs[tc.tool])
	confirmed := maps.Clone(args)
	confirmed["confirmed"] = true
	params := func(name string, a map[string]any) map[string]any {
		return map[string]any{"name": name, "arguments": a}
	}
	srv := newROServers(t)
	stdioCall := func() roAnswer {
		a, _ := srv.stdio(t, srv.plain, "tools/call", params(tc.tool, confirmed))
		return a
	}
	question := func(a roAnswer) {
		t.Helper()
		m := parseObject(a.text)
		i, _ := m["interrupted"].(map[string]any)
		if !a.isErr || a.rpcErr != "" || m["status"] != "confirmation_required" || i["tool"] != tc.tool || i["outcome"] != "unknown" || i["read_tool"] != tc.read || i["calls"] != float64(1) || posts != 1 {
			t.Fatalf("question after %d POST: %s %s", posts, a.rpcErr, a.text)
		}
	}
	if first == "CLI" {
		code, reason := tc.runCLI(t, args)
		if code == 0 || reason != "API_UNREACHABLE" || posts != 1 {
			t.Fatalf("first CLI: %d %s, POST=%d", code, reason, posts)
		}
		question(stdioCall())
	} else {
		a := stdioCall()
		if !a.isErr || parseObject(a.text)["reason"] != "API_UNREACHABLE" || posts != 1 {
			t.Fatalf("first stdio: %s POST=%d", a.text, posts)
		}
		code, reason := tc.runCLI(t, args)
		if code != 3 || reason != "CONFIRMATION_REQUIRED" || posts != 1 {
			t.Fatalf("CLI question: %d %s POST=%d", code, reason, posts)
		}
	}
	// Preserve unrelated unresolved operations across recovery.
	ownID := tc.addUnrelatedReceipts(t, w.dir)
	saved := retryReceipts(w.dir)
	h := mcp.NewHTTPHandler(srv.plain, mcp.HTTPOptions{})
	t.Cleanup(h.Close)
	call := func(session, name string, a map[string]any) roAnswer {
		result, _ := retryRPC(t, h, session, "tools/call", params(name, a))
		return result
	}
	for range 2 {
		a := call("", tc.tool, confirmed)
		question(a)
		if !strings.Contains(a.text, "Mcp-Session-Id") {
			t.Fatalf("missing recovery guidance: %s", a.text)
		}
	}
	initSession := func() string {
		t.Helper()
		_, s := retryRPC(t, h, "", "initialize", map[string]any{"protocolVersion": "2025-03-26"})
		if s == "" {
			t.Fatal("missing session")
		}
		return s
	}
	one, two := initSession(), initSession()
	if one == two {
		t.Fatal("two initializations returned the same session")
	}
	question(call(one, tc.tool, confirmed))
	question(call(two, tc.tool, confirmed))
	checkList := func(wantVisible bool) {
		t.Helper()
		before := reads
		listed := call(two, tc.read, tc.readArgs)
		rows, ok := parseObject(listed.text)[tc.field].([]any)
		found := false
		wantID := retryResourceID
		if posts == 2 {
			wantID = retryNewID
		}
		for _, item := range rows {
			row, _ := item.(map[string]any)
			if row["id"] == wantID {
				found = true
			}
		}
		if listed.isErr || reads <= before || !ok || found != wantVisible {
			t.Fatalf("reconciliation: reads=%d wantVisible=%v answer=%s", reads-before, wantVisible, listed.text)
		}
	}
	// A stale list cannot distinguish a delayed object from a failed create.
	checkList(false)
	question(call(two, tc.tool, confirmed))
	if records := retryReceipts(w.dir); !reflect.DeepEqual(records, saved) {
		t.Fatalf("receipts changed while the outcome was unknown")
	}
	if !appeared {
		return
	}
	visible = true
	foreignPage = true
	outside := call(two, tc.tool, confirmed)
	if !outside.isErr || parseObject(outside.text)["existing_id"] != nil || posts != 1 || !reflect.DeepEqual(retryReceipts(w.dir), saved) {
		t.Fatalf("foreign project in list: POST=%d answer=%s", posts, outside.text)
	}
	foreignPage = false
	// One matching row contradicts total=0. Neither its ID nor a write is safe.
	contradictoryTotal = true
	invalid := call(two, tc.tool, confirmed)
	if !invalid.isErr || parseObject(invalid.text)["existing_id"] != nil || posts != 1 || !reflect.DeepEqual(retryReceipts(w.dir), saved) {
		t.Fatalf("contradictory total: POST=%d answer=%s", posts, invalid.text)
	}
	contradictoryTotal = false
	checkList(true)
	// A matching name with a different ID on recheck cannot settle the receipt.
	unstableRead = reads + 2
	conflict := call(two, tc.tool, confirmed)
	if !conflict.isErr || parseObject(conflict.text)["existing_id"] != retryResourceID || posts != 1 || !reflect.DeepEqual(retryReceipts(w.dir), saved) {
		t.Fatalf("inconsistent reads: POST=%d answer=%s", posts, conflict.text)
	}
	unstableRead = 0
	{
		already := call(two, tc.tool, confirmed)
		result := parseObject(already.text)
		if !already.isErr || result["existing_id"] != retryResourceID || posts != 1 {
			t.Fatalf("visible create: POST=%d answer=%s", posts, already.text)
		}
		records := retryReceipts(w.dir)
		delete(saved, ownID)
		if !reflect.DeepEqual(records, saved) {
			t.Fatalf("stable match must settle only the original receipt: %+v", records)
		}
	}
	// The first object's creation is now known. The fixture's operator
	// removes it outside evo. A new create requires its own ordinary preview.
	visible = false
	checkList(false)
	preview := call(two, tc.tool, args)
	offered := parseObject(preview.text)
	if offered["status"] != "confirmation_required" || offered["interrupted"] != nil || posts != 1 {
		t.Fatalf("fresh preview after removal: POST=%d %s", posts, preview.text)
	}
	approved = true
	answer := call(two, tc.tool, confirmed)
	result := parseObject(answer.text)
	id := result["operation_id"]
	if tc.tool == "snat_create" {
		gw, _ := result["nat_gateway"].(map[string]any)
		id = gw["id"]
	}
	if answer.isErr || posts != 2 || id != retryNewID {
		t.Fatalf("approved retry: POST=%d answer=%s", posts, answer.text)
	}
	visible = true
	checkList(true)
	records := retryReceipts(w.dir)
	if !reflect.DeepEqual(records, saved) {
		t.Fatalf("after approval: %+v", records)
	}
}

func (tc networkRetryCase) runCLI(t *testing.T, args map[string]any) (int, string) {
	t.Helper()
	newProcess()
	tool, ok := newRegistry().Lookup(tc.tool)
	if !ok {
		t.Fatal(tc.tool)
	}
	c := commandsByTool()[tc.tool]
	line := parityCommandOf(t, c.group, c.cmd, tool, args).tree
	code, _, stderr := runCLI(t, "", append([]string{"--json", "--yes"}, line...)...)
	e, ok := errorEnvelope(stderr)
	if !ok {
		t.Fatalf("CLI exit %d: %s", code, stderr)
	}
	return code, e.Reason
}

func retryRPC(t *testing.T, h http.Handler, session, method string, p map[string]any) (roAnswer, string) {
	t.Helper()
	flagJSON, flagDryRun, flagYes = false, false, false
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", bytes.NewReader(roRequest(method, p)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", out.Code, out.Body.String())
	}
	a, _ := roDecode(t, out.Body.Bytes())
	if a.rpcErr != "" {
		t.Fatal(a.rpcErr)
	}
	return a, out.Header().Get("Mcp-Session-Id")
}

func retryReceipts(root string) map[string]intent.Intent {
	records := map[string]intent.Intent{}
	for _, dir := range intent.Dirs(root) {
		for _, record := range intent.Pending(dir) {
			records[record.ID] = record
		}
	}
	return records
}

func (tc networkRetryCase) addUnrelatedReceipts(t *testing.T, root string) string {
	t.Helper()
	initial := retryReceipts(root)
	if len(initial) != 1 {
		t.Fatalf("want one pending receipt, got %+v", initial)
	}
	var own intent.Intent
	for _, rec := range initial {
		own = rec
	}
	if own.Tool != tc.tool || !own.Sent || !own.Ended || own.Key == nil || own.Key.ServiceAccountID != "retry-fixture-account" {
		t.Fatalf("unexpected initial receipt: %+v", own)
	}

	for _, kind := range []string{"project", "key", "rotated key", "resource"} {
		rec := own
		switch kind {
		case "project":
			rec.ProjectID = retryNewID
		case "key":
			rec.Key = &state.Maker{KeyID: "other-key"}
		case "rotated key":
			rec.Key = &state.Maker{ServiceAccountID: own.Key.ServiceAccountID, KeyID: "previous-key"}
		case "resource":
			parent, _, ok := strings.Cut(own.Target, "=")
			if !ok {
				t.Fatalf("missing parent in receipt: %s", own.Target)
			}
			rec.Target = parent + "=" + retryNewID
			if tc.tool == "snat_create" {
				rec.Name = "other-gateway"
			}
		}
		rec, err := intent.Open(own.Dir, rec)
		if err != nil {
			t.Fatal(err)
		}
		intent.End(rec, history.Outcome{})
	}
	return own.ID
}
