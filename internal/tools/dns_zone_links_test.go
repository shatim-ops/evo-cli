// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	dnsLinkedVPC = "00000000-0000-4000-8000-0000000000d1"
	dnsDeleteOp  = "00000000-0000-4000-8000-0000000000e1"
)

// dnsLinksFake is one private zone linked to the VPCs of linked, and the
// operation of its delete as op answers it.
func dnsLinksFake(linked, op string) *apiFake {
	return dnsLinksFakeOnOp(linked, op, nil)
}

// dnsLinksFakeOnOp is dnsLinksFake that calls onOp when the operation is
// read.
func dnsLinksFakeOnOp(linked, op string, onOp func()) *apiFake {
	return &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		p := r.URL.Path
		switch {
		case r.URL.Host == "vpc.api.cloud.ru" && p == "/v1/vpcs":
			_, _ = io.WriteString(w, `{"vpcs":[{"id":"`+dnsLinkedVPC+`","name":"Default","default":true}]}`)
		case r.URL.Host != "dns.api.cloud.ru":
			return false
		case r.Method == http.MethodGet && p == "/v1/publicZones":
			_, _ = io.WriteString(w, `{"zones":[]}`)
		case r.Method == http.MethodGet && p == "/v1/privateZones":
			_, _ = io.WriteString(w, `{"zones":[{"id":"`+dnsPrivZone+`","name":"z.test.","domain":"z.test."}]}`)
		case r.Method == http.MethodGet && p == "/v1/privateZones/"+dnsPrivZone:
			_, _ = io.WriteString(w, `{"id":"`+dnsPrivZone+`","domain":"z.test.","state":"ok","linkedVpcIds":[`+linked+`]}`)
		case r.Method == http.MethodDelete && p == "/v1/privateZones/"+dnsPrivZone:
			_, _ = io.WriteString(w, `{"id":"`+dnsDeleteOp+`","done":false}`)
		case r.Method == http.MethodGet && p == "/v1/operations/"+dnsDeleteOp:
			if onOp != nil {
				onOp()
			}
			_, _ = io.WriteString(w, op)
		default:
			http.Error(w, `{"message":"no such path"}`, http.StatusNotFound)
		}
		return true
	}}
}

// D66 (#597, п. 26; cloud-e2e run 151): Cloud.ru does not delete a private
// zone linked to a VPC — the console says to remove the zone's links first —
// yet the API takes the DELETE and the zone stays. dns_zone_delete refuses
// a zone with linkedVpcIds before the question and without a write, naming
// the VPCs and the way; the delete of a zone without links reads its
// operation: done is deleted, an error is a refusal with the platform's
// text, an operation not done in the wait is deleting with its id.
func TestDNSZoneDelete_LinksAndTheOperation(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })

	for _, args := range []map[string]any{{"id": dnsPrivZone}, {"id": dnsPrivZone, "confirmed": true}} {
		f := dnsLinksFake(`"`+dnsLinkedVPC+`"`, `{"id":"`+dnsDeleteOp+`","done":true}`)
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, "dns_zone_delete", args)
		if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, dnsLinkedVPC) || !strings.Contains(text, "Default") {
			t.Errorf("a linked zone (%v): want USAGE naming the VPC before the question: %s", args, text)
		}
		if slices.Contains(f.calls, "DELETE dns.api.cloud.ru/v1/privateZones/"+dnsPrivZone) {
			t.Errorf("a linked zone (%v): the DELETE was sent", args)
		}
	}

	for _, c := range []struct {
		name, op, status, reason, text string
		wait                           int
	}{
		{"done", `{"id":"` + dnsDeleteOp + `","done":true}`, "deleted", "", "", 30},
		{"failed", `{"id":"` + dnsDeleteOp + `","done":true,"error":{"code":9,"message":"zone has links"}}`, "", "API_ERROR", "zone has links", 30},
		{"not done in the wait", `{"id":"` + dnsDeleteOp + `","done":false}`, "deleting", "", dnsDeleteOp, 0},
	} {
		wait := dnsZoneDeleteWait
		dnsZoneDeleteWait = c.wait
		f := dnsLinksFake("", c.op)
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, "dns_zone_delete", map[string]any{"id": dnsPrivZone, "confirmed": true})
		dnsZoneDeleteWait = wait
		switch {
		case c.reason != "" && (!isErr || m["reason"] != c.reason || !strings.Contains(text, c.text)):
			t.Errorf("%s: want %s with %q: %s", c.name, c.reason, c.text, text)
		case c.status != "" && (isErr || m["status"] != c.status || !strings.Contains(text, c.text)):
			t.Errorf("%s: want status %s: %s", c.name, c.status, text)
		}
	}
}

// A wait for the delete the caller cancelled (Ctrl-C, the MCP client's
// cancel) is an error, as the wait of *_operation is (errResult of the
// cancel), not deleting with exit code 0 (FB160-11, Fable audit 1.6).
func TestDNSZoneDelete_CancelledWaitIsAnError(t *testing.T) {
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Hour
	t.Cleanup(func() { opWaitFirstDelay = first })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := dnsLinksFakeOnOp("", `{"id":"`+dnsDeleteOp+`","done":false}`, cancel)
	r := apiFakeRegistry(t, f)
	tl, _ := r.Lookup("dns_zone_delete")
	text, isErr := tl.Handler(ctx, map[string]any{"id": dnsPrivZone, "confirmed": true})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if !isErr || m["status"] != "error" || m["error"] != context.Canceled.Error() {
		t.Errorf("a cancelled wait: want the error of the cancel, got %s", text)
	}
}
