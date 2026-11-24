// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestNetworkInterfaceListKeepsFreeAndAttached(t *testing.T) {
	pages := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/api/v1/interfaces" || q.Get("project_id") != "p" || q.Get("limit") != "100" || q.Has("vm_id") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		pages++
		switch q.Get("offset") {
		case "":
			fmt.Fprint(w, `{"items":[{"id":"attached","name":"nic","type":"regular","state":"created","vm":{"id":"vm","name":"host"},"security_groups":[{"id":"sg","name":"group"}],"subnet":{"id":"subnet"},"primary":true}],"total":2}`)
		case "1":
			fmt.Fprint(w, `{"items":[{"id":"free","name":"spare","state":"created","type":"regular","ip_address":"192.0.2.12","vm":null,"security_groups":[],"primary":false}],"total":2}`)
		default:
			t.Errorf("unexpected offset %s", q.Get("offset"))
			http.Error(w, "offset", 400)
		}
	})
	defer srv.Close()
	rows, err := a.ListInterfaces(context.Background(), "p")
	if err != nil || len(rows) != 2 || pages != 2 {
		t.Fatalf("pages=%d rows=%v error=%v", pages, rows, err)
	}
	if rows[0].VM == nil || rows[0].VM.ID != "vm" || len(rows[0].SecurityGroups) != 1 || rows[0].SecurityGroups[0].ID != "sg" || rows[1].VM != nil {
		t.Fatalf("wrong bindings: %+v, %+v", rows[0], rows[1])
	}
	data, err := json.Marshal(rows[1])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "spare" || got["primary"] != false || got["ip_address"] != "192.0.2.12" || got["state"] != "created" {
		t.Fatalf("lost interface fields: %s", data)
	}
}

func TestNetworkInterfaceCardRequiresMatchingID(t *testing.T) {
	for _, tc := range []struct {
		label        string
		status       int
		body, reason string
	}{
		{"ok", 200, `{"id":"nic","name":"spare","vm":null,"security_groups":[],"primary":false}`, ""},
		{"wrong ID", 200, `{"id":"other"}`, clierrors.CodeAPIError},
		{"null", 200, `null`, clierrors.CodeAPIError},
		{"forbidden", 403, `{"message":"access denied"}`, clierrors.CodeAuthFailed},
		{"absent", 404, `{"error_code":"interface_not_found","message":"interface not found"}`, clierrors.CodeNotFound},
	} {
		t.Run(tc.label, func(t *testing.T) {
			srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/interfaces/nic" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			defer srv.Close()
			row, err := a.GetInterface(context.Background(), "nic")
			if tc.reason == "" {
				if err != nil || row == nil || row.ID != "nic" {
					t.Fatalf("card=%v err=%v", row, err)
				}
				return
			}
			if err == nil || row != nil || clierrors.Reason(err) != tc.reason {
				t.Fatalf("card=%v err=%v, want %s", row, err, tc.reason)
			}
		})
	}
}

func TestNetworkInterfaceCardMissingIDSkipsHTTP(t *testing.T) {
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
	})
	defer srv.Close()
	card, err := a.GetInterface(context.Background(), "")
	if card != nil || err == nil || clierrors.Reason(err) != clierrors.CodeUsage {
		t.Fatalf("card=%v err=%v", card, err)
	}
}

func TestNetworkInterfaceDeniedNextPageDiscardsRows(t *testing.T) {
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "" {
			fmt.Fprint(w, `{"items":[{"id":"first"}],"total":2}`)
			return
		}
		http.Error(w, "access denied", http.StatusForbidden)
	})
	defer srv.Close()
	rows, err := a.ListInterfaces(context.Background(), "p")
	if len(rows) != 0 || err == nil || clierrors.Reason(err) != clierrors.CodeAuthFailed {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func TestNetworkInterfaceCompleteList(t *testing.T) {
	const first = `{"items":[{"id":"a"}],"total":2}`
	for _, tc := range []struct {
		name  string
		pages []string
		count int // -1 requires a failure without a partial result.
	}{
		{"null", []string{"null"}, -1},
		{"null tail", []string{first, "null"}, -1},
		{"bare tail", []string{first, "[]"}, -1},
		{"lost total", []string{first, `{"items":[]}`}, -1},
		{"overlap", []string{`{"items":[{"id":"a"},{"id":"b"}],"total":4}`, `{"items":[{"id":"b"},{"id":"c"}],"total":4}`, `{"items":[]}`}, -1},
		{"complete without repeated total", []string{first, `{"items":[{"id":"b"}]}`}, 2},
		{"legacy bare array", []string{`[{"id":"a"}]`}, 1},
		{"legacy empty array", []string{"[]"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := 0
			srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
				if i >= len(tc.pages) {
					t.Errorf("unexpected request %s", r.URL)
					http.Error(w, "unexpected page", 400)
					return
				}
				fmt.Fprint(w, tc.pages[i])
				i++
			})
			defer srv.Close()
			rows, err := a.ListInterfaces(t.Context(), "p")
			if tc.count < 0 {
				if err == nil || clierrors.Reason(err) != clierrors.CodeAPIError || len(rows) != 0 {
					t.Fatalf("incomplete list rows=%v error=%v", rows, err)
				}
			} else if err != nil || len(rows) != tc.count {
				t.Fatalf("rows=%v error=%v", rows, err)
			}
		})
	}
}
