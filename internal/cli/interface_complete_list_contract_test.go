// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The requested, attached interface is already on page one. A bound read
// can use that evidence only after page two has been read successfully.
func TestContract_InterfaceReadNeedsCompleteProjectList(t *testing.T) {
	w := withParityWorld(t)
	servers := newROServers(t)
	servers.ro.SetProjectBound()
	const path = "/api/v1/interfaces"
	row := fmt.Sprintf(`{"id":%q,"name":"nic-on-web","vm":{"id":%q,"name":"web"},"security_groups":[{"id":%q,"name":"web-group"}],"primary":true}`, parityNIC, parityVM, parityStoppedVM)
	complete := false
	var offsets []string
	cards := 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "compute.api.cloud.ru" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		}
		var body string
		switch r.URL.Path {
		case path:
			if r.URL.Query().Get("project_id") != pvProject {
				t.Fatalf("unexpected project: %s", r.URL)
			}
			offset := r.URL.Query().Get("offset")
			offsets = append(offsets, offset)
			switch offset {
			case "":
				body = `{"items":[` + row + `],"total":2}`
			case "1":
				if !complete {
					return contractRT{status: 403, body: `{"message":"forbidden"}`}.RoundTrip(r)
				}
				body = fmt.Sprintf(`{"items":[{"id":%q,"vm":null}],"total":2}`, parityRunningVM)
			default:
				t.Fatalf("unexpected offset: %s", offset)
			}
		case path + "/" + parityNIC:
			cards++
			body = row
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL)
		}
		return contractRT{status: 200, body: body}.RoundTrip(r)
	}
	checkRead := func() {
		t.Helper()
		if !slices.Contains(offsets, "1") || len(offsets) == 0 || offsets[0] != "" {
			t.Errorf("expected two list pages: %q", offsets)
		}
		offsets = nil
	}
	newProcess()
	code, out, stderr := runCLI(t, "", "--json", "vpc", "interfaces")
	e, ok := errorEnvelope(stderr)
	if code == 0 || !ok || e.Reason != "AUTH_FAILED" || strings.TrimSpace(out) != "" || strings.Contains(stderr, "nic-on-web") {
		t.Errorf("CLI list: exit %d %s%s", code, out, stderr)
	}
	checkRead()
	call := func(name string, args map[string]any) roAnswer {
		t.Helper()
		a, _ := servers.stdio(t, servers.ro, "tools/call", map[string]any{"name": name, "arguments": args})
		return a
	}
	args := map[string]any{"interface_id": parityNIC}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{{"interface_list", map[string]any{}}, {"interface_get", args}} {
		a := call(tc.name, tc.args)
		if !a.isErr || parseObject(a.text)["reason"] != "AUTH_FAILED" || strings.Contains(a.text, "nic-on-web") || cards != 0 {
			t.Errorf("%s: cards=%d, %s %s", tc.name, cards, a.text, a.rpcErr)
		}
		checkRead()
	}
	complete = true
	a := call("interface_get", args)
	checkRead()
	card, _ := parseObject(a.text)["interface"].(map[string]any)
	vm, _ := card["vm"].(map[string]any)
	groups, _ := card["security_groups"].([]any)
	if a.isErr || cards != 1 || vm["id"] != parityVM || card["primary"] != true || len(groups) != 1 {
		t.Fatalf("attached interface: cards=%d, %s %s", cards, a.text, a.rpcErr)
	}
	group, _ := groups[0].(map[string]any)
	if group["id"] != parityStoppedVM {
		t.Errorf("group: %v", group)
	}
}

// Once Compute reports two interfaces, an incomplete second page cannot
// turn a one-row prefix into a successful user-visible inventory.
func TestContract_InterfaceListKeepsReportedTotal(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"null", "null"}, {"empty items without total", `{"items":[]}`}, {"empty array", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := withParityWorld(t)
			var offsets []string
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/interfaces" || r.URL.Query().Get("project_id") != pvProject {
					return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				offset := r.URL.Query().Get("offset")
				offsets = append(offsets, offset)
				body := tc.body
				if offset == "" {
					body = fmt.Sprintf(`{"items":[{"id":%q,"name":"first-row"}],"total":2}`, parityNIC)
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			newProcess()
			code, out, stderr := runCLI(t, "", "--json", "vpc", "interfaces")
			e, ok := errorEnvelope(stderr)
			if !slices.Equal(offsets, []string{"", "1"}) || code != 5 || !ok || e.Reason != "API_ERROR" || strings.TrimSpace(out) != "" {
				t.Errorf("offsets=%v exit=%d stdout=%s stderr=%s", offsets, code, out, stderr)
			}
		})
	}
}
