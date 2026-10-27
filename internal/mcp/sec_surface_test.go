// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// fakeResult makes a tool answer text without reaching the cloud. It wraps
// the registry handler first, so the HTTP policy applied later sits on top.
func fakeResult(s *Server, name, text string) {
	s.wrapHandler(name, func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) { return text, false }
	})
}

func call(s *Server, name string, args map[string]any) (string, bool) {
	return s.handlers[name](context.Background(), args)
}

// BACKLOG [08]: secret_value answered a decrypted secret over HTTP even
// without --allow-secret-export.
func TestSec_SecretValueNeedsExportFlagOverHTTP(t *testing.T) {
	args := map[string]any{"path": "app/db"}

	s := newTestServer()
	s.restrictForHTTP(false)
	if text, isErr := call(s, "secret_value", args); !isErr || !strings.Contains(text, "--allow-secret-export") {
		t.Errorf("HTTP without the flag: %s", text)
	}
	for _, export := range []bool{true} {
		s := newTestServer()
		s.restrictForHTTP(export)
		if text, _ := call(s, "secret_value", args); strings.Contains(text, "allow-secret-export") {
			t.Errorf("HTTP with --allow-secret-export: %s", text)
		}
	}
	stdio := newTestServer()
	if text, _ := call(stdio, "secret_value", args); strings.Contains(text, "allow-secret-export") {
		t.Errorf("stdio: %s", text)
	}
}

// Results carrying credentials (deploy_apply's generated VM password, a
// cluster object with a password from pg/redis/kafka *_get) are masked over
// HTTP without the flag, and only there.
func TestSec_CredentialFieldsMaskedOverHTTP(t *testing.T) {
	results := map[string]string{
		"deploy_apply": `{"vm_id":"v","password":"Gen3rated!pw","ssh":"ssh user1@1.2.3.4"}`,
		"redis_get":    `{"id":"r","name":"c","password":"redis-pw","users":[{"name":"u","Password":"x"}],"endpoint":{"secret_key":"k"}}`,
		"audit_events": `{"events":[],"next_page_token":"keep-me"}`,
	}
	newServer := func() *Server {
		s := newTestServer()
		for name, text := range results {
			fakeResult(s, name, text)
		}
		return s
	}

	s := newServer()
	s.restrictForHTTP(false)
	for name := range results {
		text, _ := call(s, name, map[string]any{})
		for _, secret := range []string{"Gen3rated!pw", "redis-pw", `"x"`, `"k"`} {
			if strings.Contains(text, secret) {
				t.Errorf("%s over HTTP leaks %s: %s", name, secret, text)
			}
		}
		if !json.Valid([]byte(text)) {
			t.Errorf("%s: masked result is not JSON: %s", name, text)
		}
	}
	if text, _ := call(s, "audit_events", nil); !strings.Contains(text, "keep-me") {
		t.Errorf("page token masked: %s", text)
	}
	// REQ-9: the masked result names what was masked (redacted), and only
	// a masked one does.
	for name, want := range map[string][]string{
		"deploy_apply": {"password"},
		"redis_get":    {"endpoint.secret_key", "password", "users[].Password"},
		"audit_events": nil,
	} {
		text, _ := call(s, name, nil)
		if got := redactedOf(t, text); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: redacted %v, want %v: %s", name, got, want, text)
		}
	}
	if text, _ := call(s, "deploy_apply", nil); !strings.Contains(text, "ssh user1@1.2.3.4") {
		t.Errorf("non-secret fields lost: %s", text)
	}

	for _, srv := range []*Server{func() *Server { s := newServer(); s.restrictForHTTP(true); return s }(), newServer()} {
		if text, _ := call(srv, "deploy_apply", nil); !strings.Contains(text, "Gen3rated!pw") {
			t.Errorf("stdio / --allow-secret-export must return the password once: %s", text)
		}
	}
}

// deploy reads compose_file on the server: over HTTP only inside the
// working directory.
func TestSec_DeployComposeFileConfinedOverHTTP(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("docker-compose.yml", []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.yml")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, "link.yml"); err != nil {
		t.Skip("no symlinks here:", err)
	}

	s := newTestServer()
	s.restrictForHTTP(true) // not a secret-export matter
	for _, p := range []string{outside, "../x.yml", "link.yml", "sub/../../x.yml"} {
		text, isErr := call(s, "deploy", map[string]any{"compose_file": p})
		if !isErr || !strings.Contains(text, "outside the server's working directory") {
			t.Errorf("compose_file %q over HTTP: %s", p, text)
		}
	}
	for _, p := range []string{"docker-compose.yml", filepath.Join(dir, "docker-compose.yml"), "sub/missing.yml"} {
		if text, _ := call(s, "deploy", map[string]any{"compose_file": p}); strings.Contains(text, "outside") {
			t.Errorf("compose_file %q refused: %s", p, text)
		}
	}
	stdio := newTestServer()
	if text, _ := call(stdio, "deploy", map[string]any{"compose_file": outside}); strings.Contains(text, "outside") {
		t.Errorf("stdio restricted: %s", text)
	}
}

func TestSec_KubectlHTTPArgs(t *testing.T) {
	denied := []string{
		"get pods --kubeconfig=/root/.kube/config",
		"get pods --kubeconfig /root/.kube/config",
		"get pods -s https://example.com",
		"get pods --server=https://example.com",
		"get pods --token=x",
		"get pods --insecure-skip-tls-verify",
		"cp ns/pod:/etc/hosts /tmp/x",
		"port-forward svc/x 8080:80",
		"proxy",
		"edit deploy/x",
		"kustomize .",
		"myplugin do",
		"-n kube-system get pods",
		"apply -f /etc/x.yaml",
		"apply -f https://example.com/x.yaml",
		"delete -f/tmp/x.yaml",
		"apply -k .",
		"create configmap c --from-file=/etc/hosts",
		"create secret generic s --from-env-file=.env",
		"get pods -o go-template-file=/etc/x",
		"get pods -ojsonpath-file=/etc/x",
		"get pods --log-file=/tmp/x",
		"cluster-info dump --output-directory=/tmp/x",
		"get pods --profile=cpu --profile-output=/tmp/x",
		// secret material without --allow-secret-export
		"get secret db -o yaml",
		"get secrets -ojson",
		"get secret/db --output=jsonpath={.data}",
		"get cm,secrets -o yaml",
		"get --raw /api/v1/namespaces/default/secrets",
		"create token default",
		// A flag before the command: kubectl reads "get" as the value of
		// -n and runs cp, proxy, kustomize (security audit 19.10, SEC-1).
		"-n get cp get/pod:/x /home/u/.bashrc",
		"-n get proxy --address=0.0.0.0 --port=8001",
		"--namespace get kustomize /",
		"--context=x get pods",
		"-- get pods",
		// A flag value before the subcommand.
		"create -n default token sa",
		"apply -n default view-last-applied secret/x",
		// describe prints last-applied-configuration (break r4, R4-1).
		"describe secret db",
	}
	for _, a := range denied {
		if err := kubectlCheck(strings.Fields(a), false, httpWording); err == nil {
			t.Errorf("allowed over HTTP: kubectl %s", a)
		}
	}
	allowed := []string{
		"get pods -A",
		"get pods -n kube-system -o wide",
		"get secrets",
		"get secret db -o name",
		"describe pod db",
		"logs deploy/x -c app --tail 50",
		"rollout status deploy/x",
		"exec pod-0 -- cat -f /x",
		"scale deploy/x --replicas=2",
		"create secret generic s --from-literal=a=b",
		"get pods -o yaml",
	}
	for _, a := range allowed {
		if err := kubectlCheck(strings.Fields(a), false, httpWording); err != nil {
			t.Errorf("kubectl %s refused: %v", a, err)
		}
	}
	// --allow-secret-export opens secret reads, not host access.
	if err := kubectlCheck(strings.Fields("get secret db -o yaml"), true, httpWording); err != nil {
		t.Errorf("--allow-secret-export: %v", err)
	}
	if err := kubectlCheck(strings.Fields("get pods --kubeconfig=/x"), true, httpWording); err == nil {
		t.Error("--allow-secret-export opened --kubeconfig")
	}
}

// The policy sits in front of the handler (which would need kubectl and a
// cluster), and stdio keeps the escape hatch as it was.
func TestSec_KubectlPolicyWiring(t *testing.T) {
	orig := binaryAvailable
	binaryAvailable = func(string) bool { return true }
	t.Cleanup(func() { binaryAvailable = orig })

	args := map[string]any{"cluster_id": "c", "args": "cp ns/p:/x /tmp/x", "confirmed": true}
	s := newTestServer()
	s.restrictForHTTP(true)
	if text, isErr := call(s, "k8s_kubectl", args); !isErr || !strings.Contains(text, "not a built-in kubectl command") {
		t.Errorf("HTTP: %s", text)
	}
	if text, _ := call(newTestServer(), "k8s_kubectl", args); strings.Contains(text, "built-in") {
		t.Errorf("stdio: %s", text)
	}
}

func TestSec_HelmHTTPArgs(t *testing.T) {
	orig := lookupIP
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "charts.example.com", "registry.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "internal.example.com":
			return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.5")}, nil
		case "metadata.example.com":
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		}
		return nil, fmt.Errorf("no such host")
	}
	t.Cleanup(func() { lookupIP = orig })
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("bitnami/local", 0o700); err != nil {
		t.Fatal(err)
	}

	denied := []map[string]any{
		{"chart": "/srv/charts/app"},
		{"chart": "./app"},
		{"chart": "../app"},
		{"chart": "~/app"},
		{"chart": "bitnami/local"}, // exists as a directory: helm would take it
		{"chart": "file:///etc"},
		{"chart": "oci://127.0.0.1:5000/app"},
		{"chart": "https://localhost/app.tgz"},
		{"chart": "http://charts.example.com/app.tgz"},
		{"chart": "app", "repo_url": "http://charts.example.com"},
		{"chart": "repo/app", "repo_url": "https://internal.example.com"},
		{"chart": "repo/app", "repo_url": "https://metadata.example.com"},
		{"chart": "repo/app", "repo_url": "https://169.254.169.254/"},
		{"chart": "repo/app", "repo_url": "https://100.64.0.1/"},
		{"chart": "repo/app", "repo_url": "https://[::1]/"},
		{"chart": "repo/app", "repo_url": "https://nowhere.invalid/"},
	}
	for _, a := range denied {
		if err := helmHTTPCheck(context.Background(), a); err == nil {
			t.Errorf("allowed over HTTP: %v", a)
		}
	}
	allowed := []map[string]any{
		{"chart": "bitnami/nginx", "repo_url": "https://charts.example.com"},
		{"chart": "bitnami/nginx"},
		{"chart": "oci://registry.example.com/charts/app"},
		{"action": "list"},
	}
	for _, a := range allowed {
		if err := helmHTTPCheck(context.Background(), a); err != nil {
			t.Errorf("%v refused: %v", a, err)
		}
	}
}

// One POST held a batch of tools/list requests: ~40 KB of answer per
// 30-byte message, all kept in memory (16 MiB body ≈ 20 GB of response).
func TestSec_HTTPBatchSizeLimited(t *testing.T) {
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	msgs := make([]string, httpMaxBatch+1)
	for i := range msgs {
		msgs[i] = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, i)
	}
	resp := post(t, ts, "["+strings.Join(msgs, ",")+"]", nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("batch of %d: status %d", len(msgs), resp.StatusCode)
	}
	resp = post(t, ts, "["+strings.Join(msgs[:httpMaxBatch], ",")+"]", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch of %d: status %d", httpMaxBatch, resp.StatusCode)
	}
}

// Tool calls over HTTP are bounded: the ones over the limit are refused at
// once instead of piling up goroutines and cloud requests (H8).
func TestSec_HTTPConcurrentToolCallsLimited(t *testing.T) {
	s := newTestServer()
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(httpMaxToolCalls)
	s.wrapHandler("vm_list", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			select {
			case <-release:
				return `{"vms":[]}`, false
			default:
			}
			started.Done()
			<-release
			return `{"vms":[]}`, false
		}
	})
	h := NewHTTPHandler(s, HTTPOptions{BearerToken: testToken})
	req := func(id int) *rpcRequest {
		return &rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprint(id)), Method: "tools/call", Params: json.RawMessage(`{"name":"vm_list"}`)}
	}
	var done sync.WaitGroup
	for i := range httpMaxToolCalls {
		done.Add(1)
		go func() { defer done.Done(); h.dispatch(context.Background(), "", req(i)) }()
	}
	started.Wait()

	resp, _ := h.dispatch(context.Background(), "", req(999))
	data, _ := json.Marshal(resp)
	if !strings.Contains(string(data), "server busy") {
		t.Errorf("call over the limit: %s", data)
	}
	// Лимит — самого сервера, не Cloud.ru: SERVER_BUSY, а не
	// API_RATE_LIMITED (релизный аудит, L-03).
	if !strings.Contains(string(data), `\"reason\": \"SERVER_BUSY\"`) || strings.Contains(string(data), "API_RATE_LIMITED") {
		t.Errorf("call over the limit: reason is not SERVER_BUSY: %s", data)
	}
	close(release)
	done.Wait()

	resp, _ = h.dispatch(context.Background(), "", &rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(`{"name":"vm_list"}`)})
	if data, _ := json.Marshal(resp); strings.Contains(string(data), "busy") {
		t.Errorf("after the calls finished: %s", data)
	}
}
