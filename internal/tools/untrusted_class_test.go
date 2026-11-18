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
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
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
// holds it bare — anywhere but at the end of a value quoteCloud quoted.
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
		has = strings.Contains(x, cloudMark)
		// Quoted, the value ends `\n` + cloudMark + `"` in evo's text
		// (quoteCloud): any other occurrence is the value bare, whatever
		// stands before it — a newline, a space a mutant put there
		// («Риск», 371).
		return has, strings.Count(x, cloudMark) != strings.Count(x, `\n`+cloudMark+`"`)
	}
	return has, bare
}

// untrustedClassPending are the places known to carry a value of the cloud
// as evo's, "tool: field" ("tool: *" — every field of its answer) → the
// finding; each is a line to the owner of the tool. Only shrinks: a line
// no longer found fails the test. Closes by 1.9, before the tag (#2393).
var untrustedClassPending = map[string]string{
	"k8s_nodepools: node_pools": "ids and names of the pools, not in _untrusted",
	"cert_import: certificate":  "the certificate the cloud answered, not in _untrusted",
	"cert_renew: operation":     "the operation the cloud answered, not in _untrusted",
	"pg_db_create: database":    "the database the cloud answered, not in _untrusted",
	"pg_user_create: user":      "the user the cloud answered, not in _untrusted",
	// Found once the calls reached the cloud (#2434).
	"billing_balance: *":       "the answer of the balance API as it came, no _untrusted (#2434)",
	"cert_issue_le: operation": "the operation the cloud answered, not in _untrusted (#2434)",
	// The repeat of a create whose outcome is not known yet (made/): the
	// id of the cloud the record holds, in message and repeat_of (#2434).
	"ar_create: message":          "the repeat names the id the record holds, bare (#2434)",
	"ar_create: repeat_of":        "the id the record holds, not in _untrusted (#2434)",
	"backup_create: message":      "the repeat names the id the record holds, bare (#2434)",
	"backup_create: repeat_of":    "the id the record holds, not in _untrusted (#2434)",
	"dns_zone_create: message":    "the repeat names the id the record holds, bare (#2434)",
	"dns_zone_create: repeat_of":  "the id the record holds, not in _untrusted (#2434)",
	"pg_backup_create: message":   "the repeat names the id the record holds, bare (#2434)",
	"pg_backup_create: repeat_of": "the id the record holds, not in _untrusted (#2434)",
	"tg_create: message":          "the repeat names the id the record holds, bare (#2434)",
	"tg_create: repeat_of":        "the id the record holds, not in _untrusted (#2434)",
	"vpc_create: message":         "the repeat names the id the record holds, bare (#2434)",
	"vpc_create: repeat_of":       "the id the record holds, not in _untrusted (#2434)",
}

// classWorldArgs are the arguments a tool needs beyond its required ones
// to reach the cloud: a rule needs its source, helm status its release,
// destroy a VM when there is no deployment in the folder (#2434).
var classWorldArgs = map[string]map[string]any{
	"sg_rule_create": {"cidr": "0.0.0.0/0"},
	"k8s_helm":       {"release": "web"},
	"destroy":        {"vm_id": "00000000-0000-4000-8000-0000000000f1"},
}

// classLocal are the tools whose answer is evo's own, of the folder or of
// the registry, with no request to the cloud by design.
var classLocal = map[string]string{
	"agent_skill": "the cards of the groups, from the registry",
	"list":        "the deployments of the local state",
	"stack_plan":  "the plan of a manifest, a job on disk",
}

// classUnreachedPending are the tools the world of the class does not yet
// bring to the cloud: each needs what an earlier call leaves in the folder
// (a deployment, a plan, a job of a stack, a compose file). Only shrinks.
// Closes by 1.9, before the tag (#2434).
var classUnreachedPending = map[string]string{
	"deploy":        "a compose file in the folder",
	"deploy_apply":  "a plan of deploy_plan",
	"logs":          "a deployment of the folder",
	"status":        "a deployment of the folder",
	"stack_apply":   "a job of stack_plan",
	"stack_destroy": "a job of stack_plan",
	"stack_status":  "a job of stack_plan",
}

// countingRT counts the requests of the world.
type countingRT struct {
	http.RoundTripper
	n atomic.Int64
}

func (c *countingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.RoundTripper.RoundTrip(r)
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
	reached := map[string]bool{}
	answers := 0
	// The world a person has set up: a zone and an image, the keys of
	// Object Storage, the billing agreement — without them a tool refuses
	// before the cloud, and the check reads its refusal (#2434).
	t.Setenv("CLOUDRU_AGREEMENT_ID", "00000000-0000-4000-8000-0000000000a1")
	world := func(c *config.Config) {
		c.Region = "ru.AZ-1"
		c.Image = "ubuntu-22.04"
		c.S3 = config.S3Config{Endpoint: "https://s3.example.invalid", AccessKey: "a", SecretKey: "s", Region: "ru-central-1"}
	}
	for _, status := range []int{http.StatusOK, http.StatusUnprocessableEntity} {
		api := &countingRT{RoundTripper: &oddAPI{status: status, body: body}}
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, api, cloud, world)
		r.SetCallBudget(containerBudget)
		for _, tl := range r.Tools() {
			_, consent := schemaProps(tl)["confirmed"]
			for _, call := range requiredCalls(tl) {
				maps.Copy(call, classWorldArgs[tl.Name])
				for _, confirmed := range []bool{false, true} {
					if confirmed && !consent {
						continue
					}
					args := maps.Clone(call)
					if confirmed {
						args["confirmed"] = true
					}
					sent := api.n.Load()
					_, text, _ := callNamed(t, r, tl.Name, args)
					texts := []string{text}
					if confirmed {
						// The repeat answers from the record of the first
						// call (repeat_of, made/): the same check.
						_, again, _ := callNamed(t, r, tl.Name, maps.Clone(args))
						texts = append(texts, again)
					}
					for i, text := range texts {
						// A request, or a value of the cloud a cache of an
						// earlier call answers with: the cloud is reached.
						if api.n.Load() > sent || strings.Contains(text, cloudMark) {
							reached[tl.Name] = true
						}
						var m map[string]any
						if json.Unmarshal([]byte(text), &m) != nil {
							continue
						}
						answers++
						for _, f := range classLeaks(m) {
							key := tl.Name + ": " + f
							if untrustedClassPending[key] == "" && untrustedClassPending[tl.Name+": *"] != "" {
								// The whole answer is a known place.
								key = tl.Name + ": *"
							}
							if !found[key] && untrustedClassPending[key] == "" {
								b, _ := json.Marshal(m[f])
								t.Errorf("%s (HTTP %d, confirmed %v, repeat %v): the field %q carries a value of the cloud outside \"_untrusted\" %v — name it in Decl.Untrusted, or quote it in evo's text (quoteCloud)\n%.300s",
									tl.Name, status, confirmed, i > 0, f, m["_untrusted"], b)
							}
							found[key] = true
						}
					}
				}
			}
		}
	}
	for _, tl := range raRegistry(t, nil, nil, nil).Tools() {
		_, local := classLocal[tl.Name]
		_, pending := classUnreachedPending[tl.Name]
		switch {
		case reached[tl.Name] && (local || pending):
			t.Errorf("%s reaches the cloud now: remove its line of classLocal or classUnreachedPending", tl.Name)
		case !reached[tl.Name] && !local && !pending:
			t.Errorf("%s sent no request to the cloud in any call of the world: the check read its refusals only — give it the arguments it acts on (requiredCalls, classWorldArgs)", tl.Name)
		}
	}
	for name := range classLocal {
		if _, ok := decls[name]; !ok {
			t.Errorf("classLocal names %s, no tool of the registry", name)
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
