// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
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
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The cloud knows one VM. It is readable immediately, and becomes active
// 50 seconds after CreateVM, beyond the 45-second MCP budget (H14).
type slowProvisionVM struct {
	provisionCloud
	id       string
	creates  int
	activeAt time.Time
}

func (c *slowProvisionVM) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	vm, err := c.provisionCloud.CreateVM(ctx, req)
	if err == nil {
		c.creates++
		c.id = vm.ID
		c.activeAt = time.Now().Add(50 * time.Second)
	}
	return vm, err
}

func (c *slowProvisionVM) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	if id != c.id || id == "" {
		return nil, fmt.Errorf("unexpected VM id %q", id)
	}
	// A read takes one virtual second, or ends sooner at the caller's deadline.
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	status := "creating"
	if !time.Now().Before(c.activeAt) {
		status = "active"
	}
	return &provision.VM{ID: id, Name: "web", Status: status}, nil
}

// Both transports use the same cloud timeline. Only the wait argument changes.
func TestH14_ProvisionContinuationAndWait(t *testing.T) {
	for _, transport := range []string{"stdio", "http"} {
		for _, mode := range []string{"continue", "wait-true"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					checkH14Provision(t, transport, mode == "wait-true")
				})
			})
		}
	}
}

func checkH14Provision(t *testing.T, transport string, wait bool) {
	t.Helper()
	cloud := &slowProvisionVM{}
	s := provisionServer(t, cloud)
	var handler *HTTPHandler
	if transport == "http" {
		handler = NewHTTPHandler(s, HTTPOptions{})
		defer handler.Close()
	}
	args := map[string]any{"name": "web", "no_public_ip": true, "confirmed": true}
	if wait {
		args["wait"] = true
	}
	start := time.Now()
	answer := h14Call(t, s, handler, "provision", args)
	if !wait {
		elapsed := time.Since(start)
		next, _ := answer["next_args"].(map[string]any)
		if elapsed > 45*time.Second || answer["status"] != "creating" || answer["vm_status"] != "creating" {
			t.Fatalf("first provision after %s: status=%v vm_status=%v", elapsed, answer["status"], answer["vm_status"])
		}
		if answer["next_tool"] != "provision" || answer["next"] == nil || answer["next"] == "" || answer["vm_id"] != cloud.id || next["vm_id"] != cloud.id {
			t.Fatalf("bounded provision after %s: status=%v vm_status=%v next_tool=%v next_args=%v", elapsed, answer["status"], answer["vm_status"], answer["next_tool"], next)
		}
		t.Logf("first call after %s: status=%v, next_tool=%v, receipt matches VM", elapsed, answer["status"], answer["next_tool"])
		answer = h14Call(t, s, handler, answer["next_tool"].(string), next)
	}
	if answer["status"] != "ok" || cloud.creates != 1 || answer["vm_id"] != cloud.id || time.Now().Before(cloud.activeAt) {
		t.Fatalf("provision after %s: status=%v creates=%d vm_id=%v, want the same completed VM", time.Since(start), answer["status"], cloud.creates, answer["vm_id"])
	}
	t.Logf("finished after %s: status=%v, creates=%d, same VM", time.Since(start), answer["status"], cloud.creates)
}

func h14Call(t *testing.T, s *Server, handler *HTTPHandler, name string, args map[string]any) map[string]any {
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
