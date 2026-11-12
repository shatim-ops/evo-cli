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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// docs/mcp.md, the protocol-version line: the server answers the version the
// client asked for when it knows it, otherwise the newest it speaks.
// A number does not unmarshal into the string field, and "" is not a known
// version, so both are answered with the default. That is the documented
// rule, not a defect.
func TestContract_ProtocolVersionFallsBack(t *testing.T) {
	for _, params := range []string{
		`{"protocolVersion":1}`,
		`{"protocolVersion":""}`,
		`{}`,
	} {
		got := roundTrip(t, newTestServer(t), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)
		res, _ := got["result"].(map[string]any)
		if res["protocolVersion"] != defaultProtocolVersion {
			t.Errorf("%s: protocolVersion %v, want %s", params, res["protocolVersion"], defaultProtocolVersion)
		}
	}
	// A known version is echoed. docs/mcp.md lists these three.
	got := roundTrip(t, newTestServer(t), `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	res, _ := got["result"].(map[string]any)
	if res["protocolVersion"] != "2024-11-05" {
		t.Fatalf("known version: %v", res["protocolVersion"])
	}
}

// D89 («Риск», conclusion 38): over stdio a request before initialize is
// refused with -32002 «server not initialized» (MCP, lifecycle) — the
// agent would work without the instructions of initialize; ping and the
// notifications pass. A second initialize is accepted, and the next run
// is a session of its own.
func TestContract_InitializeRequiredOnStdio(t *testing.T) {
	s := newTestServer(t)
	got := rawLines(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/list"}`,
	)
	if len(got) != 6 {
		t.Fatalf("got %d responses: %v", len(got), got)
	}
	for i := 0; i < 2; i++ {
		if rpcCode(got[i]) != codeNotInitialized {
			t.Errorf("request %d before initialize: %v, want %d", i+1, got[i], codeNotInitialized)
		}
	}
	if got[2].(map[string]any)["result"] == nil {
		t.Errorf("ping before initialize: %v", got[2])
	}
	a, _ := got[3].(map[string]any)["result"].(map[string]any)
	b, _ := got[4].(map[string]any)["result"].(map[string]any)
	if a["protocolVersion"] != "2025-06-18" || b["protocolVersion"] != "2025-03-26" {
		t.Fatalf("second initialize: %v %v", a, b)
	}
	if res, _ := got[5].(map[string]any)["result"].(map[string]any); res["tools"] == nil {
		t.Errorf("tools/list after initialize: %v", got[5])
	}
	// The next run is a new session: initialize again.
	if again := rawLines(t, s, `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`); len(again) != 1 || rpcCode(again[0]) != codeNotInitialized {
		t.Errorf("a new run without initialize: %v", again)
	}
}

// Over HTTP a session id is issued only by initialize: a call with an id
// the server did not issue is 404, so a session with an id has always
// initialized; a client without an id is served statelessly, by design
// (D89; «Риск», conclusion 38).
func TestContract_HTTPSessionIDOnlyAfterInitialize(t *testing.T) {
	h := NewHTTPHandler(newTestServer(t), HTTPOptions{BearerToken: "t"})
	t.Cleanup(h.Close)
	post := func(sid, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Authorization", "Bearer t")
		if sid != "" {
			r.Header.Set("Mcp-Session-Id", sid)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if w := post("0123456789abcdef0123456789abcdef", call); w.Code != http.StatusNotFound {
		t.Errorf("a session id the server did not issue: %d %s", w.Code, w.Body)
	}
	w := post("", call)
	if w.Code != http.StatusOK || w.Header().Get("Mcp-Session-Id") != "" {
		t.Errorf("without a session id (stateless): %d, id %q", w.Code, w.Header().Get("Mcp-Session-Id"))
	}
	w = post("", `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`)
	sid := w.Header().Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatalf("initialize issued no session id: %d %s", w.Code, w.Body)
	}
	if w := post(sid, call); w.Code != http.StatusOK {
		t.Errorf("the issued session: %d %s", w.Code, w.Body)
	}
}

// Two JSON values on one line are not one message (json.Valid fails): one
// parse error, and the cloud is not called. CR LF is a newline.
func TestContract_StdioFraming(t *testing.T) {
	old := httpretry.DefaultBase
	var n int
	httpretry.DefaultBase = roundTripper(func(*http.Request) (*http.Response, error) {
		n++
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("x")), Header: http.Header{}}, nil
	})
	t.Cleanup(func() { httpretry.DefaultBase = old })

	got := runLines(t, newTestServer(t),
		`{"jsonrpc":"2.0","id":1,"method":"ping"}{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(got) != 1 || rpcCode(got[0]) != codeParseError {
		t.Fatalf("two objects: %v", got)
	}
	if n != 0 {
		t.Fatalf("parse error sent %d cloud requests", n)
	}

	out := &bytes.Buffer{}
	line := "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"ping\"}\r\n"
	if err := newTestServer(t).Run(strings.NewReader(line), out); err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp) != nil || resp["id"] != float64(9) {
		t.Fatalf("CRLF: %s", out.String())
	}
}

// Invalid UTF-8 inside a JSON string is TestBreak_UTF8_ReplacedQueryHitsCloud:
// encoding/json accepts it, so it is not a contract of the framing tests.

// resources/list is not a method this server implements. JSON-RPC -32601.
func TestContract_ResourcesListIsMethodNotFound(t *testing.T) {
	got := roundTrip(t, newTestServer(t), `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	if rpcCode(got) != codeMethodNotFound {
		t.Fatalf("resources/list: %v", got)
	}
}

// The limit is maxMessageSize (16 MiB). A line of exactly that many bytes
// plus the newline is accepted; one more byte is refused and the next line
// still runs. The check is the one in readLine: len(line)+len(chunk) > max+1.
func TestContract_LineLimitBoundary(t *testing.T) {
	if maxMessageSize != 16<<20 {
		t.Fatalf("maxMessageSize = %d, the boundary test must follow the constant", maxMessageSize)
	}
	exact := append(bytes.Repeat([]byte("a"), maxMessageSize), '\n')
	line, tooLong, err := readLine(bufio.NewReader(bytes.NewReader(exact)), maxMessageSize)
	if err != nil || tooLong || len(line) != maxMessageSize+1 {
		t.Fatalf("exact max: tooLong=%v err=%v len=%d", tooLong, err, len(line))
	}
	over := append(bytes.Repeat([]byte("b"), maxMessageSize+1), '\n')
	_, tooLong, err = readLine(bufio.NewReader(bytes.NewReader(over)), maxMessageSize)
	if err != nil || !tooLong {
		t.Fatalf("max+1: tooLong=%v err=%v", tooLong, err)
	}

	// The same boundary through Run: the oversized line is one error, then ping.
	body := append([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":"`), bytes.Repeat([]byte("c"), maxMessageSize)...)
	body = append(body, '"', '}', '}', '\n')
	body = append(body, `{"jsonrpc":"2.0","id":2,"method":"ping"}`...)
	body = append(body, '\n')
	out := &bytes.Buffer{}
	if err := newTestServer(t).Run(bytes.NewReader(body), out); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), maxMessageSize+1024)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			t.Fatalf("response not JSON: %s", sc.Text())
		}
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 || rpcCode(msgs[0]) != codeInvalidReq || msgs[1]["id"] != float64(2) {
		t.Fatalf("boundary through Run: %v", msgs)
	}
}

// docs/mcp.md «Отмена»: closing stdin does not cancel a call. The line read
// before EOF runs to the end, is answered, and only then does the server
// return. Nothing is written to state: the handler blocks on a channel and
// returns a fixed body.
func TestContract_StdinCloseDoesNotCancel(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New(func(map[string]any) (*engine.Engine, error) {
		return nil, io.EOF
	}, "test")
	s.SetStateDir(dir)
	started := make(chan struct{})
	release := make(chan struct{})
	s.handlers["status"] = func(context.Context, map[string]any) (string, bool) {
		close(started)
		<-release
		return `{"status":"ok"}`, false
	}
	pr, pw := io.Pipe()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- s.Run(pr, out) }()
	if _, err := io.WriteString(pw, initLine+"\n"+`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	<-started
	select {
	case err := <-done:
		t.Fatalf("Run returned before the call finished: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"id":1`)) && !bytes.Contains(out.Bytes(), []byte(`"id": 1`)) {
		t.Fatalf("no answer after the call: %s", out.String())
	}
	if _, err := os.Stat(".evo"); !os.IsNotExist(err) {
		t.Fatalf("state appeared: %v", err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
