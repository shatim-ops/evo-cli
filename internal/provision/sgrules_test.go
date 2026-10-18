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
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "x"}}, "total": 1 << 30})
	})
	defer srv.Close()
	if _, err := a.ListSecurityGroupRules(context.Background(), "sg-1"); err != nil {
		t.Fatal(err)
	}
	if calls > sgRulesMaxPages {
		t.Errorf("%d requests", calls)
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
	}{{http.StatusNotFound, clierrors.CodeVMNotFound}, {http.StatusUnprocessableEntity, clierrors.CodeAPIError}} {
		srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) })
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
