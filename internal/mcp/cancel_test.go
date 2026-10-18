// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Обработчики получали context.Background(): ушедший клиент, закрытый
// stdin, DELETE сессии или notifications/cancelled не доходили до вызова,
// и provision/k8s_helm работали дальше минутами. Здесь обработчик-заглушка
// ждёт ctx.Done() и сообщает причину отмены.

const waitLimit = 5 * time.Second

type blocker struct {
	started chan struct{}
	causes  chan error
}

// blockTool подменяет vm_list обработчиком, который ждёт отмены.
func blockTool(s *Server) *blocker {
	b := &blocker{started: make(chan struct{}, 8), causes: make(chan error, 8)}
	s.wrapHandler("vm_list", func(tools.Handler) tools.Handler {
		return func(ctx context.Context, _ map[string]any) (string, bool) {
			b.started <- struct{}{}
			<-ctx.Done()
			b.causes <- context.Cause(ctx)
			return `{"status":"error","error":"cancelled"}`, true
		}
	})
	return b
}

func (b *blocker) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(waitLimit):
		t.Fatal("обработчик не запустился")
	}
}

func (b *blocker) wantCause(t *testing.T, want error) {
	t.Helper()
	select {
	case got := <-b.causes:
		if !errors.Is(got, want) {
			t.Fatalf("причина отмены %v, ждали %v", got, want)
		}
	case <-time.After(waitLimit):
		t.Fatalf("отмена (%v) не дошла до обработчика", want)
	}
}

const callLine = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"vm_list"}}` + "\n"

// stdio: запускает RunContext на трубе; возвращает запись в stdin, чтение
// ответов и канал с результатом RunContext.
func runStdio(t *testing.T, ctx context.Context, s *Server) (*io.PipeWriter, chan string, chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- s.RunContext(ctx, inR, outW)
		outW.Close()
	}()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() { inW.Close() })
	return inW, lines, done
}

func nextLine(t *testing.T, lines chan string) string {
	t.Helper()
	select {
	case l := <-lines:
		return l
	case <-time.After(waitLimit):
		t.Fatal("нет ответа")
		return ""
	}
}

func write(t *testing.T, w io.Writer, line string) {
	t.Helper()
	if _, err := io.WriteString(w, line); err != nil {
		t.Fatal(err)
	}
}

func waitRun(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(waitLimit):
		t.Fatal("RunContext не вернулся")
		return nil
	}
}

func TestCancel_StdioNotificationCancelsCallWithoutAnswer(t *testing.T) {
	s := newTestServer()
	b := blockTool(s)
	in, out, done := runStdio(t, context.Background(), s)
	write(t, in, callLine)
	b.waitStarted(t)
	write(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"user"}}`+"\n")
	b.wantCause(t, errCancelledByClient)

	// На отменённый вызов ответа нет: первым приходит ответ на ping.
	write(t, in, `{"jsonrpc":"2.0","id":8,"method":"ping"}`+"\n")
	line := nextLine(t, out)
	var resp map[string]any
	_ = json.Unmarshal([]byte(line), &resp)
	if resp["id"] != float64(8) {
		t.Fatalf("первый ответ — не ping: %s", line)
	}
	in.Close()
	if err := waitRun(t, done); err != nil {
		t.Fatal(err)
	}
}

// Вызов, ещё ждущий в очереди за другим, тоже отменяется по id.
func TestCancel_StdioNotificationCancelsQueuedCall(t *testing.T) {
	s := newTestServer()
	b := blockTool(s)
	in, out, _ := runStdio(t, context.Background(), s)
	write(t, in, callLine)
	b.waitStarted(t)
	write(t, in, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"vm_list"}}`+"\n")
	write(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":9}}`+"\n")
	write(t, in, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`+"\n")
	b.wantCause(t, errCancelledByClient) // 7
	b.waitStarted(t)                     // 9 стартует уже отменённым
	b.wantCause(t, errCancelledByClient)
	write(t, in, `{"jsonrpc":"2.0","id":10,"method":"ping"}`+"\n")
	if line := nextLine(t, out); !strings.Contains(line, `"id":10`) {
		t.Fatalf("ответы на отменённые вызовы: %s", line)
	}
}

// Закрытый stdin вызов не отменяет: клиенты вида «printf … | evo mcp»
// закрывают stdin сразу после запроса и ждут ответа. Вызов доходит до
// конца, ответ пишется, затем Run завершается.
func TestCancel_StdioEOFLetsCallFinish(t *testing.T) {
	s := newTestServer()
	started, release := make(chan struct{}), make(chan struct{})
	cause := make(chan error, 1)
	s.wrapHandler("vm_list", func(tools.Handler) tools.Handler {
		return func(ctx context.Context, _ map[string]any) (string, bool) {
			close(started)
			select {
			case <-release:
				cause <- nil
			case <-ctx.Done():
				cause <- context.Cause(ctx)
			}
			return `{"status":"ok"}`, false
		}
	})
	in, out, done := runStdio(t, context.Background(), s)
	write(t, in, callLine)
	select {
	case <-started:
	case <-time.After(waitLimit):
		t.Fatal("обработчик не запустился")
	}
	in.Close()
	time.Sleep(50 * time.Millisecond) // reader видит EOF раньше, чем вызов завершится
	close(release)
	if err := <-cause; err != nil {
		t.Fatalf("EOF отменил вызов: %v", err)
	}
	if line := nextLine(t, out); !strings.Contains(line, `"id":7`) {
		t.Fatalf("нет ответа на вызов после EOF: %s", line)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatal(err)
	}
}

// SIGINT/SIGTERM (контекст из main) отменяет вызов; RunContext возвращает
// ctx.Err(), дождавшись обработчика.
func TestCancel_StdioSignalCancelsCall(t *testing.T) {
	s := newTestServer()
	b := blockTool(s)
	ctx, cancel := context.WithCancel(context.Background())
	in, _, done := runStdio(t, ctx, s)
	write(t, in, callLine)
	b.waitStarted(t)
	cancel()
	b.wantCause(t, context.Canceled)
	if err := waitRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext = %v", err)
	}
}

// --- HTTP ---

func newCancelHTTP(t *testing.T) (*httptest.Server, *HTTPHandler, *blocker) {
	t.Helper()
	s := newTestServer()
	b := blockTool(s) // до NewHTTPHandler: его обёртки ложатся поверх
	h := NewHTTPHandler(s, HTTPOptions{BearerToken: testToken})
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})
	return ts, h, b
}

func mcpRequest(t *testing.T, ctx context.Context, ts *httptest.Server, method, body string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, ts.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func initSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp := post(t, ts, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	id := resp.Header.Get(headerSessionID)
	if id == "" {
		t.Fatal("нет Mcp-Session-Id")
	}
	return id
}

// asyncCall шлёт tools/call и возвращает канал с ответом (или ошибкой).
func asyncCall(t *testing.T, ctx context.Context, ts *httptest.Server, headers map[string]string) chan string {
	t.Helper()
	got := make(chan string, 1)
	req := mcpRequest(t, ctx, ts, http.MethodPost, strings.TrimSpace(callLine), headers)
	go func() {
		resp, err := ts.Client().Do(req)
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		got <- string(data)
	}()
	return got
}

func TestCancel_HTTPClientDisconnectCancelsCall(t *testing.T) {
	for _, accept := range []string{"application/json", "application/json, text/event-stream"} {
		t.Run(accept, func(t *testing.T) {
			ts, _, b := newCancelHTTP(t)
			ctx, cancel := context.WithCancel(context.Background())
			asyncCall(t, ctx, ts, map[string]string{"Accept": accept})
			b.waitStarted(t)
			cancel() // клиент закрыл соединение
			b.wantCause(t, errClientGone)
		})
	}
}

func TestCancel_HTTPSessionDeleteCancelsItsCalls(t *testing.T) {
	ts, _, b := newCancelHTTP(t)
	sid := initSession(t, ts)
	asyncCall(t, context.Background(), ts, map[string]string{headerSessionID: sid, "Accept": "application/json, text/event-stream"})
	b.waitStarted(t)
	resp, err := ts.Client().Do(mcpRequest(t, context.Background(), ts, http.MethodDelete, "", map[string]string{headerSessionID: sid}))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE: %d", resp.StatusCode)
	}
	b.wantCause(t, errSessionDeleted)
}

func TestCancel_HTTPNotificationCancelsOnlyItsSessionCall(t *testing.T) {
	ts, _, b := newCancelHTTP(t)
	sid := initSession(t, ts)
	other := initSession(t, ts)
	answer := asyncCall(t, context.Background(), ts, map[string]string{headerSessionID: sid, "Accept": "application/json, text/event-stream"})
	b.waitStarted(t)

	cancelNote := `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`
	// Тот же id из чужой сессии не трогает вызов.
	if resp := post(t, ts, cancelNote, map[string]string{headerSessionID: other}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("уведомление: %d", resp.StatusCode)
	}
	select {
	case cause := <-b.causes:
		t.Fatalf("отменён уведомлением чужой сессии: %v", cause)
	case <-time.After(100 * time.Millisecond):
	}

	if resp := post(t, ts, cancelNote, map[string]string{headerSessionID: sid}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("уведомление: %d", resp.StatusCode)
	}
	b.wantCause(t, errCancelledByClient)
	select {
	case body := <-answer:
		if strings.Contains(body, "event: message") {
			t.Errorf("ответ на отменённый вызов: %s", body)
		}
	case <-time.After(waitLimit):
		t.Fatal("поток SSE не закрылся")
	}
}

func TestCancel_HTTPServerCloseCancelsCalls(t *testing.T) {
	ts, h, b := newCancelHTTP(t)
	asyncCall(t, context.Background(), ts, map[string]string{"Accept": "application/json"})
	b.waitStarted(t)
	h.Close()
	b.wantCause(t, errServerStopping)
}
