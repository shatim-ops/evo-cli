// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The children start together, using separate registries and transports.
// Widen the gap after an empty snapshot: an unlocked check then open
// allows both children to decide to create before either intent exists.
func TestNetworkRepeat_ProcessChild(t *testing.T) {
	tool := os.Getenv("EVO_TEST_NETWORK_CREATE")
	if tool == "" {
		t.Skip("subprocess helper")
	}
	endpoint, err := url.Parse(os.Getenv("EVO_TEST_NETWORK_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	shared := os.Getenv("EVO_TEST_NETWORK_STATE")
	base := faultAPIRegistry(t, &faultAPI{})
	forward := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		local := req.Clone(req.Context())
		local.URL.Host, local.URL.Scheme = endpoint.Host, endpoint.Scheme
		local.Host = req.URL.Host
		resp, err := http.DefaultTransport.RoundTrip(local)
		if err == nil && req.Method == http.MethodPost {
			resp.Body.Close()
			return nil, errors.New("connection reset after create")
		}
		return resp, err
	})
	httpretry.DefaultBase = forward
	r := New(func(a map[string]any) (*engine.Engine, error) {
		eng, err := base.newEngine(a)
		if err == nil {
			eng.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: forward}, "")
			eng.StateDir = shared
		}
		return eng, err
	})
	r.SetStateDir(shared)
	readPending = func(dir string) []intent.Intent {
		records := intent.Pending(dir)
		if len(records) == 0 {
			time.Sleep(150 * time.Millisecond)
		}
		return records
	}
	ready, err := http.Get(endpoint.String() + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	ready.Body.Close()
	for _, c := range faultMatrixCases {
		if c.tool == tool {
			text, failed := r.handlers[tool](context.Background(), maps.Clone(c.args))
			if !failed {
				t.Fatalf("unexpected success: %s", text)
			}
			if strings.Contains(text, `"going_on"`) {
				t.Log("observed in-flight create")
			}
			return
		}
	}
	t.Fatalf("no fault world for %s", tool)
}

func TestNetworkRepeat_SimultaneousProcesses(t *testing.T) {
	for _, tool := range []string{"dns_record_create", "vpc_route_create", "snat_create"} {
		t.Run(tool, func(t *testing.T) {
			var arrivals atomic.Int32
			ready, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			api := &faultAPI{lag: 1000}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ready" {
					if arrivals.Add(1) == 2 {
						close(ready)
					}
					select {
					case <-ready:
					case <-ctx.Done():
					}
					return
				}
				r.URL.Host = r.Host
				result, err := api.RoundTrip(r)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				defer result.Body.Close()
				if r.Method == http.MethodPost {
					select {
					case <-release:
					case <-time.After(2 * time.Second):
					}
				}
				w.WriteHeader(result.StatusCode)
				_, _ = io.Copy(w, result.Body)
			}))
			defer server.Close()
			root := t.TempDir()
			type result struct {
				text string
				err  error
			}
			done := make(chan result, 2)
			for range 2 {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNetworkRepeat_ProcessChild$", "-test.v")
				cmd.Env = append(os.Environ(), "EVO_TEST_NETWORK_CREATE="+tool, "EVO_TEST_NETWORK_ENDPOINT="+server.URL, "EVO_TEST_NETWORK_STATE="+root)
				go func() { out, err := cmd.CombinedOutput(); done <- result{string(out), err} }()
			}
			first := <-done
			close(release)
			second := <-done
			if first.err != nil || second.err != nil || !strings.Contains(first.text, "observed in-flight create") || creates(api) != 1 || len(api.billed()) != 1 {
				t.Fatalf("writes=%d objects=%d; children: %v %s / %v %s", creates(api), len(api.billed()), first.err, first.text, second.err, second.text)
			}
		})
	}
}
