// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inventoryHosts are the hosts of the JSON world whose first label is not
// the name of the service in scripts/api-contract.json.
var inventoryHosts = map[string]string{}

// bodiesNotInInventory are the fields of request bodies known to be sent and
// not listed in scripts/api-contract.json: "METHOD service path: field" →
// why, and what closes it. It only shrinks: a field not here fails the test,
// and so does a line whose field the inventory now has or the code no longer
// sends. Empty: its deadline 1.9 is met, and a new line comes with a
// deadline of its own and its plan line (release plan 1.9, «Инварианты до
// новых изменяющих операций», Н2).
var bodiesNotInInventory = map[string]string{}

// bodiesNotSent are the requests whose body the inventory lists and no
// case of the JSON world sends: "METHOD service path" → why. Their fields
// are compared with nothing here. It only shrinks: a new request with a
// body comes with a case of the world that sends it. Closes by 1.10: the
// bodies of the worlds of internal/login and internal/auth join the check
// (backlog of the process, docs/internal/audits/process-handoff.md).
var bodiesNotSent = map[string]string{
	"POST compute /api/v1/projects/{project_id}/price-calculation":   "the price of a preview: no case with consent sends it as a write of the JSON world",
	"POST dns /v1/privateRecordsSole":                                "no case of the JSON world creates a private record",
	"POST dns /v1/privateZones":                                      "no case of the JSON world creates a private zone",
	"POST iam /api/v1/auth/token":                                    "the exchange of the key for a token: the worlds of internal/auth, not the JSON world",
	"POST iam /api/v1/permissions/bulk-evaluate":                     "evo login checks the permissions (internal/iamadmin): the world of internal/login",
	"POST iam /api/v2/serviceAccounts":                               "evo login makes the service account: the world of internal/login",
	"POST iam /api/v2/serviceAccounts/{serviceAccountId}/accessKeys": "evo login makes the key: the world of internal/login",
	"POST iam /api/v3/permissions":                                   "evo login grants the role: the world of internal/login",
	"POST oidc /auth/system/openid/token":                            "the exchange of a code or a refresh token: the worlds of internal/auth and internal/login",
	"PUT iam /api/v3/permissions/{permissionId}":                     "evo login grants the role: the world of internal/login",
}

// inventoryRequest is a request of scripts/api-contract.json: the segments
// of its path and the fields of its body.
type inventoryRequest struct {
	service, method, path string
	body                  []string
	hasBody               bool
}

func inventoryWrites(t *testing.T) []inventoryRequest {
	t.Helper()
	var contract struct {
		Services []struct {
			Name     string `json:"name"`
			Requests []struct {
				Method string    `json:"method"`
				Path   string    `json:"path"`
				Body   *[]string `json:"body"`
			} `json:"requests"`
		} `json:"services"`
	}
	if err := json.Unmarshal(mustReadRepo(t, "scripts/api-contract.json"), &contract); err != nil {
		t.Fatalf("scripts/api-contract.json: %v", err)
	}
	var out []inventoryRequest
	for _, s := range contract.Services {
		for _, r := range s.Requests {
			if r.Method == "GET" {
				continue
			}
			req := inventoryRequest{service: s.Name, method: r.Method, path: r.Path}
			if r.Body != nil {
				req.body, req.hasBody = *r.Body, true
			}
			out = append(out, req)
		}
	}
	return out
}

// templateName is a {name} of a path of the inventory.
var templateName = regexp.MustCompile(`\\\{[^/}]+\\\}`)

// pathMatches reports whether the path of a request is the template of the
// inventory: a {name} of the template stands for anything within a segment
// ("/v1/certificate/{id}:renew").
func pathMatches(template, path string) bool {
	re := templateName.ReplaceAllString(regexp.QuoteMeta(template), `[^/]+`)
	ok, err := regexp.MatchString("^"+re+"$", path)
	return err == nil && ok
}

// bodyFields are the fields a JSON body sends, as the inventory names
// them: "a.b" for a field of an object, "a[].b" for one of the objects of
// a list. An empty object or list is the field itself.
func bodyFields(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 && prefix != "" {
			out[prefix] = true
		}
		for k, child := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			bodyFields(child, p, out)
		}
	case []any:
		if len(x) == 0 {
			out[prefix] = true
		}
		for _, child := range x {
			bodyFields(child, prefix+"[]", out)
		}
	default:
		out[strings.TrimSuffix(prefix, "[]")] = true
	}
}

// listed reports whether the inventory names field: itself, or an object
// or a list it is inside of (the inventory names a free-form object —
// labels, metadata — once, not by its keys).
func listed(inventory []string, field string) bool {
	for _, f := range inventory {
		if field == f || strings.HasPrefix(field, f+".") || strings.HasPrefix(field, f+"[]") {
			return true
		}
	}
	return false
}

// TestContract_WorldBodiesInTheInventory keeps the fields evo sends in the
// inventory of its requests (owner's decision 11.11, Н2). The check of the
// inventory against the specifications of Cloud.ru (scripts/api-contract-
// check.py, job contract) sees a body only as the inventory lists it: a
// field the code sends and the inventory does not name is checked against
// nothing — not that the platform has it, nor its impact on the price. The
// bodies are those of the JSON world: every command and tool with a case
// of parity, called with consent. A write the world sees and the inventory
// does not have fails too: the paths were compared by the literals of the
// code only (code-uncovered), not by what is sent.
func TestContract_WorldBodiesInTheInventory(t *testing.T) {
	inventory := inventoryWrites(t)
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	found := map[string]string{}
	seen := map[*inventoryRequest]bool{}
	sent := 0
	for _, c := range cases {
		r := runJSONCase(t, c, w, fake)
		for _, write := range append(r.cliWrites, r.toolWrites...) {
			method, rest, _ := strings.Cut(write, " ")
			url, body, _ := strings.Cut(rest, " ")
			url, _, _ = strings.Cut(url, "?")
			host, path, _ := strings.Cut(url, "/")
			path = "/" + path
			if !strings.HasSuffix(host, ".cloud.ru") {
				continue // the cluster's own API, a registry: not Cloud.ru's
			}
			service := inventoryHosts[host]
			if service == "" {
				service, _, _ = strings.Cut(host, ".")
			}
			var parsed any
			if strings.TrimSpace(body) == "" || json.Unmarshal([]byte(body), &parsed) != nil {
				continue // no body, or not JSON (a form, an upload)
			}
			sent++
			var entry *inventoryRequest
			for i := range inventory {
				if e := &inventory[i]; e.service == service && e.method == method && pathMatches(e.path, path) {
					entry = e
					break
				}
			}
			key := method + " " + service + " "
			if entry == nil {
				found[key+path+": the request"] = fmt.Sprintf("%s sends it, and no request of the inventory is %s %s of %s", c.tool.Name, method, path, service)
				continue
			}
			seen[entry] = true
			fields := map[string]bool{}
			bodyFields(parsed, "", fields)
			for f := range fields {
				// A body that is a list (POST vms) sends the fields of
				// its items: the inventory names them without the list.
				f = strings.TrimPrefix(f, "[].")
				if !listed(entry.body, f) {
					found[key+entry.path+": "+f] = fmt.Sprintf("%s sends the field, and the inventory's body does not name it", c.tool.Name)
				}
			}
		}
	}
	if sent == 0 {
		t.Fatal("the JSON world recorded no body: the check sees nothing")
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if bodiesNotInInventory[k] == "" {
			t.Errorf("%s — %s: add it to \"body\" of the request in scripts/api-contract.json (the check against the specification then sees it)", k, found[k])
		}
	}
	for k := range bodiesNotInInventory {
		if found[k] == "" {
			t.Errorf("bodiesNotInInventory names %q, which is in the inventory now or is no longer sent: remove the line", k)
		}
	}
	unseen := map[string]bool{}
	for i := range inventory {
		if e := &inventory[i]; e.hasBody && len(e.body) > 0 && !seen[e] {
			unseen[e.method+" "+e.service+" "+e.path] = true
		}
	}
	for k := range unseen {
		if bodiesNotSent[k] == "" {
			t.Errorf("%s: the inventory lists its body, and no case of the JSON world sends the request — its fields are compared with nothing: give the tool a case of parity whose write goes through the HTTP world (a fake of CloudAPI alone sends no body)", k)
		}
	}
	for k := range bodiesNotSent {
		if !unseen[k] {
			t.Errorf("bodiesNotSent names %q, which the world sends now or the inventory no longer has: remove the line", k)
		}
	}
	t.Logf("bodies compared: %d; requests of the inventory with a body the world does not send: %d", sent, len(unseen))
}
