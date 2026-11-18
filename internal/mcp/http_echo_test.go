// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// What a client sends that the server does not know — a tool name, an
// HTTP method, a protocol version — is not written back: not to the
// server's log (stderr), before authentication or after, and not into the
// body of a refusal. The log names a tool of the registry or "unknown", a
// method of HTTP's own or "other", and a refusal names the versions the
// server speaks (Астра #2382, #2385).
func TestHTTP_ClientInputNotEchoed(t *testing.T) {
	const marker = "s3cr3tMarker2382x2385"
	logs := &syncBuffer{}
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	var bodies []string
	read := func(resp *http.Response) {
		b, _ := io.ReadAll(resp.Body)
		bodies = append(bodies, string(b))
	}
	call := func(id string) string {
		return `{"jsonrpc":"2.0",` + id + `"method":"tools/call","params":{"name":"` + marker + `","arguments":{}}}`
	}
	read(post(t, ts, call(`"id":7,`), nil))
	read(post(t, ts, call(""), nil))
	read(post(t, ts, call(`"id":8,`), map[string]string{"Accept": "application/json, text/event-stream"}))
	resp := post(t, ts, `{"jsonrpc":"2.0","id":9,"method":"ping"}`, map[string]string{"Mcp-Protocol-Version": marker})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unsupported version: %d, want 400", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "2025-06-18") {
		t.Errorf("the refusal does not name the versions the server speaks: %s", b)
	}
	bodies = append(bodies, string(b))
	// A method of no HTTP, before the bearer token is checked.
	req, _ := http.NewRequest(marker, ts.URL+"/mcp", nil)
	unauth, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	read(unauth)
	unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", unauth.StatusCode)
	}
	// A known tool is still named: the log is of use to its operator.
	read(post(t, ts, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"vm_list","arguments":{}}}`, nil))

	log := logs.String()
	if strings.Contains(log, marker) {
		t.Errorf("the server's log repeats what the client sent:\n%s", log)
	}
	for _, want := range []string{"tool=vm_list", "tool=unknown", "method=other"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %s:\n%s", want, log)
		}
	}
	for _, body := range bodies[3:5] {
		if strings.Contains(body, marker) {
			t.Errorf("a refusal repeats what the client sent: %s", body)
		}
	}
}
