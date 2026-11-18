// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"maps"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// cloudMark ends every string the world of the class gives: a value of the
// cloud — a name, an id, a message, an address — on a line of its own. A
// raw newline before it in an answer is the value as the cloud gave it.
const cloudMark = "CLOUD-VALUE-MARK"

// hostileEverywhere is v with every string leaf ended by a newline and
// cloudMark, but the words of a state (status, state, phase): a state the
// code knows keeps it on its usual path.
func hostileEverywhere(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = hostileEverywhere(e, k)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = hostileEverywhere(e, key)
		}
		return out
	case string:
		switch strings.ToLower(key) {
		case "status", "state", "phase":
			return x
		}
		return x + "\n" + cloudMark
	}
	return v
}

// classLeaks are the fields of the answer m that carry a value of the cloud
// as evo's: a field outside "_untrusted" with cloudMark in it, but evo's
// own text where the value is quoted (no raw newline before the mark).
func classLeaks(m map[string]any) []string {
	named := map[string]bool{}
	if list, ok := m["_untrusted"].([]any); ok {
		for _, f := range list {
			if s, ok := f.(string); ok {
				named[s] = true
			}
		}
	}
	var out []string
	for field, v := range m {
		if field == "_untrusted" || named[field] {
			continue
		}
		has, bare := markIn(v)
		if !has || textFields[field] && !bare {
			// evo's own text holds the value quoted (quoteCloud writes the
			// newline as \n): the rule of #2253.
			continue
		}
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

// markIn reports whether v holds cloudMark at any depth, and whether it
// holds it bare — after a real newline, the value as the cloud gave it.
func markIn(v any) (has, bare bool) {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			h, b := markIn(e)
			has, bare = has || h, bare || b
		}
	case []any:
		for _, e := range x {
			h, b := markIn(e)
			has, bare = has || h, bare || b
		}
	case string:
		return strings.Contains(x, cloudMark), strings.Contains(x, "\n"+cloudMark)
	}
	return has, bare
}

// untrustedClassPending are the places known to carry a value of the cloud
// as evo's, "tool: field" → the finding; each is a line to the owner of
// the tool. Only shrinks: a line no longer found fails the test. Closes by
// the tag 1.9 (#2393).
var untrustedClassPending = map[string]string{
	"ar_delete: next":           "id of the operation in a command, bare (#2253)",
	"dns_record_delete: next":   "id of the operation in a command, bare (#2253)",
	"kafka_delete: next":        "id of the operation in a command, bare (#2253)",
	"lb_delete: next":           "id of the operation in a command, bare (#2253)",
	"pg_delete: next":           "id of the operation in a command, bare (#2253)",
	"redis_delete: next":        "id of the operation in a command, bare (#2253)",
	"tg_delete: next":           "id of the operation in a command, bare (#2253)",
	"vpc_delete: next":          "id of the operation in a command, bare (#2253)",
	"backup_get: id":            "id of the resource, not in _untrusted",
	"k8s_nodepool_get: id":      "id of the resource, not in _untrusted",
	"vm_get: id":                "id of the resource, not in _untrusted",
	"vm_get: not_checked":       "id of a security group in check, not in _untrusted",
	"k8s_nodepools: node_pools": "ids and names of the pools, not in _untrusted",
	"tg_create: targets":        "interface_id of a target, not in _untrusted",
	"tg_create: tg_id":          "id of the group as the cloud gave it, not in _untrusted",
	"cert_import: certificate":  "the certificate the cloud answered, not in _untrusted",
	"cert_renew: operation":     "the operation the cloud answered, not in _untrusted",
	"disk_resize: disk":         "the disk the cloud answered, not in _untrusted",
	"pg_db_create: database":    "the database the cloud answered, not in _untrusted",
	"pg_user_create: user":      "the user the cloud answered, not in _untrusted",
}

// untrustedClassAnswers is how many answers the world brings to the check
// (560 on 18.11): a world that reaches fewer checks less.
const untrustedClassAnswers = 500

// Any value of the cloud in an answer of any tool is either in a field
// "_untrusted" names, or in evo's own text quoted (#2253) — the class, not
// a list of the names of fields («Риск», 362; #2393: operation_id, *_id,
// next, repeat_of.id, user.*, certificate.*, message went past the lists
// one by one). The world gives every string of every body of the cloud —
// ids, names, messages, addresses — a raw newline and cloudMark; every
// tool is called with its required arguments, before and after the
// consent, in a success and in a refusal of the API.
func TestUntrustedClass_CloudValueMarkedOrQuoted(t *testing.T) {
	faultToolsIsolate(t)
	first := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = first })
	var base any
	if err := json.Unmarshal(hostileBody(hostileNested()), &base); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(hostileEverywhere(base, ""))
	found := map[string]bool{}
	answers := 0
	for _, status := range []int{http.StatusOK, http.StatusUnprocessableEntity} {
		api := &oddAPI{status: status, body: body}
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, api, cloud, nil)
		r.SetCallBudget(containerBudget)
		for _, tl := range r.Tools() {
			for _, call := range requiredCalls(tl) {
				for _, confirmed := range []bool{false, true} {
					if confirmed && !tl.Mutating {
						continue
					}
					args := maps.Clone(call)
					if confirmed {
						args["confirmed"] = true
					}
					_, text, _ := callNamed(t, r, tl.Name, args)
					var m map[string]any
					if json.Unmarshal([]byte(text), &m) != nil {
						continue
					}
					answers++
					for _, f := range classLeaks(m) {
						key := tl.Name + ": " + f
						if !found[key] && untrustedClassPending[key] == "" {
							b, _ := json.Marshal(m[f])
							t.Errorf("%s (HTTP %d, confirmed %v): the field %q carries a value of the cloud outside \"_untrusted\" %v — name it in Decl.Untrusted, or quote it in evo's text (quoteCloud)\n%.300s",
								tl.Name, status, confirmed, f, m["_untrusted"], b)
						}
						found[key] = true
					}
				}
			}
		}
	}
	for key := range untrustedClassPending {
		if !found[key] {
			t.Errorf("untrustedClassPending names %s, which carries no unmarked value of the cloud now: remove the line (the list only shrinks)", key)
		}
	}
	if answers < untrustedClassAnswers {
		t.Errorf("%d answers read, %d expected: the world no longer reaches the tools", answers, untrustedClassAnswers)
	}
	t.Logf("answers read: %d; known places: %d", answers, len(untrustedClassPending))
}
