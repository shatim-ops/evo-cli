// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The live DNS API answers a record id of the right form it has no record
// for with 400 «precondition», not 404 (register NET-15), on GET and on
// DELETE alike. dnsRecordVisibility took only 403 and 404 as «not in this
// API»: a private record asked without type met the public API's 400 and
// the call failed there, before the private API was asked (Grok live run
// on 1.3.0, G130-07); a record deleted earlier answered 400 again, not
// already_deleted (wave 2, G13-31). D40: the 400 of a request by id is the
// API's «no such record», as its 404. The UUIDs are made up.

const dnsPubRec = "00000000-0000-4000-8000-0000000000d1"

// dnsNet15Fake is dnsVisFake with the live API's 400 on a record id it does
// not have, in both APIs; the public API holds one record of its own.
// deleteAnswers, when set, is what DELETE of the private record answers.
func dnsNet15Fake(deleteStatus int) *apiFake {
	base := dnsVisFake()
	return &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "dns.api.cloud.ru" {
			return false
		}
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && p == "/v1/publicRecordsSole/"+dnsPubRec:
			_, _ = io.WriteString(w, `{"id":"`+dnsPubRec+`","name":"www","ttl":300}`)
		case strings.HasPrefix(p, "/v1/publicRecordsSole/") || strings.HasPrefix(p, "/v1/privateRecordsSole/") && !strings.HasSuffix(p, "/"+dnsPrivRec):
			http.Error(w, `{"code":9,"message":"precondition failed"}`, http.StatusBadRequest)
		case r.Method == http.MethodDelete && p == "/v1/privateRecordsSole/"+dnsPrivRec && deleteStatus != 0:
			http.Error(w, `{"code":9,"message":"precondition failed"}`, deleteStatus)
		default:
			return base.own(w, r)
		}
		return true
	}}
}

func dnsCalls(f *apiFake) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// A private record without type: the public API's 400 is «not here», the
// private API is asked and answers.
func TestDNS_RecordWithoutTypeBehindPublic400(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"dns_record_get", map[string]any{"record_id": dnsPrivRec}, "GET dns.api.cloud.ru/v1/privateRecordsSole/" + dnsPrivRec},
		{"dns_record_delete", map[string]any{"record_id": dnsPrivRec, "confirmed": true}, "DELETE dns.api.cloud.ru/v1/privateRecordsSole/" + dnsPrivRec},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := dnsNet15Fake(0)
			r := apiFakeRegistry(t, f)
			_, text, isErr := callNamed(t, r, c.tool, c.args)
			if isErr {
				t.Fatalf("%s of a private record behind the public API's 400: %s", c.tool, text)
			}
			if calls := dnsCalls(f); !slices.Contains(calls, c.want) {
				t.Fatalf("no request %q; sent: %v", c.want, calls)
			}
		})
	}
}

// A record neither API has: NOT_FOUND for the read; already_deleted for the
// delete, without type and with it, and no DELETE is sent (D26).
func TestDNS_RecordNeitherAPIHasIs400InBoth(t *testing.T) {
	f := dnsNet15Fake(0)
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "dns_record_get", map[string]any{"record_id": dnsMissing})
	if !isErr || m["reason"] != clierrors.CodeNotFound {
		t.Fatalf("dns_record_get of a record neither API has: want NOT_FOUND, got %s", text)
	}
	for name, args := range map[string]map[string]any{
		"without type": {"record_id": dnsMissing, "confirmed": true},
		"type=private": {"record_id": dnsMissing, "type": "private", "confirmed": true},
		"type=public":  {"record_id": dnsMissing, "type": "public", "confirmed": true},
	} {
		t.Run(name, func(t *testing.T) {
			f := dnsNet15Fake(0)
			r := apiFakeRegistry(t, f)
			m, text, isErr := callNamed(t, r, "dns_record_delete", args)
			if isErr || m["status"] != "already_deleted" {
				t.Fatalf("delete of a record neither API has: want already_deleted, got %s", text)
			}
			for _, k := range dnsCalls(f) {
				if strings.HasPrefix(k, "DELETE ") {
					t.Fatalf("a DELETE was sent for a record that is not there: %s", k)
				}
			}
		})
	}
}

// DELETE itself answers 400: the record is read again. Gone — the delete
// is done (already_deleted); still there — the error as it is, not a
// success: «deleted» means the record is not there (CLAUDE.md §6, axis 3).
func TestDNS_RecordDelete400IsCheckedByRead(t *testing.T) {
	f := dnsNet15Fake(http.StatusBadRequest)
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "dns_record_delete", map[string]any{"record_id": dnsPrivRec, "type": "private", "confirmed": true})
	if !isErr || m["reason"] != clierrors.CodeAPIError || !strings.Contains(text, "dns API 400") {
		t.Fatalf("DELETE 400 while the record is still there: want the API's error, got %s", text)
	}
	if m["status"] == "already_deleted" {
		t.Fatalf("a record the API still has was answered as deleted: %s", text)
	}
}
