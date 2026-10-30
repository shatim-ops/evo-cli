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
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// expiringCloud is IAM and mk8s of a long session: IAM hands out tok-1,
// tok-2, … for an hour each; mk8s refuses tok-1 with 401 "Jwt is expired",
// as a service does with a token whose expiry evo misjudged (a paused VM,
// G140-3), and accepts any later one.
type expiringCloud struct {
	mu        sync.Mutex
	exchanges int
}

func (c *expiringCloud) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	c.mu.Lock()
	defer c.mu.Unlock()
	switch r.URL.Host {
	case "iam.api.cloud.ru":
		c.exchanges++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok-`+string(rune('0'+c.exchanges))+`","expires_in":3600}`)
	case "mk8s.api.cloud.ru":
		if r.Header.Get("Authorization") == "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":16,"message":"Jwt is expired"}`)
			break
		}
		_, _ = io.WriteString(w, `{"clusters":[]}`)
	default:
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "unexpected host "+r.URL.Host)
	}
	return w.Result(), nil
}

// G140-3, plan 1.4.1 item 1: a service that refuses the token as expired
// does not leave the session holding it — from memory or the file, until
// its expiry by evo's own clock (the Grok run of 1.4.0: 401 "Jwt is
// expired" 43 times in a row in one MCP session). Since 1.6 (the machine
// that slept, process backlog 1.6, priority 1) the refused call itself
// goes once more with the token the provider exchanges anew, and the next
// call of the session (one provider, as tokenProvider keeps per process
// for evo mcp) takes that token without another exchange.
func TestTokenRefusedAsExpired_NextCallExchangesAnew(t *testing.T) {
	c := &expiringCloud{}
	old := http.DefaultTransport
	http.DefaultTransport = c
	t.Cleanup(func() { http.DefaultTransport = old })
	cache := auth.FileCache{Path: filepath.Join(t.TempDir(), "token.json")}
	p := auth.NewIAMProvider(auth.Credentials{KeyID: "k", Secret: "s"}, nil, cache)
	// Held for the test, as cli.tokenProvider holds it for the process:
	// the renewal of a refused token finds live providers only (weak).
	t.Cleanup(func() { runtime.KeepAlive(p) })
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    p,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
	first, failed := r.handlers["k8s_list"](context.Background(), map[string]any{})
	if failed || c.exchanges != 2 {
		t.Fatalf("a call refused with 401 \"Jwt is expired\" should go once more with a new token (%d exchanges of the key): %s", c.exchanges, first)
	}
	second, failed := r.handlers["k8s_list"](context.Background(), map[string]any{})
	if failed || c.exchanges != 2 {
		t.Errorf("the call after a 401 \"Jwt is expired\" (%d exchanges of the key): evo holds the refused token, or exchanges the key for each call — %s", c.exchanges, second)
	}
}
