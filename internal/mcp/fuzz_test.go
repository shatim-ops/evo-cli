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
	"testing"
	"time"
)

// fuzzServer is a server whose tool handlers are stubs: the fuzzers target
// the transport (framing, decoding, dispatch), not the cloud.
func fuzzServer() *Server {
	s := newTestServer()
	for name := range s.handlers {
		s.handlers[name] = func(_ context.Context, args map[string]any) (string, bool) {
			if args["panic"] == true {
				panic("fuzz")
			}
			b, _ := json.Marshal(args)
			return string(b), false
		}
	}
	return s
}

var fuzzSeeds = []string{
	`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
	`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`,
	`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"status","arguments":{"x":1}}}`,
	`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status","arguments":{"panic":true}}}`,
	`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":[1,2]}`,
	`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"status","arguments":"str"}}`,
	`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
	`{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`,
	`{"jsonrpc":"2.0","id":6}`,
	`{"jsonrpc":"2.0","id":7,"result":{}}`,
	`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"ping"},1,null,[]]`,
	`[]`, `[`, `{`, `null`, `"x"`, `1e999`, "\xff\xfe", "{}\n{}\r\n\n",
}

func FuzzStdioMessages(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	panicLog = io.Discard
	srv := fuzzServer()
	f.Fuzz(func(t *testing.T, data []byte) {
		out := &bytes.Buffer{}
		if err := srv.Run(bytes.NewReader(data), out); err != nil {
			t.Fatalf("Run: %v", err)
		}
		sc := bufio.NewScanner(out)
		sc.Buffer(nil, 64<<20)
		for sc.Scan() {
			var v any
			if err := decodeJSON(sc.Bytes(), &v); err != nil {
				t.Fatalf("response is not JSON: %q", sc.Text())
			}
			checkResponses(t, v)
		}
	})
}

// decodeJSON keeps numbers as json.Number: an id like 1e999 is valid JSON
// that float64 cannot hold.
func decodeJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(v)
}

// checkResponses: every answer is a JSON-RPC 2.0 response with an id (null
// when the request's id was unreadable) and exactly one of result/error.
func checkResponses(t *testing.T, v any) {
	t.Helper()
	list, isBatch := v.([]any)
	if !isBatch {
		list = []any{v}
	} else if len(list) == 0 {
		t.Fatal("empty batch response")
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("response is not an object: %v", e)
		}
		if m["jsonrpc"] != "2.0" {
			t.Fatalf("no jsonrpc 2.0: %v", m)
		}
		if _, ok := m["id"]; !ok {
			t.Fatalf("response without id: %v", m)
		}
		_, hasRes := m["result"]
		_, hasErr := m["error"]
		if hasRes == hasErr {
			t.Fatalf("want exactly one of result/error: %v", m)
		}
	}
}

func FuzzHTTPPost(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s), false)
		f.Add([]byte(s), true)
	}
	panicLog = io.Discard
	h := NewHTTPHandler(fuzzServer(), HTTPOptions{})
	f.Fuzz(func(t *testing.T, body []byte, sse bool) {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sse {
			req.Header.Set("Accept", "application/json, text/event-stream")
		}
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { h.ServeHTTP(rec, req); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("request hangs: %q (sse=%v)", body, sse)
		}
		switch rec.Code {
		case http.StatusOK:
			ct := rec.Header().Get("Content-Type")
			if ct == "text/event-stream" {
				return
			}
			var v any
			if err := decodeJSON(rec.Body.Bytes(), &v); err != nil {
				t.Fatalf("200 with non-JSON body %q", rec.Body.String())
			}
			checkResponses(t, v)
		case http.StatusAccepted:
		case http.StatusBadRequest:
			var v any
			if err := decodeJSON(rec.Body.Bytes(), &v); err != nil {
				t.Fatalf("400 with non-JSON body %q", rec.Body.String())
			}
			checkResponses(t, v)
		default:
			t.Fatalf("status %d for %q", rec.Code, body)
		}
	})
}
