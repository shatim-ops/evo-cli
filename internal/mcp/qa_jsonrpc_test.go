// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// runLines feeds the lines to the stdio transport and returns the decoded
// response lines.
func runLines(t *testing.T, s *Server, lines ...string) []any {
	t.Helper()
	out := &bytes.Buffer{}
	if err := s.Run(strings.NewReader(strings.Join(lines, "\n")+"\n"), out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []any
	sc := bufio.NewScanner(out)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var v any
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatalf("response line is not JSON: %q", sc.Text())
		}
		got = append(got, v)
	}
	return got
}

// JSON-RPC 2.0: «The Server MUST NOT reply to a Notification». До фикса
// ping и tools/call без id получали ответ без id — строгий клиент принимает
// его за ответ на неизвестный запрос.
func TestQA_NotificationsAreNotAnswered(t *testing.T) {
	got := runLines(t, newTestServer(),
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"status"}}`,
		`{"jsonrpc":"2.0","method":"initialize"}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(got) != 1 || got[0].(map[string]any)["id"] != float64(7) {
		t.Fatalf("want only the answer to id 7, got %v", got)
	}

	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `[{"jsonrpc":"2.0","method":"ping"},{"jsonrpc":"2.0","id":1,"method":"ping"}]`, nil)
	var out []map[string]any
	decodeBody(t, resp, &out)
	if len(out) != 1 || out[0]["id"] != float64(1) {
		t.Fatalf("HTTP batch: notification answered: %v", out)
	}
}

// Ответ на сообщение, id которого не прочитать, обязан нести "id": null.
func TestQA_ErrorWithoutRequestIDHasNullID(t *testing.T) {
	for _, line := range []string{`{not json`, `[]`, `"str"`, `{"jsonrpc":"2.0"}`} {
		got := runLines(t, newTestServer(), line)
		if len(got) != 1 {
			t.Fatalf("%s: %d responses", line, len(got))
		}
		m := got[0].(map[string]any)
		id, ok := m["id"]
		if !ok || id != nil {
			t.Errorf("%s: want \"id\": null, got %v", line, m)
		}
		if m["error"] == nil {
			t.Errorf("%s: want an error, got %v", line, m)
		}
	}
}

func rpcCode(v any) float64 {
	m, _ := v.(map[string]any)
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

// MCP 2025-03-26 (сервер её поддерживает): реализация MUST принимать
// батчи. По stdio батч получал parse error; пустой батч и не-объект в
// батче — Invalid Request, а не parse error на весь батч.
func TestQA_Batches(t *testing.T) {
	s := newTestServer()
	got := runLines(t, s, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},1]`)
	if len(got) != 1 {
		t.Fatalf("want one batch response, got %v", got)
	}
	arr, ok := got[0].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("stdio batch: want array of 2, got %v", got[0])
	}
	if arr[0].(map[string]any)["id"] != float64(1) || rpcCode(arr[1]) != codeInvalidReq {
		t.Fatalf("stdio batch: %v", arr)
	}

	got = runLines(t, s, `[]`)
	if _, isArr := got[0].([]any); isArr || rpcCode(got[0]) != codeInvalidReq {
		t.Fatalf("empty batch: want a single Invalid Request, got %v", got)
	}

	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	resp := post(t, ts, `[1,{"jsonrpc":"2.0","id":2,"method":"ping"}]`, nil)
	var out []any
	decodeBody(t, resp, &out)
	if resp.StatusCode != http.StatusOK || len(out) != 2 || rpcCode(out[0]) != codeInvalidReq {
		t.Fatalf("HTTP mixed batch: %d %v", resp.StatusCode, out)
	}
	resp = post(t, ts, `[]`, nil)
	var single map[string]any
	decodeBody(t, resp, &single)
	if rpcCode(single) != codeInvalidReq {
		t.Fatalf("HTTP empty batch: %v", single)
	}
}

// Запрос без method — Invalid Request (-32600), не «method not found: ».
// Ответ клиента (result/error без method) не получает ответа.
func TestQA_MessageWithoutMethod(t *testing.T) {
	got := runLines(t, newTestServer(),
		`{"jsonrpc":"2.0","id":3}`,
		`{"jsonrpc":"2.0","id":4,"result":{}}`,
		`{"jsonrpc":"2.0","id":5,"error":{"code":1,"message":"x"}}`,
	)
	if len(got) != 1 || rpcCode(got[0]) != codeInvalidReq || got[0].(map[string]any)["id"] != float64(3) {
		t.Fatalf("got %v", got)
	}
}

// Строка stdin больше лимита роняла сервер (bufio.Scanner: token too long),
// все следующие запросы терялись. Теперь — ошибка в ответ и работа дальше.
func TestQA_OversizedLineDoesNotKillStdio(t *testing.T) {
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{"pad":"` +
		strings.Repeat("x", maxMessageSize) + `"}}}`
	got := runLines(t, newTestServer(), big, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if len(got) != 2 {
		t.Fatalf("want 2 responses (error, pong), got %d", len(got))
	}
	if rpcCode(got[0]) != codeInvalidReq {
		t.Errorf("oversized line: %v", got[0])
	}
	if got[1].(map[string]any)["id"] != float64(2) {
		t.Errorf("request after the oversized line: %v", got[1])
	}
}

// Паника обработчика не роняет сервер: ни stdio, ни HTTP (там SSE-ответ
// tools/call считается в отдельной горутине, паника в ней убивает процесс).
func TestQA_ToolPanicDoesNotKillServer(t *testing.T) {
	panicLog = io.Discard
	t.Cleanup(func() { panicLog = os.Stderr })
	s := newTestServer()
	s.handlers["status"] = func(context.Context, map[string]any) (string, bool) { panic("boom") }
	got := runLines(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	res, _ := got[0].(map[string]any)["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(fmt.Sprint(res["content"]), "boom") {
		t.Fatalf("panic must become a tool error: %v", got[0])
	}

	// Through the registry: the HTTP policy re-reads every handler from it.
	hs := newTestServer()
	hs.wrapHandler("status", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) { panic("boom") }
	})
	h := NewHTTPHandler(hs, HTTPOptions{BearerToken: testToken})
	ts := newServerFor(t, h)
	resp := post(t, ts, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status"}}`,
		map[string]string{"Accept": "application/json, text/event-stream"})
	if msg := readSSEMessage(t, resp.Body); !strings.Contains(msg, "boom") {
		t.Fatalf("SSE: %s", msg)
	}
}

// DNS rebinding: без токена (--auth none на loopback) страница evil.example,
// чьё имя указывает на 127.0.0.1, шлёт Origin и Host evil.example — до фикса
// проверка Origin == Host её пропускала.
func TestQA_NoAuthRejectsForeignHost(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{})
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	evil := "evil.example:" + port
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Host = evil
	req.Header.Set("Origin", "http://"+evil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("rebinding request: %d, want 403", resp.StatusCode)
	}
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Host %s: %d", host, resp.StatusCode)
		}
	}
}

// Медленное тело (slowloris): ReadHeaderTimeout ограничивает только
// заголовки, тело можно было тянуть бесконечно, держа соединение и горутину.
func TestQA_SlowBodyTimesOut(t *testing.T) {
	old := httpBodyTimeout
	httpBodyTimeout = 300 * time.Millisecond
	t.Cleanup(func() { httpBodyTimeout = old })

	ts, _ := newHTTPTest(t, HTTPOptions{})
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer to a stalled body: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status %d, want 408", resp.StatusCode)
	}
}

func newServerFor(t *testing.T, h *HTTPHandler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})
	return ts
}

// S1-app 1: страница на другом loopback-порту шлёт text/plain без
// preflight; с --auth none такой POST доходил до обработчика. Теперь — 415,
// а JSON с charset принимается.
func TestQA_PostNeedsJSONContentType(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{})
	for _, tc := range []struct {
		ct   string
		want int
	}{
		{"text/plain", http.StatusUnsupportedMediaType},
		{"", http.StatusUnsupportedMediaType},
		{"application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"application/json; charset=utf-8", http.StatusOK},
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Origin", "http://localhost:5173")
		if tc.ct != "" {
			req.Header.Set("Content-Type", tc.ct)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("Content-Type %q: %d, want %d", tc.ct, resp.StatusCode, tc.want)
		}
	}
}
