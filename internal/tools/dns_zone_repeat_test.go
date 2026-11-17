// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// dnsRepeatAPI is dnsVPCAPI whose zone create answers an operation that
// names the zone and goes on; the zone is listed, in the list of its
// visibility, once listed is set.
type dnsRepeatAPI struct {
	*dnsVPCAPI
	mu     sync.Mutex
	posts  int
	listed string // the visibility whose list shows the zone; "" — none
	opGone bool   // the read of the operation answers 404
}

const dnsRepeatZone = "00000000-0000-4000-8000-0000000000f1"

func (a *dnsRepeatAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "Zones") && r.Method == http.MethodPost:
		a.posts++
		_, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(rec, `{"id":"op-1","resourceId":%q,"done":false}`, dnsRepeatZone)
	case strings.HasSuffix(r.URL.Path, "Zones"):
		vis := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/"), "Zones")
		if vis == a.listed {
			_, _ = fmt.Fprintf(rec, `{"zones":[{"id":%q,"name":"evo-z","domain":"corp.example."}],"total":1}`, dnsRepeatZone)
		} else {
			_, _ = io.WriteString(rec, `{"zones":[],"total":0}`)
		}
	case strings.HasPrefix(r.URL.Path, "/v1/operations/") && a.opGone:
		rec.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rec, `{"message":"operation not found"}`)
	case strings.HasPrefix(r.URL.Path, "/v1/operations/"):
		_, _ = fmt.Fprintf(rec, `{"id":"op-1","resourceId":%q,"done":false}`, dnsRepeatZone)
	default:
		a.mu.Unlock()
		defer a.mu.Lock()
		return a.dnsVPCAPI.RoundTrip(r)
	}
	return rec.Result(), nil
}

// The line of the release plan #2145: dns_zone_create's answer names its
// next step — the read of its operation (dns_operation, as the deletes of
// #2025) — and a repeat of the same call, public zone or private, reads
// that operation and sends no second create, with consent or without. Once
// the zone is listed — dns_zones reads both visibilities — the repeat is a
// call as any other: the name is taken.
func TestDNSZoneCreate_RepeatReadsItsOperationInBothVisibilities(t *testing.T) {
	for _, vis := range []string{"public", "private"} {
		t.Run(vis, func(t *testing.T) {
			api := &dnsRepeatAPI{dnsVPCAPI: &dnsVPCAPI{subnetAPI: newSubnetAPI()}}
			r := waitingRegistry(t, api)
			args := map[string]any{"name": "evo-z", "domain": "corp.example.", "type": vis, "confirmed": true}
			m, text, _ := callNamed(t, r, "dns_zone_create", args)
			na, _ := m["next_args"].(map[string]any)
			if api.posts != 1 || m["next_tool"] != "dns_operation" || na["operation_id"] != "op-1" {
				t.Fatalf("the create: %d POSTs, want 1, and next_tool dns_operation with operation_id op-1: %s", api.posts, text)
			}
			for _, confirmed := range []bool{false, true} {
				args["confirmed"] = confirmed
				m, text, _ = callNamed(t, r, "dns_zone_create", args)
				rep, _ := m["repeat_of"].(map[string]any)
				if api.posts != 1 || rep["id"] != dnsRepeatZone || m["next_tool"] != "dns_operation" {
					t.Errorf("confirmed=%v: %d POSTs, want 1, and the read of the operation: %s", confirmed, api.posts, text)
				}
			}
			api.listed = vis
			args["confirmed"] = true
			_, text, _ = callNamed(t, r, "dns_zone_create", args)
			if api.posts != 1 || strings.Contains(text, "repeat_of") {
				t.Errorf("the zone listed: %d POSTs, want 1 and no repeat_of: %s", api.posts, text)
			}
		})
	}
}

// «Риск», conclusion 310, У1: the read of the operation answers 404 — an
// operation the platform does not show yet, not a zone gone. Within
// madeListGrace a repeat with consent sends no second create, and answers
// waiting, not the read's NOT_FOUND, which would read as the zone gone.
func TestDNSZoneCreate_RepeatWhileItsOperationIsNotFoundSendsNoSecondCreate(t *testing.T) {
	api := &dnsRepeatAPI{dnsVPCAPI: &dnsVPCAPI{subnetAPI: newSubnetAPI()}, opGone: true}
	r := waitingRegistry(t, api)
	args := map[string]any{"name": "evo-z", "domain": "corp.example.", "confirmed": true}
	if _, text, _ := callNamed(t, r, "dns_zone_create", args); api.posts != 1 {
		t.Fatalf("the create: %d POSTs: %s", api.posts, text)
	}
	m, text, isErr := callNamed(t, r, "dns_zone_create", args)
	rep, _ := m["repeat_of"].(map[string]any)
	if api.posts != 1 || rep["id"] != dnsRepeatZone || isErr || m["status"] != "waiting" || m["reason"] != nil || m["next_tool"] != "dns_operation" {
		t.Errorf("the repeat with consent, the operation not found: %d POSTs, want 1, waiting with repeat_of and the read as the next step: %s", api.posts, text)
	}
}
