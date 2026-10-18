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
	"os"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
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
		got := roundTrip(t, newTestServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)
		res, _ := got["result"].(map[string]any)
		if res["protocolVersion"] != defaultProtocolVersion {
			t.Errorf("%s: protocolVersion %v, want %s", params, res["protocolVersion"], defaultProtocolVersion)
		}
	}
	// A known version is echoed. docs/mcp.md lists these three.
	got := roundTrip(t, newTestServer(), `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	res, _ := got["result"].(map[string]any)
	if res["protocolVersion"] != "2024-11-05" {
		t.Fatalf("known version: %v", res["protocolVersion"])
	}
}

// docs/mcp.md does not require initialize before tools/call, and does not
// forbid a second initialize. Both are accepted.
func TestContract_InitializeIsOptional(t *testing.T) {
	s := newTestServer()
	got := runLines(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
	)
	if len(got) != 3 {
		t.Fatalf("got %d responses", len(got))
	}
	if rpcCode(got[0]) != 0 && got[0].(map[string]any)["result"] == nil {
		t.Fatalf("tools/call before initialize: %v", got[0])
	}
	a, _ := got[1].(map[string]any)["result"].(map[string]any)
	b, _ := got[2].(map[string]any)["result"].(map[string]any)
	if a["protocolVersion"] != "2025-06-18" || b["protocolVersion"] != "2025-03-26" {
		t.Fatalf("second initialize: %v %v", a, b)
	}
}

// Two JSON values on one line are not one message (json.Valid fails): one
// parse error, and the cloud is not called. CR LF is a newline.
func TestContract_StdioFraming(t *testing.T) {
	old := http.DefaultTransport
	var n int
	http.DefaultTransport = roundTripper(func(*http.Request) (*http.Response, error) {
		n++
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("x")), Header: http.Header{}}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })

	got := runLines(t, newTestServer(),
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
	if err := newTestServer().Run(strings.NewReader(line), out); err != nil {
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
	got := roundTrip(t, newTestServer(), `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
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
	if err := newTestServer().Run(bytes.NewReader(body), out); err != nil {
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
	if _, err := io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`+"\n"); err != nil {
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
