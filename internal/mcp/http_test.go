// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "test-token-0123456789"

// syncBuffer is a bytes.Buffer safe for the concurrent writes of a logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newHTTPTest(t *testing.T, opts HTTPOptions) (*httptest.Server, *HTTPHandler) {
	t.Helper()
	h := NewHTTPHandler(newTestServer(), opts)
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})
	return ts, h
}

// post sends body to /mcp with the bearer token and extra headers.
func post(t *testing.T, ts *httptest.Server, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}`

func TestHTTP_InitializeNegotiatesVersionAndOpensSession(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})

	for _, c := range []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		{"2099-01-01", "2025-06-18"}, // unsupported → newest supported
	} {
		body := strings.Replace(initBody, "2025-06-18", c.asked, 1)
		resp := post(t, ts, body, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", c.asked, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: Content-Type %q", c.asked, ct)
		}
		if resp.Header.Get("Mcp-Session-Id") == "" {
			t.Errorf("%s: no Mcp-Session-Id", c.asked)
		}
		var r struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		decodeBody(t, resp, &r)
		if r.Result.ProtocolVersion != c.want {
			t.Errorf("asked %s: got %s, want %s", c.asked, r.Result.ProtocolVersion, c.want)
		}
	}
}

func TestHTTP_ToolsListMatchesStdio(t *testing.T) {
	ts, h := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	var r struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	decodeBody(t, resp, &r)
	if want := len(h.srv.ListTools()); len(r.Result.Tools) != want || want == 0 {
		t.Fatalf("tools/list over HTTP: %d tools, want %d", len(r.Result.Tools), want)
	}
}

func TestHTTP_Unauthorized(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	for name, auth := range map[string]string{
		"no header":   "",
		"wrong token": "Bearer nope",
		"basic":       "Basic " + testToken,
		"no scheme":   testToken,
		"bearerless":  "Bearer" + testToken,
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(initBody))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: WWW-Authenticate %q", name, resp.Header.Get("WWW-Authenticate"))
		}
	}

	// Релизный аудит, R-08: схема в Authorization — без учёта регистра
	// (RFC 9110 §11.1), токен сравнивается как прежде.
	for _, auth := range []string{"bearer " + testToken, "BEARER " + testToken, "Bearer  " + testToken} {
		resp := post(t, ts, initBody, map[string]string{"Authorization": auth})
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%q: status %d, want 200", strings.Fields(auth)[0], resp.StatusCode)
		}
	}

	// The liveness probe needs no token.
	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz: status %d", resp.StatusCode)
	}
}

func TestHTTP_NoAuthMode(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{})
	resp, err := ts.Client().Post(ts.URL+"/mcp", "application/json", strings.NewReader(initBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTP_ToolsCallJSONAndSSE(t *testing.T) {
	logs := &syncBuffer{}
	ts, _ := newHTTPTest(t, HTTPOptions{
		BearerToken: testToken,
		Logger:      slog.New(slog.NewTextHandler(logs, nil)),
		KeepAlive:   time.Millisecond,
	})
	// A gated mutation answers with a preview without touching the cloud.
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"ca_start","arguments":{"name":"app-sentinel-arg"}}}`

	resp := post(t, ts, call, nil)
	var r struct {
		ID     int `json:"id"`
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	decodeBody(t, resp, &r)
	if r.ID != 7 || len(r.Result.Content) == 0 || !strings.Contains(r.Result.Content[0].Text, "confirmation_required") {
		t.Fatalf("JSON tools/call: %+v", r)
	}

	resp = post(t, ts, call, map[string]string{"Accept": "application/json, text/event-stream"})
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("SSE tools/call: Content-Type %q", ct)
	}
	data := readSSEMessage(t, resp.Body)
	r.ID = 0
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("SSE data %q: %v", data, err)
	}
	if r.ID != 7 || !strings.Contains(r.Result.Content[0].Text, "confirmation_required") {
		t.Fatalf("SSE tools/call: %+v", r)
	}

	// One line per call: tool name, status, duration; never the arguments.
	out := logs.String()
	if strings.Count(out, "tool=ca_start") != 2 || !strings.Contains(out, "status=error") || !strings.Contains(out, "duration_ms=") {
		t.Errorf("tool call log: %s", out)
	}
	if strings.Contains(out, "app-sentinel-arg") {
		t.Errorf("arguments leaked into the log: %s", out)
	}
}

// readSSEMessage returns the data of the first "message" event.
func readSSEMessage(t *testing.T, r io.Reader) string {
	t.Helper()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "message":
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("no message event in the stream: %v", sc.Err())
	return ""
}

func TestHTTP_NotificationIsAccepted(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d, want 202", resp.StatusCode)
	}
}

func TestHTTP_Batch(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"bogus"}]`, nil)
	var out []map[string]any
	decodeBody(t, resp, &out)
	if len(out) != 2 || out[0]["id"] != float64(1) || out[1]["error"] == nil {
		t.Fatalf("batch: %v", out)
	}
}

func TestHTTP_ParseError(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `{not json`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var r struct {
		Error rpcError `json:"error"`
	}
	decodeBody(t, resp, &r)
	if r.Error.Code != codeParseError {
		t.Fatalf("code %d", r.Error.Code)
	}
}

func TestHTTP_Sessions(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	id := post(t, ts, initBody, nil).Header.Get("Mcp-Session-Id")

	ping := `{"jsonrpc":"2.0","id":3,"method":"ping"}`
	if s := post(t, ts, ping, map[string]string{"Mcp-Session-Id": id}).StatusCode; s != http.StatusOK {
		t.Fatalf("known session: %d", s)
	}
	if s := post(t, ts, ping, map[string]string{"Mcp-Session-Id": "nope"}).StatusCode; s != http.StatusNotFound {
		t.Fatalf("unknown session: %d, want 404", s)
	}
	// Without a session id the server answers statelessly.
	if s := post(t, ts, ping, nil).StatusCode; s != http.StatusOK {
		t.Fatalf("no session: %d", s)
	}

	del := func() int {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Mcp-Session-Id", id)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if s := del(); s != http.StatusNoContent {
		t.Fatalf("DELETE: %d", s)
	}
	if s := del(); s != http.StatusNotFound {
		t.Fatalf("second DELETE: %d", s)
	}
	if s := post(t, ts, ping, map[string]string{"Mcp-Session-Id": id}).StatusCode; s != http.StatusNotFound {
		t.Fatalf("deleted session: %d, want 404", s)
	}
}

func TestHTTP_ProtocolHeaderAndOrigin(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	ping := `{"jsonrpc":"2.0","id":3,"method":"ping"}`
	if s := post(t, ts, ping, map[string]string{"MCP-Protocol-Version": "2025-06-18"}).StatusCode; s != http.StatusOK {
		t.Errorf("supported version header: %d", s)
	}
	if s := post(t, ts, ping, map[string]string{"MCP-Protocol-Version": "1999-01-01"}).StatusCode; s != http.StatusBadRequest {
		t.Errorf("unsupported version header: %d, want 400", s)
	}
	if s := post(t, ts, ping, map[string]string{"Origin": "https://evil.example"}).StatusCode; s != http.StatusForbidden {
		t.Errorf("foreign origin: %d, want 403", s)
	}
	if s := post(t, ts, ping, map[string]string{"Origin": "http://localhost:3000"}).StatusCode; s != http.StatusOK {
		t.Errorf("loopback origin: %d", s)
	}
}

func TestHTTP_GetStreamEndsOnClose(t *testing.T) {
	ts, h := newHTTPTest(t, HTTPOptions{BearerToken: testToken, KeepAlive: time.Millisecond})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("GET /mcp: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ":") {
		t.Fatalf("first line %q, %v", line, err)
	}
	h.Close()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream not closed after Close")
	}

	// Without Accept: text/event-stream the GET is refused.
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotAcceptable {
		t.Errorf("GET without SSE accept: %d", resp2.StatusCode)
	}
}

func TestHTTPPolicy_SecretExport(t *testing.T) {
	call := func(s *Server, tool string, args map[string]any) string {
		text, _ := s.handlers[tool](context.Background(), args)
		return text
	}

	s := newTestServer()
	s.restrictForHTTP(false)
	if got := call(s, "cert_private_key", map[string]any{"certificate_id": "c", "confirmed": true}); !strings.Contains(got, "--allow-secret-export") {
		t.Errorf("cert_private_key over HTTP: %s", got)
	}
	if got := call(s, "k8s_kubeconfig", map[string]any{"cluster_id": "c"}); !strings.Contains(got, "embed_token=false") {
		t.Errorf("k8s_kubeconfig (default embed) over HTTP: %s", got)
	}
	// Without the token the original handler runs (and fails on the
	// missing engine, not on the policy).
	if got := call(s, "k8s_kubeconfig", map[string]any{"cluster_id": "c", "embed_token": false}); strings.Contains(got, "allow-secret-export") {
		t.Errorf("k8s_kubeconfig embed_token=false refused: %s", got)
	}

	allowed := newTestServer()
	allowed.restrictForHTTP(true)
	if got := call(allowed, "cert_private_key", map[string]any{"certificate_id": "c"}); strings.Contains(got, "allow-secret-export") {
		t.Errorf("cert_private_key with --allow-secret-export refused: %s", got)
	}
	if got := call(allowed, "k8s_kubeconfig", map[string]any{"cluster_id": "c"}); strings.Contains(got, "allow-secret-export") {
		t.Errorf("k8s_kubeconfig with --allow-secret-export refused: %s", got)
	}
}

func TestHTTPPolicy_KubeBinariesMissing(t *testing.T) {
	orig := binaryAvailable
	binaryAvailable = func(string) bool { return false }
	t.Cleanup(func() { binaryAvailable = orig })
	s := newTestServer()
	s.restrictForHTTP(false)
	for _, tool := range []string{"k8s_kubectl", "k8s_apply", "k8s_helm"} {
		text, isErr := s.handlers[tool](context.Background(), map[string]any{"cluster_id": "c", "args": "get pods", "manifest": "x", "action": "list"})
		if !isErr || !strings.Contains(text, "not available in this mode") {
			t.Errorf("%s: %s", tool, text)
		}
	}
	// Tools/list is unchanged: the policy never removes tools.
	if len(s.ListTools()) != len(s.handlers) {
		t.Errorf("tools %d, handlers %d", len(s.ListTools()), len(s.handlers))
	}
}

func TestListenAndServe_ShutsDownOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan net.Addr, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- newTestServer().ListenAndServe(ctx, "127.0.0.1:0", HTTPOptions{BearerToken: testToken}, func(a net.Addr) { addrc <- a })
	}()
	var addr net.Addr
	select {
	case addr = <-addrc:
	case err := <-errc:
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + addr.String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("/healthz: %d %s", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("ListenAndServe: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
}
