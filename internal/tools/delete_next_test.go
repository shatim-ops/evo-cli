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

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// deleteNextTools are the deletes whose answer, while the delete runs,
// names in "next" the *_operation call that shows its end (#2392).
const deleteNextTools = 9

// "next" is evo's text, outside "_untrusted": the operation_id the cloud
// gave the delete goes into it only in the form of one word; any other
// value stays in next_args, a field the marker names (#2392; «Риск», 362).
// A plain id is still written out, so an agent calls on with no lookup.
func TestDeleteNext_CloudOperationIDOnlyAsOneWord(t *testing.T) {
	faultToolsIsolate(t)
	const plain = "op-0123456789abcdef"
	for _, c := range []struct {
		opID   string
		inLine bool
	}{{hostileName, false}, {plain, true}} {
		body := hostileBody(map[string]any{"id": c.opID, "operation_id": c.opID, "operationId": c.opID})
		api := &oddAPI{status: http.StatusOK, body: body}
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, api, cloud, nil)
		r.SetCallBudget(containerBudget)
		seen := map[string]bool{}
		for _, tl := range r.Tools() {
			if !tl.Mutating {
				continue
			}
			for _, args := range requiredCalls(tl) {
				args = maps.Clone(args)
				args["confirmed"] = true
				args["wait"] = false
				_, text, _ := callNamed(t, r, tl.Name, args)
				var m map[string]any
				if json.Unmarshal([]byte(text), &m) != nil || m["status"] != "deleting" {
					continue
				}
				next, _ := m["next"].(string)
				nextArgs, _ := m["next_args"].(map[string]any)
				if next == "" || nextArgs["operation_id"] != c.opID {
					continue
				}
				seen[tl.Name] = true
				if strings.ContainsAny(next, "\n\r") || strings.Contains(next, hostileMarker) {
					t.Errorf("%s: \"next\" is evo's text and carries the cloud's operation_id: %q", tl.Name, next)
				}
				if c.inLine != strings.Contains(next, "operation_id="+c.opID) {
					t.Errorf("%s: operation_id %q written out in \"next\": %v, want %v: %q", tl.Name, c.opID, !c.inLine, c.inLine, next)
				}
				if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any("next_args")) {
					t.Errorf("%s: next_args holds the cloud's operation_id, \"_untrusted\" %v does not name it", tl.Name, list)
				}
			}
		}
		if len(seen) < deleteNextTools {
			t.Errorf("operation_id %q: %d deletes answer deleting with next %v, %d expected: the world no longer reaches them", c.opID, len(seen), slices.Sorted(maps.Keys(seen)), deleteNextTools)
		}
	}
}
