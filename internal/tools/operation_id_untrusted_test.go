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

// operationIDWriters is how many tools the world below brings to an answer
// of a write with the operation_id of the cloud.
const operationIDWriters = 18

// The id of an operation in the answer of a create or a delete is what the
// cloud answered the write with, not evo's: "_untrusted" names
// operation_id whenever the answer has the cloud's text in it («Риск»,
// 353; the read of an operation, #2336).
func TestOperationID_MarkedInTheAnswerOfAWrite(t *testing.T) {
	faultToolsIsolate(t)
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	// Every body of the cloud has the hostile name for its id: the answer
	// to the write that starts an operation, too.
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
			if !tl.Mutating {
				continue
			}
			for _, args := range requiredCalls(tl) {
				args = maps.Clone(args)
				args["confirmed"] = true
				_, text, _ := callNamed(t, r, tl.Name, args)
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil {
					continue
				}
				v, has := m["operation_id"]
				if !has {
					continue
				}
				b, _ := json.Marshal(v)
				if !strings.Contains(string(b), hostileMarker) {
					continue
				}
				seen[tl.Name] = true
				list, _ := m["_untrusted"].([]any)
				if !slices.Contains(list, any("operation_id")) && !bad[tl.Name] {
					bad[tl.Name] = true
					t.Errorf("%s (HTTP %d): operation_id is the cloud's (%q), and \"_untrusted\" %v does not name it", tl.Name, status, v, list)
				}
			}
		}
	}
	t.Logf("tools whose write answers the cloud's operation_id: %d %v; not marked: %d", len(seen), slices.Sorted(maps.Keys(seen)), len(bad))
	// 18 on 18.11 (Risk 353): a world that no longer reaches the writes
	// checks nothing.
	if len(seen) < operationIDWriters {
		t.Errorf("%d tools answer a write with the cloud's operation_id, %d expected: the world no longer reaches them", len(seen), operationIDWriters)
	}
}
