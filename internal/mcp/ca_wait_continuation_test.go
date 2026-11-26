// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// A fresh client follows the interrupted call's advice. The recorder sees the
// server's result even when the original client has disconnected.
func TestCAWait_InterruptedCallReturnsReadOnlyContinuation(t *testing.T) {
	for _, interrupt := range []string{"notification", "disconnect"} {
		t.Run(interrupt, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const app = "web"
				const op = "00000000-0000-4000-8000-000000000031"
				reads, deletes := 0, 0
				previous := httpretry.DefaultBase
				httpretry.DefaultBase = roundTripper(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path != "/v2/containers/"+app {
						return nil, fmt.Errorf("unexpected app path %q", r.URL.Path)
					}
					var body string
					switch r.Method {
					case http.MethodGet:
						reads++
						body = `{"name":"web","status":"DELETING"}`
					case http.MethodDelete:
						deletes++
						body = `{"done":false}`
						if interrupt == "notification" {
							body = `{"id":"` + op + `","done":false}`
						}
					default:
						return nil, fmt.Errorf("unexpected method %s", r.Method)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
				})
				defer func() { httpretry.DefaultBase = previous }()
				server := provisionServer(t, &provisionCloud{})
				handler := NewHTTPHandler(server, HTTPOptions{})
				defer handler.Close()
				request := func(ctx context.Context, body, session string) *httptest.ResponseRecorder {
					req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(body)).WithContext(ctx)
					req.Header.Set("Content-Type", "application/json")
					if session != "" {
						req.Header.Set(headerSessionID, session)
					}
					resp := httptest.NewRecorder()
					handler.ServeHTTP(resp, req)
					return resp
				}
				session := request(t.Context(), initBody, "").Header().Get(headerSessionID)
				if session == "" {
					t.Fatal("initialize did not open a session")
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				reply := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					reply <- request(ctx, `{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"ca_delete","arguments":{"name":"web","confirmed":true,"wait":true}}}`, session)
				}()
				synctest.Wait()
				if deletes != 1 || reads == 0 || len(handler.calls) != 1 {
					t.Fatalf("active wait: deletes=%d reads=%d slots=%d", deletes, reads, len(handler.calls))
				}
				if interrupt == "notification" {
					notice := request(t.Context(), `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":3}}`, session)
					if notice.Code != http.StatusAccepted {
						t.Fatalf("cancellation notification: HTTP %d", notice.Code)
					}
				} else {
					cancel()
				}
				synctest.Wait()
				var w *httptest.ResponseRecorder
				select {
				case w = <-reply:
				default:
					t.Fatal("call still running after cancellation")
				}
				var result struct {
					Result struct {
						IsError bool
						Content []struct{ Text string }
					}
				}
				if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Result.IsError || len(result.Result.Content) != 1 {
					t.Fatalf("interrupted tool result: HTTP %d %s", w.Code, w.Body)
				}
				var got struct {
					Reason      string
					Name        string
					OperationID *string        `json:"operation_id"`
					NextTool    string         `json:"next_tool"`
					NextArgs    map[string]any `json:"next_args"`
				}
				if err := json.Unmarshal([]byte(result.Result.Content[0].Text), &got); err != nil {
					t.Fatal(err)
				}
				if got.Reason != "INTERRUPTED" || got.Name != app || len(handler.calls) != 0 {
					t.Fatalf("interruption reason=%q name=%q slots=%d", got.Reason, got.Name, len(handler.calls))
				}
				if interrupt == "notification" && (got.OperationID == nil || *got.OperationID != op) || interrupt == "disconnect" && got.OperationID != nil {
					t.Fatalf("unexpected operation ID in %s", result.Result.Content[0].Text)
				}
				after := reads
				time.Sleep(20 * time.Second)
				synctest.Wait()
				if reads != after {
					t.Fatalf("%d reads after cancellation", reads-after)
				}
				if got.NextTool != "ca_get" || got.NextArgs["name"] != app {
					t.Fatalf("unusable continuation: next_tool=%q next_args=%v", got.NextTool, got.NextArgs)
				}
				next := h14AuditCall(t, server, handler, got.NextTool, got.NextArgs)
				if next["status"] != "DELETING" || reads != after+1 || deletes != 1 {
					t.Fatalf("continued call: result=%v reads=%d additional deletes=%d", next, reads-after, deletes-1)
				}
			})
		})
	}
}
