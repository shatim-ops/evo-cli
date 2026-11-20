// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func interfaceReadRegistry(t *testing.T, serve http.HandlerFunc) *Registry {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") == "" {
			t.Errorf("unexpected request: %s %s, missing token=%t", r.Method, r.URL, r.Header.Get("Authorization") == "")
		}
		serve(w, r)
	}))
	t.Cleanup(server.Close)
	r, _ := fastRegistry(t, provision.NewCloudRuAdapter(server.Client(), server.URL), config.Config{ProjectID: raProject})
	return r
}

func TestNetworkInterfaceReadProjection(t *testing.T) {
	card := fmt.Sprintf(`{"id":%q,"name":"free\ncloud text","vm":null,"security_groups":[],"primary":false}`, faultSG)
	r := interfaceReadRegistry(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/v1/interfaces":
			if req.URL.Query().Get("project_id") != raProject {
				t.Errorf("project query: %v", req.URL)
			}
			fmt.Fprintf(w, `{"items":[%s],"total":1}`, card)
		case "/api/v1/interfaces/" + faultSG:
			fmt.Fprint(w, card)
		default:
			t.Errorf("unexpected URL %v", req.URL)
			w.WriteHeader(404)
		}
	})
	for _, tc := range []struct {
		name string
		call Handler
	}{
		{"interfaces", r.handleInterfaceList},
		{"interface", r.handleInterfaceGet},
	} {
		text, failed := tc.call(t.Context(), map[string]any{"interface_id": faultSG})
		var result map[string]json.RawMessage
		if failed || json.Unmarshal([]byte(text), &result) != nil {
			t.Fatalf("%s: %s", tc.name, text)
		}
		raw := result[tc.name]
		if tc.name == "interfaces" {
			var rows []json.RawMessage
			if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 {
				t.Fatalf("list: %s", text)
			}
			raw = rows[0]
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil || string(row["vm"]) != "null" || string(row["security_groups"]) != "[]" || string(row["primary"]) != "false" || string(row["name"]) != `"free\ncloud text"` {
			t.Fatalf("%s: %s", tc.name, text)
		}
	}
}

func TestNetworkInterfaceProjectEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, listBody, reason string
	}{
		{"absent", `{"items":[],"total":0}`, clierrors.CodeNotFound},
		{"contradictory list", fmt.Sprintf(`{"items":[{"id":%q,"project_id":"foreign-secret"}],"total":1}`, faultSG), clierrors.CodeAPIError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := interfaceReadRegistry(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/api/v1/interfaces" || req.URL.Query().Get("project_id") != raProject {
					t.Errorf("unexpected request %v", req.URL)
				}
				fmt.Fprint(w, tc.listBody)
			})
			err := kindInterface.check(t.Context(), &ownerCall{r: r}, kindInterface, faultSG)
			if err == nil || clierrors.Reason(err) != tc.reason || strings.Contains(err.Error(), "foreign-secret") {
				t.Fatalf("ownership check: %v", err)
			}
		})
	}
}

func TestNetworkInterfaceBoundGetRejectsContradictoryCard(t *testing.T) {
	r := interfaceReadRegistry(t, func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, `{"id":%q,"project_id":"foreign-secret","name":"foreign-data"}`, faultSG)
	})
	r.SetProjectBound()
	text, failed := r.handleInterfaceGet(t.Context(), map[string]any{"interface_id": faultSG})
	var result map[string]any
	if !failed || json.Unmarshal([]byte(text), &result) != nil || result["reason"] != clierrors.CodeNotFound || strings.Contains(text, "foreign-") {
		t.Fatalf("card: %s", text)
	}
}
