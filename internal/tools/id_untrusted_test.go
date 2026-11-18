// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The id of a resource in the answer of a write is what the cloud answered
// with, as operation_id is: "_untrusted" names the field whenever it has
// the cloud's text in it («Риск», 357; #2378).
func TestResourceID_MarkedInTheAnswerOfAWrite(t *testing.T) {
	faultToolsIsolate(t)
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	fields := []string{"registry_id", "vpc_id", "zone_id", "record_id", "route_id", "backup_id", "vm_id"}
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
				for _, f := range fields {
					v, has := m[f]
					if !has {
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
