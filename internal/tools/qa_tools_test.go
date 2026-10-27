// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

type staticToken struct{}

func (staticToken) Token(context.Context) (auth.Token, error) {
	return auth.Token{Value: "a.eyJzdWIiOiJ4In0.c", ExpiresAt: clock.Now().Add(time.Hour)}, nil
}

// recorder answers every Cloud.ru API call with one JSON body and records
// the method and path of each.
type recorder struct {
	mu   sync.Mutex
	reqs []string
	body string
}

func (rc *recorder) RoundTrip(r *http.Request) (*http.Response, error) {
	rc.record(r)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(rc.body)), Request: r}, nil
}

func (rc *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc.record(r)
	w.WriteHeader(http.StatusOK)
}

func (rc *recorder) record(r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
	}
	rc.mu.Lock()
	rc.reqs = append(rc.reqs, r.Method+" "+r.URL.Host+r.URL.Path)
	rc.mu.Unlock()
}

func (rc *recorder) take() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	out := rc.reqs
	rc.reqs = nil
	return out
}

// Ни один изменяющий инструмент без confirmed=true не шлёт в облако
// ничего, кроме чтений. s3_bucket_create создавал бакет без гейта
// (BACKLOG [04]).
func TestQA_MutatingToolsWriteNothingWithoutConfirmed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const id = "00000000-0000-0000-0000-000000000001"
	rec := &recorder{body: `{"id":"` + id + `","name":"x","items":[{"id":"` + id + `","name":"x"}],"done":true}`}
	old := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = old })
	s3srv := httptest.NewServer(rec)
	t.Cleanup(s3srv.Close)

	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: id, Region: id, ImageID: id, KeyID: "k", Secret: "s",
				S3: config.S3Config{Endpoint: s3srv.URL, AccessKey: "a", SecretKey: "b"}},
			Auth:     staticToken{},
			Cloud:    provision.NewCloudRuAdapter(&http.Client{Transport: rec}, ""),
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	})
	for _, tl := range r.Tools() {
		if !tl.Mutating {
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		args := map[string]any{}
		for k, p := range props {
			switch p.(map[string]any)["type"] {
			case "string":
				args[k] = id
			case "integer":
				args[k] = float64(1)
			case "boolean":
				args[k] = true
			case "array":
				args[k] = []any{"80"}
			case "object":
				args[k] = map[string]any{"name": "x"}
			}
		}
		delete(args, "confirmed")
		if tl.Name == "k8s_kubectl" {
			args["args"] = "delete pod x"
		}
		if tl.Name == "k8s_helm" {
			args["action"] = "install"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		text, _ := tl.Handler(ctx, args)
		cancel()
		for _, q := range rec.take() {
			if strings.HasPrefix(q, "GET ") || strings.HasPrefix(q, "HEAD ") {
				continue
			}
			t.Errorf("%s without confirmed sent %s (answer: %.200s)", tl.Name, q, text)
		}
	}
}

// Глагол kubectl определял гейт целиком: «rollout restart/undo/pause/
// resume» и «auth reconcile» шли без подтверждения, хотя описание
// инструмента называет rollout restart изменяющим.
func TestQA_KubectlWritingSubcommandsNeedConfirmation(t *testing.T) {
	r := newTestServer()
	for _, a := range []string{"rollout restart deploy/x", "rollout undo deploy/x", "rollout pause deploy/x",
		"rollout resume deploy/x", "rollout", "auth reconcile -f rbac.yaml", "delete pod x"} {
		got := callTool(t, r.handleK8sKubectl, map[string]any{"cluster_id": "c", "args": a})
		if got["status"] != "confirmation_required" {
			t.Errorf("%q ran without confirmation: %v", a, got)
		}
	}
	for _, a := range []string{"get pods -A", "rollout status deploy/x", "rollout history deploy/x", "auth can-i get pods", "logs x"} {
		got := callTool(t, r.handleK8sKubectl, map[string]any{"cluster_id": "c", "args": a})
		if got["status"] == "confirmation_required" {
			t.Errorf("read %q asks for confirmation", a)
		}
	}
}

// release и chart уходили в helm позиционными аргументами: значение с
// '-' в начале helm читал как флаг (--post-renderer запускает программу
// на хосте сервера).
func TestQA_HelmRejectsFlagsInPositionalArgs(t *testing.T) {
	r := newTestServer()
	for _, a := range []map[string]any{
		{"release": "--post-renderer=/bin/sh", "chart": "c"},
		{"release": "r", "chart": "--post-renderer=/bin/sh"},
		{"release": "r", "chart": "c", "namespace": "--kubeconfig=/etc/x"},
	} {
		a["cluster_id"], a["action"], a["confirmed"] = "c", "install", true
		got := callTool(t, r.handleK8sHelm, a)
		if msg, _ := got["error"].(string); !strings.Contains(msg, "must not start with '-'") {
			t.Errorf("%v: %v", a, got)
		}
	}
}
