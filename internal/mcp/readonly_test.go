// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Q12 (spec.md §8): tools/list of a --read-only server leaves out the tools
// every call of which needs consent and keeps the rest, k8s_kubectl and
// k8s_helm among them; with --toolsets it is the intersection, in either
// order of the two settings.
func TestReadOnly_ToolsList(t *testing.T) {
	s := newTestServer()
	s.SetReadOnly()
	s.SetReadOnly() // a second call changes nothing
	listed := map[string]bool{}
	for _, tl := range s.ListTools() {
		listed[tl.Name] = true
	}
	for _, tl := range s.reg.Tools() {
		if listed[tl.Name] == tools.AlwaysNeedsConsent(tl) {
			t.Errorf("%s: listed %v, every call needs consent %v", tl.Name, listed[tl.Name], tools.AlwaysNeedsConsent(tl))
		}
	}
	for _, name := range []string{"k8s_kubectl", "k8s_helm", "vm_list", "whoami"} {
		if !listed[name] {
			t.Errorf("%s is not listed", name)
		}
	}
	for _, name := range []string{"deploy", "stack_apply", "secret_value", "cert_private_key", "k8s_apply"} {
		if listed[name] {
			t.Errorf("%s is listed", name)
		}
	}

	for _, readOnlyFirst := range []bool{true, false} {
		s := newTestServer()
		if readOnlyFirst {
			s.SetReadOnly()
		}
		if err := s.SetToolsets([]string{"k8s"}); err != nil {
			t.Fatal(err)
		}
		if !readOnlyFirst {
			s.SetReadOnly()
		}
		got := map[string]bool{}
		for _, tl := range s.ListTools() {
			got[tl.Name] = true
			if g := s.groupOf(tl.Name); g != "k8s" {
				t.Errorf("read-only first %v: %s (group %s) is listed", readOnlyFirst, tl.Name, g)
			}
		}
		if !got["k8s_list"] || !got["k8s_kubectl"] || got["k8s_create"] || got["k8s_delete"] {
			t.Errorf("read-only first %v: --toolsets k8s --read-only lists %v", readOnlyFirst, got)
		}
		if err := s.SetToolsets([]string{"all"}); err != nil || len(s.ListTools()) != len(listed) {
			t.Errorf("read-only first %v: --toolsets all lists %d tools, want the %d of --read-only", readOnlyFirst, len(s.ListTools()), len(listed))
		}
	}
}

// A call that needs consent is refused before its handler, confirmed or
// not, with the answer of H11; a tool left out of tools/list answers it too
// (no -32602: the tool exists, the server refuses it). k8s_kubectl and
// k8s_helm run their reads; a command that prints a credential is refused
// as over HTTP without --allow-secret-export (H3, H6), worded for
// --read-only; results are masked (H7).
func TestReadOnly_Calls(t *testing.T) {
	s := newTestServer()
	reached := map[string]int{}
	for _, tl := range s.reg.Tools() {
		name := tl.Name
		s.wrapHandler(name, func(tools.Handler) tools.Handler {
			return func(context.Context, map[string]any) (string, bool) {
				reached[name]++
				return `{"status":"ok","users":[{"name":"app","password":"pw-1"}]}`, false
			}
		})
	}
	s.SetReadOnly()

	refused := 0
	for _, tl := range s.reg.Tools() {
		if !tools.AlwaysNeedsConsent(tl) {
			continue
		}
		for _, args := range []map[string]any{{}, {"confirmed": true}} {
			text, isErr := call(s, tl.Name, args)
			if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, "this server runs with --read-only") {
				t.Errorf("%s %v: %s", tl.Name, args, text)
			}
		}
		if reached[tl.Name] > 0 {
			t.Errorf("%s: the handler ran", tl.Name)
		}
		refused++
	}
	if refused == 0 {
		t.Fatal("no tool needs consent on every call")
	}

	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"deploy","arguments":{"confirmed":true}}}`)
	b, _ := json.Marshal(resp)
	if resp["error"] != nil || !strings.Contains(string(b), "this server runs with --read-only") || !strings.Contains(string(b), `"isError":true`) {
		t.Errorf("tools/call of deploy: %s", b)
	}

	for _, c := range []struct {
		tool, args string
		want       string // "" runs; else a part of the refusal
	}{
		{"k8s_kubectl", `{"cluster_id":"c","args":"get pods -A"}`, ""},
		{"k8s_kubectl", `{"cluster_id":"c","args":"rollout status deploy/web"}`, ""},
		{"k8s_kubectl", `{"cluster_id":"c","args":"delete pod web","confirmed":true}`, "this server runs with --read-only"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"rollout restart deploy/web"}`, "this server runs with --read-only"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"get pods -f /etc/passwd"}`, "this server runs with --read-only"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"config view --raw"}`, "k8s_kubectl config is disabled on this --read-only server"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"get secret db -o jsonpath={.data}"}`, "is disabled on this --read-only server"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"get --raw /api/v1/namespaces/default/secrets"}`, "k8s_kubectl on this --read-only server: --raw is disabled"},
		{"k8s_kubectl", `{"cluster_id":"c","args":"get secret db -v=8"}`, "at higher verbosity"},
		{"k8s_helm", `{"cluster_id":"c","action":"status","release":"web"}`, ""},
		{"k8s_helm", `{"cluster_id":"c","action":"uninstall","release":"web","confirmed":true}`, "this server runs with --read-only"},
		{"k8s_kubeconfig", `{"cluster_id":"c"}`, "k8s_kubeconfig with an embedded token is disabled on this --read-only server"},
		{"k8s_kubeconfig", `{"cluster_id":"c","embed_token":false}`, ""},
	} {
		var args map[string]any
		if err := json.Unmarshal([]byte(c.args), &args); err != nil {
			t.Fatal(err)
		}
		before := reached[c.tool]
		text, isErr := call(s, c.tool, args)
		ran := reached[c.tool] > before
		switch {
		case c.want == "" && (!ran || isErr):
			t.Errorf("%s %s: did not run: %s", c.tool, c.args, text)
		case c.want != "" && (ran || !isErr || !strings.Contains(text, c.want)):
			t.Errorf("%s %s: ran %v, want the refusal %q: %s", c.tool, c.args, ran, c.want, text)
		}
	}

	// H7: credentials are masked over stdio too, with the mask of
	// --read-only.
	text, _ := call(s, "pg_users", map[string]any{})
	if strings.Contains(text, "pw-1") || !strings.Contains(text, readOnlyWording.mask) || len(redactedOf(t, text)) == 0 {
		t.Errorf("pg_users is not masked: %s", text)
	}
}

// The refusals of the secret restrictions name what lifts them: over HTTP
// --allow-secret-export, as before --read-only existed (byte for byte), on a
// --read-only server its operator.
func TestReadOnly_Wording(t *testing.T) {
	fields := strings.Fields("get secret db -o yaml")
	if err := kubectlCheck(fields, false, httpWording); err == nil || err.Error() !=
		"k8s_kubectl get secret with -o yaml is disabled over HTTP: it prints the secret data; start the server with --allow-secret-export to enable it" {
		t.Errorf("over HTTP: %v", err)
	}
	if err := kubectlCheck(fields, false, readOnlyWording); err == nil || err.Error() !=
		"k8s_kubectl get secret with -o yaml is disabled on this --read-only server: it prints the secret data; ask the server's operator to restart it without --read-only to enable it" {
		t.Errorf("read-only: %v", err)
	}
	if httpWording.mask != "[redacted over HTTP: start the server with --allow-secret-export]" {
		t.Errorf("the mask over HTTP changed: %s", httpWording.mask)
	}
	s := newTestServer()
	s.restrictForHTTP(false)
	if text, _ := call(s, "k8s_kubectl", map[string]any{"cluster_id": "c", "args": "config view"}); !strings.Contains(text,
		"k8s_kubectl config is disabled over HTTP: the kubeconfig holds the cluster token. Start the server with --allow-secret-export to enable it") {
		t.Errorf("H6 over HTTP: %s", text)
	}
}
