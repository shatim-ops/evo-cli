// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The documented create answer is 201 with the rule and its id: deploy
// keeps the id so destroy can delete exactly that rule.
func TestAddSecurityGroupRule_ReturnsCreatedID(t *testing.T) {
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/security-groups/sg-1/rules" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["id"]; ok {
			t.Errorf("create body carries id (additionalProperties:false): %v", body)
		}
		if _, ok := body["state"]; ok {
			t.Errorf("create body carries state: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"rule-7","direction":"ingress","ether_type":"IPv4","ip_protocol":"tcp","port_range":"80:80","state":"creating","security_group":{"id":"sg-1"}}`))
	})
	defer srv.Close()

	got, err := a.AddSecurityGroupRule(context.Background(), "sg-1", "proj", SGRule{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "80:80"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "rule-7" || got.State != "creating" {
		t.Errorf("created = %+v", got)
	}
}

// A success without a readable body is still a created rule, not an error.
func TestAddSecurityGroupRule_EmptyBody(t *testing.T) {
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	defer srv.Close()
	got, err := a.AddSecurityGroupRule(context.Background(), "sg-1", "", SGRule{})
	if err != nil || got == nil || got.ID != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// The rules list is paginated (limit at most 100): every page is read.
func TestListSecurityGroupRules_Pages(t *testing.T) {
	const total = 130
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security-groups/sg-1/rules" {
			t.Errorf("path %s", r.URL.Path)
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 100 {
			t.Errorf("limit %d outside the documented 1..100", limit)
		}
		var items []map[string]any
		for i := offset; i < total && i < offset+limit; i++ {
			items = append(items, map[string]any{"id": fmt.Sprintf("r%d", i), "direction": "ingress", "port_range": "80:80"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": total, "offset": offset, "limit": limit})
	})
	defer srv.Close()

	rules, err := a.ListSecurityGroupRules(context.Background(), "sg-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != total || rules[total-1].ID != fmt.Sprintf("r%d", total-1) {
		t.Fatalf("got %d rules", len(rules))
	}
}

// A server that keeps sending full pages and a growing total does not keep
// the CLI looping.
func TestListSecurityGroupRules_BoundedWalk(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": fmt.Sprintf("x%d", calls)}}, "total": 1 << 30})
	})
	defer srv.Close()
	// The total is never reached: the list is not complete, an error after
	// sgRulesMaxPages requests (break r4, R4-5: it was nil and the rules
	// read so far).
	var ce *clierrors.CLIError
	if rules, err := a.ListSecurityGroupRules(context.Background(), "sg-1"); !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError {
		t.Fatalf("rules = %d, err = %v; want an incomplete-list error", len(rules), err)
	}
	if calls != sgRulesMaxPages {
		t.Errorf("%d requests, want %d", calls, sgRulesMaxPages)
	}
}

// A page of rules already read (offset ignored) is an error, not the end.
func TestListSecurityGroupRules_OffsetIgnoredIsError(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "x"}}, "total": 3})
	})
	defer srv.Close()
	if rules, err := a.ListSecurityGroupRules(context.Background(), "sg-1"); err == nil || calls != 2 {
		t.Fatalf("rules = %d, err = %v, requests = %d", len(rules), err, calls)
	}
}

func TestDeleteSecurityGroupRule(t *testing.T) {
	var method, path string
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()
	if err := a.DeleteSecurityGroupRule(context.Background(), "sg-1", "rule-7"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/api/v1/security-groups/sg-1/rules/rule-7" {
		t.Errorf("request %s %s", method, path)
	}
}

// 404: the rule is already gone, reported as not found (destroy reads that
// as done); 422 (rule in a state that cannot be deleted) is an API error.
func TestDeleteSecurityGroupRule_Errors(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{http.StatusNotFound, clierrors.CodeNotFound}, {http.StatusUnprocessableEntity, clierrors.CodeAPIError}} {
		srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
			if tc.status == http.StatusNotFound {
				computeNotFound(w, r)
				return
			}
			w.WriteHeader(tc.status)
		})
		err := a.DeleteSecurityGroupRule(context.Background(), "sg-1", "rule-7")
		srv.Close()
		var ce *clierrors.CLIError
		if !errors.As(err, &ce) || ce.Code != tc.code {
			t.Errorf("status %d: err = %v", tc.status, err)
		}
	}
}

// IDs from state go into the path as one segment each.
func TestDeleteSecurityGroupRule_EscapesIDs(t *testing.T) {
	var raw string
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()
	_ = a.DeleteSecurityGroupRule(context.Background(), "sg-1", "../../vms/x")
	if raw != "/api/v1/security-groups/sg-1/rules/..%2F..%2Fvms%2Fx" {
		t.Errorf("path %s", raw)
	}
}

func TestSecurityGroupRuleCleanupRequiresCompleteListing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		pages  []string
		valid  bool
	}{
		{"empty", 200, []string{`{"items":[],"total":0}`}, true},
		{"two pages", 200, []string{`{"items":[{"id":"a"}],"total":2}`, `{"items":[{"id":"b"}],"total":2,"offset":1}`}, true},
		{"partial HTTP response", 206, []string{`{"items":[],"total":0}`}, false},
		{"null", 200, []string{`null`}, false},
		{"missing items", 200, []string{`{"total":0}`}, false},
		{"null items", 200, []string{`{"items":null,"total":0}`}, false},
		{"missing total", 200, []string{`{"items":[]}`}, false},
		{"unidentified rule", 200, []string{`{"items":[null],"total":1}`}, false},
		{"padded rule ID", 200, []string{`{"items":[{"id":" a "}],"total":1}`}, false},
		{"extra data", 200, []string{`{"items":[],"total":0}{}`}, false},
		{"shrinking total", 200, []string{`{"items":[{"id":"a"}],"total":2}`, `{"items":[],"total":1}`}, false},
		{"wrong offset", 200, []string{`{"items":[{"id":"a"}],"total":2}`, `{"items":[{"id":"b"}],"total":2,"offset":0}`}, false},
		{"overlap", 200, []string{`{"items":[{"id":"a"}],"total":3}`, `{"items":[{"id":"a"},{"id":"b"}],"total":3}`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			server, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
				if n >= len(tc.pages) {
					t.Errorf("unexpected request %s", r.URL.Path)
					http.Error(w, "extra request", http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.pages[n])
				n++
			})
			defer server.Close()
			items, err := ReadSecurityGroupRulesForCleanup(context.Background(), adapter, "sg-1", "")
			if n != len(tc.pages) || (err == nil) != tc.valid || (err != nil && (items != nil || clierrors.Reason(err) != clierrors.CodeAPIError)) {
				t.Fatalf("read %d pages, items=%v, err=%v", n, items, err)
			}
		})
	}
}

// The old emulator used only message. The Compute group-not-found schema
// supplies code and message. Neither alone proves the project is accessible.
func TestCleanupRuleRead_ProvesMissingParent(t *testing.T) {
	const base = "/api/v1/security-groups"
	const service = `[{"code":"security_group_not_found","message":"Security group with this id is not found"}]`
	const inventory = `{"items":[{"id":"other","project_id":"project"}],"total":1}`
	for _, tc := range []struct {
		name, body, groups    string
		short, wantRead, gone bool
	}{
		{name: "documented service error", body: service, groups: inventory, wantRead: true, gone: true},
		{name: "contradictory service code", body: `{"code":"access_denied","message":"security group not found"}`},
		{name: "wrong resource", body: `[{"code":"security_group_rule_not_found","message":"security group rule with this id is not found"}]`},
		{name: "ambiguous message", body: `{"message":"not found"}`},
		{name: "truncated valid JSON", body: service, short: true},
		{name: "oversized valid JSON", body: service + strings.Repeat(" ", maxErrorBody)},
		{name: "mixed project inventory", body: service, groups: `{"items":[{"id":"other","project_id":"elsewhere"}],"total":1}`, wantRead: true},
		{name: "inventory without total", body: service, groups: `{"items":[]}`, wantRead: true},
		{name: "bare inventory", body: service, groups: `[]`, wantRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := false
			srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case base + "/sg-1/rules":
					if tc.short {
						w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)+20))
					}
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(tc.body))
				case base:
					read = true
					if r.URL.Query().Get("project_id") != "project" || r.URL.Query().Has("name") || r.URL.Query().Has("availability_zone_id") {
						t.Errorf("inventory must be complete and scoped: %s", r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(tc.groups))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			defer srv.Close()
			ctx := Narrowed(t.Context(), base, "name", "other")
			got, err := ReadSecurityGroupRulesForCleanup(ctx, a, "sg-1", "project")
			if read != tc.wantRead || (err == nil) != tc.gone || len(got) != 0 {
				t.Fatalf("inventory read=%v, rules=%v, error=%v", read, got, err)
			}
		})
	}
}
