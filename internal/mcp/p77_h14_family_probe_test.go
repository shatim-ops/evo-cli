// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

func TestP77H14Families(t *testing.T) {
	for _, wire := range []string{"stdio", "http"} {
		for _, tool := range []string{"ar_operation", "cert_operation", "dns_operation", "k8s_operation", "kafka_operation", "lb_operation", "pg_operation", "redis_operation", "vpc_operation", "ca_delete_5s", "ca_delete_50s"} {
			t.Run(wire+"/"+tool, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					const id = "00000000-0000-4000-8000-000000000031"
					start := time.Now()
					readyAfter := 50 * time.Second
					if tool == "ca_delete_5s" {
						readyAfter = 5 * time.Second
					}
					readyAt := start.Add(readyAfter)
					reads, writes := 0, 0
					old := httpretry.DefaultBase
					httpretry.DefaultBase = roundTripper(func(r *http.Request) (*http.Response, error) {
						status := 200
						done := !time.Now().Before(readyAt)
						var body string
						if strings.HasPrefix(tool, "ca_delete_") {
							if r.URL.Path != "/v2/containers/web" {
								return nil, fmt.Errorf("unexpected app path %s", r.URL.Path)
							}
							switch r.Method {
							case "DELETE":
								writes++
								body = `{"id":"` + id + `","done":false}`
							case "GET":
								reads++
								if writes > 0 && done {
									status = 404
									body = `{"message":"not found"}`
								} else {
									body = `{"name":"web","status":"DELETING"}`
								}
							default:
								return nil, fmt.Errorf("unexpected app verb %s", r.Method)
							}
						} else {
							if r.Method != "GET" || path.Base(r.URL.Path) != id {
								return nil, fmt.Errorf("unexpected operation %s %s", r.Method, r.URL.Path)
							}
							reads++
							body = fmt.Sprintf(`{"id":"%s","done":%t}`, id, done)
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
					})
					defer func() { httpretry.DefaultBase = old }()
					server := provisionServer(t, &provisionCloud{})
					var handler *HTTPHandler
					if wire == "http" {
						handler = NewHTTPHandler(server, HTTPOptions{})
						defer handler.Close()
					}
					if strings.HasPrefix(tool, "ca_delete_") {
						result := h14AuditCall(t, server, handler, "ca_delete", map[string]any{"name": "web", "confirmed": true, "wait": true})
						t.Logf("wait=true; resource gone at %s: t=%s status=%v next_tool=%v DELETE=%d", readyAfter, time.Since(start), result["status"], result["next_tool"], writes)
						if result["status"] != "deleted" || writes != 1 || time.Now().Before(readyAt) {
							t.Errorf("expected one complete delete after %s", readyAfter)
						}
						return
					}
					args := map[string]any{"operation_id": id, "wait_seconds": 45}
					first := h14AuditCall(t, server, handler, tool, args)
					// wall-clock: виртуальные часы synctest проверяют предел H14 — 45 с на ожидание операции облака.
					if first["done"] != false || first["still_running"] != true || first["id"] != id || time.Since(start) > 45*time.Second {
						t.Fatalf("first %s, got %v", time.Since(start), first)
					}
					firstTime := time.Since(start)
					second := h14AuditCall(t, server, handler, tool, args)
					if second["done"] != true || second["id"] != id || time.Now().Before(readyAt) || reads < 2 {
						t.Fatalf("repeat %s, got %v", time.Since(start), second)
					}
					t.Logf("same operation id: t=%s done=false still_running=true; t=%s done=true; reads=%d writes=%d", firstTime, time.Since(start), reads, writes)
				})
			})
		}
	}
}

func h14AuditCall(t *testing.T, s *Server, handler *HTTPHandler, name string, args map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`, data)
	var rpc map[string]any
	if handler == nil {
		rpc = roundTrip(t, s, req)
	} else {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(req))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rpc); err != nil {
			t.Fatal(err)
		}
	}
	result, _ := rpc["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if rpc["id"] != float64(2) || result["isError"] == true || len(content) != 1 {
		t.Fatalf("unexpected tools/call envelope: id=%v isError=%v content blocks=%d", rpc["id"], result["isError"], len(content))
	}
	message, _ := content[0].(map[string]any)
	text, _ := message["text"].(string)
	var answer map[string]any
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}
