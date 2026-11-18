// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The id of a resource in the answer of a write is what the cloud answered
// with, as operation_id is: "_untrusted" names every "*_id" field of the
// answer whenever it has the cloud's text in it («Риск», 357; #2378).
func TestResourceID_MarkedInTheAnswerOfAWrite(t *testing.T) {
	faultToolsIsolate(t)
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	body := hostileBody(map[string]any{"id": hostileName, "operation_id": hostileName, "operationId": hostileName})
	seen, bad := map[string]bool{}, map[string]bool{}
	for _, status := range []int{http.StatusOK, http.StatusAccepted} {
		api := &oddAPI{status: status, body: body}
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, api, cloud, nil)
		r.SetCallBudget(containerBudget)
		for _, tl := range r.Tools() {
			for _, args := range requiredCalls(tl) {
				args = maps.Clone(args)
				if !tl.Mutating && !NeedsConsent(tl, args) {
					continue
				}
				args["confirmed"] = true
				_, text, _ := callNamed(t, r, tl.Name, args)
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil {
					continue
				}
				list, _ := m["_untrusted"].([]any)
				// cert_private_key passes on the body of the service: every
				// field of it is the cloud's but key_format, evo's.
				if tl.Name == "cert_private_key" {
					for f, v := range m {
						b, _ := json.Marshal(v)
						if f == "_untrusted" || f == "key_format" || !strings.Contains(string(b), hostileMarker) {
							continue
						}
						seen[tl.Name+"."+f] = true
						if !slices.Contains(list, any(f)) && !bad[tl.Name+"."+f] {
							bad[tl.Name+"."+f] = true
							t.Errorf("%s (HTTP %d): %s is the cloud's, and \"_untrusted\" %v does not name it", tl.Name, status, f, list)
						}
					}
				}
				// Any id of the answer, not only those a write has today: a
				// cluster_id a create starts to answer with is checked too.
				for f, v := range m {
					if !strings.HasSuffix(f, "_id") {
						continue
					}
					b, _ := json.Marshal(v)
					if !strings.Contains(string(b), hostileMarker) {
						continue
					}
					key := tl.Name + "." + f
					seen[key] = true
					if !slices.Contains(list, any(f)) && !bad[key] {
						bad[key] = true
						t.Errorf("%s (HTTP %d): %s is the cloud's (%q), and \"_untrusted\" %v does not name it", tl.Name, status, f, v, list)
					}
				}
			}
		}
	}
	t.Logf("write answers with the cloud's resource id: %d %v; not marked: %d", len(seen), slices.Sorted(maps.Keys(seen)), len(bad))
	// 18.11: ten writes answer a resource id of the cloud, and
	// cert_private_key the body of the service; a world that no longer
	// reaches them checks nothing.
	for _, want := range []string{"ar_create.registry_id", "vpc_create.vpc_id", "dns_zone_create.zone_id", "dns_record_create.record_id", "vpc_route_create.route_id", "pg_backup_create.backup_id", "vm_start.vm_id", "vm_stop.vm_id", "vm_reboot.vm_id", "vm_resize.vm_id"} {
		if !seen[want] {
			t.Errorf("%s: the world no longer reaches it", want)
		}
	}
	if !slices.ContainsFunc(slices.Collect(maps.Keys(seen)), func(k string) bool { return strings.HasPrefix(k, "cert_private_key.") }) {
		t.Errorf("cert_private_key: the world no longer reaches its answer")
	}
}

// certAfterIssue is the certificate service around a Let's Encrypt issue:
// no certificate of the name before it, one with the cloud's id after.
type certAfterIssue struct{ issued bool }

func (a *certAfterIssue) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch {
	case req.Method == http.MethodPost:
		a.issued = true
		_, _ = rec.WriteString(`{"id":"00000000-0000-4000-8000-0000000000f2"}`)
	case a.issued:
		b, _ := json.Marshal(map[string]any{"certificates": []any{map[string]any{"id": hostileName, "name": "web"}}})
		_, _ = rec.Write(b)
	default:
		_, _ = rec.WriteString(`{"certificates":[]}`)
	}
	return rec.Result(), nil
}

// cert_issue_le names the certificate by the id the list of the service
// gives it after the issue: the cloud's text, marked (Qwen review of
// #2390).
func TestResourceID_CertificateIDOfAnIssueMarked(t *testing.T) {
	faultToolsIsolate(t)
	r := raRegistry(t, &certAfterIssue{}, nil, nil)
	_, text, _ := callNamed(t, r, "cert_issue_le", map[string]any{"name": "web", "dns_names": []any{"web.example.com"}, "confirmed": true})
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || m["certificate_id"] != hostileName {
		t.Fatalf("the world no longer reaches the certificate_id of an issue: %s", text)
	}
	if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any("certificate_id")) {
		t.Errorf("certificate_id is the cloud's, and \"_untrusted\" %v does not name it: %s", list, text)
	}
}
