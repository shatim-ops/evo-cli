// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Параллельные tools/call и батчи по HTTP (гонять с -race): общий реестр,
// обёртки политики и сессии не должны гоняться.
func TestQA_HTTPParallelCalls(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	sid := post(t, ts, initBody, nil).Header.Get("Mcp-Session-Id")
	bodies := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"vm_get","arguments":{"name":"x"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"disk_delete","arguments":{"disk_id":"d"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"k8s_kubeconfig","arguments":{"cluster_id":"c"}}}`,
		`[{"jsonrpc":"2.0","id":4,"method":"tools/list"},{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}]`,
		initBody,
	}
	var wg sync.WaitGroup
	errs := make(chan string, 200)
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(bodies[i%len(bodies)]))
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Content-Type", "application/json")
			if i%3 == 0 {
				req.Header.Set("Accept", "application/json, text/event-stream")
			}
			if i%2 == 0 {
				req.Header.Set("Mcp-Session-Id", sid)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				errs <- err.Error()
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Sprintf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// По HTTP без --allow-secret-export k8s_kubeconfig с токеном закрыт, но
// k8s_kubectl работает на kubeconfig с тем же токеном, и
// «kubectl config view --raw» его печатал — обход политики.
func TestQA_HTTPPolicyBlocksKubectlConfig(t *testing.T) {
	orig := binaryAvailable
	binaryAvailable = func(string) bool { return true }
	t.Cleanup(func() { binaryAvailable = orig })

	s := newTestServer()
	s.restrictForHTTP(false)
	for _, a := range []string{"config view --raw", "-n x config view --raw --minify"} {
		text, isErr := s.handlers["k8s_kubectl"](context.Background(), map[string]any{"cluster_id": "c", "args": a, "confirmed": true})
		if !isErr || !strings.Contains(text, "--allow-secret-export") {
			t.Errorf("%q over HTTP: %s", a, text)
		}
	}
	if text, _ := s.handlers["k8s_kubectl"](context.Background(), map[string]any{"cluster_id": "c", "args": "get pods"}); strings.Contains(text, "allow-secret-export") {
		t.Errorf("get pods refused by the policy: %s", text)
	}

	allowed := newTestServer()
	allowed.restrictForHTTP(true)
	if text, _ := allowed.handlers["k8s_kubectl"](context.Background(), map[string]any{"cluster_id": "c", "args": "config view --raw", "confirmed": true}); strings.Contains(text, "allow-secret-export") {
		t.Errorf("--allow-secret-export: %s", text)
	}
}
