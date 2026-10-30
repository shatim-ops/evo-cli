// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// refusingVPC is IAM and the VPC service after the machine slept: IAM
// hands out tok-1, tok-2, …; the service lists no VPCs and refuses the
// create sent with a token of refused (401 "Jwt is expired") — a 401 is
// refused before the request is applied — and makes one VPC for any other.
type refusingVPC struct {
	mu        sync.Mutex
	refuseAll bool
	exchanges int
	created   int
	posted    []string // the token of each create
}

func (c *refusingVPC) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Host == "iam.api.cloud.ru":
		c.exchanges++
		_, _ = io.WriteString(w, `{"access_token":"tok-`+strconv.Itoa(c.exchanges)+`","expires_in":3600}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/vpcs":
		_, _ = io.WriteString(w, `{"vpcs":[]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/vpcs":
		c.posted = append(c.posted, r.Header.Get("Authorization"))
		if c.refuseAll || r.Header.Get("Authorization") == "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":16,"message":"Jwt is expired"}`)
			break
		}
		c.created++
		_, _ = io.WriteString(w, `{"id":"op-1","resourceId":"00000000-0000-4000-8000-0000000000a1","done":false}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no such path"}`)
	}
	return w.Result(), nil
}

func refusingVPCRegistry(t *testing.T, c *refusingVPC) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = c
	t.Cleanup(func() { http.DefaultTransport = old })
	cache := auth.FileCache{Path: filepath.Join(t.TempDir(), "token.json")}
	p := auth.NewIAMProvider(auth.Credentials{KeyID: "k", Secret: "s"}, nil, cache)
	// Held for the test, as cli.tokenProvider holds it for the process:
	// the renewal of a refused token finds live providers only (weak).
	t.Cleanup(func() { runtime.KeepAlive(p) })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    p,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// A create the service refuses once after the machine slept goes once more
// with a new token and makes one VPC, not two (CLAUDE.md §6, axis 3): the
// refused request was not applied. Refused again, the token is the key's:
// the answer is as before 1.6 (the VPC client's reason of a 401), nothing
// is made, and the create was sent twice — once more, with another token,
// never a third time. The exit code of a refused key on the compute path
// (AUTH_EXPIRED, 2) is TestDeploy_AuthExpiredDuringPollExitsAuth.
func TestCreate_OneRefusedTokenMakesOneResource(t *testing.T) {
	args := map[string]any{"name": "net", "confirmed": true}
	c := &refusingVPC{}
	text, failed := refusingVPCRegistry(t, c).handlers["vpc_create"](context.Background(), args)
	if failed || c.created != 1 || c.exchanges != 2 {
		t.Errorf("one refused token: %d VPCs made, %d exchanges of the key, want 1 and 2 — %s", c.created, c.exchanges, text)
	}

	c = &refusingVPC{refuseAll: true}
	text, failed = refusingVPCRegistry(t, c).handlers["vpc_create"](context.Background(), args)
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if !failed || m["reason"] != clierrors.CodeAPIError || c.created != 0 {
		t.Errorf("the key refused (two 401 in a row): want the 401 of the VPC client as before, nothing made; %d made — %s", c.created, text)
	}
	if len(c.posted) != 2 || c.posted[0] == c.posted[1] {
		t.Errorf("the key refused: the create went %d times with %q, want twice, the second with a renewed token", len(c.posted), c.posted)
	}
}
