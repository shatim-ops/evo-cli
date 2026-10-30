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
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// sleptCloud is IAM and a service of an operation an agent waits for
// (wait_seconds) while the machine evo runs on sleeps: a VM paused in the
// middle of the wait, the environment of the Grok run of 1.5.0-rc.1 that
// slept around 40 minutes in a Kafka create (owner 29.10). IAM hands out
// tok-1, tok-2, … for an hour each. The service answers the first poll
// with tok-1 as running; then the machine sleeps: the service refuses
// tok-1 from then on with 401 "Jwt is expired", while evo's clock — the
// clock of the paused VM, behind until it is synced — still holds it
// valid (G140-3). What became of the operation meanwhile is after: done,
// failed, or gone.
type sleptCloud struct {
	mu        sync.Mutex
	after     string // done, failed, gone
	keepToken bool   // the service still takes tok-1 after the sleep
	exchanges int
	polls     int // polls the service answered with a live token
	refused   int // polls refused with 401
}

func (c *sleptCloud) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Host == "iam.api.cloud.ru" {
		c.exchanges++
		_, _ = io.WriteString(w, `{"access_token":"tok-`+strconv.Itoa(c.exchanges)+`","expires_in":3600}`)
		return w.Result(), nil
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return w.Result(), nil
	}
	slept := c.polls > 0
	if slept && !c.keepToken && r.Header.Get("Authorization") == "Bearer tok-1" {
		c.refused++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":16,"message":"Jwt is expired"}`)
		return w.Result(), nil
	}
	c.polls++
	switch {
	case !slept:
		_, _ = io.WriteString(w, `{"id":"op-1","done":false,"status":"RUNNING","state":"RUNNING"}`)
	case c.after == "gone":
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":5,"message":"operation op-1 not found"}`)
	case c.after == "failed":
		_, _ = io.WriteString(w, `{"id":"op-1","done":true,"status":"FAILED","state":"FAILED","error":{"code":13,"message":"cluster failed"}}`)
	default:
		_, _ = io.WriteString(w, `{"id":"op-1","done":true,"status":"DONE","state":"DONE"}`)
	}
	return w.Result(), nil
}

// sleepWorldOut are the tools with wait_seconds this world does not
// drive, each with the reason.
var sleepWorldOut = map[string]string{
	"stack_status": "advances a stack of many steps, not one operation: its world is TestStackStatus_GoesOnAfterOneRefusedToken",
}

// sleepBreaksWait are the waits a 401 after the sleep still ends with an
// error, though the operation goes on and the next call would see it.
// Development 1.6 takes a line out when the wait gets a new token and goes
// on; a line of a wait that already goes on fails the test. A connection
// the sleep dropped is the transport's: a poll is a GET, repeated
// (httpretry, TestTransportErrorRetriedOnGet), on every client
// (TestNoHandBuiltTransport).
var sleepBreaksWait = map[string]string{}

// sleepWorld returns a registry whose engine signs in through IAM on c,
// as the CLI and evo mcp do (one provider per process).
func sleepWorld(t *testing.T, c *sleptCloud) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = c
	t.Cleanup(func() { http.DefaultTransport = old })
	oldDelay := opWaitFirstDelay
	opWaitFirstDelay = time.Millisecond
	t.Cleanup(func() { opWaitFirstDelay = oldDelay })
	cache := auth.FileCache{Path: filepath.Join(t.TempDir(), "token.json")}
	p := auth.NewIAMProvider(auth.Credentials{KeyID: "k", Secret: "s"}, nil, cache)
	// The CLI and evo mcp hold their provider for the process
	// (cli.tokenProvider); here only the engine closure holds it, and a
	// handler done with the registry leaves it to the collector mid-wait —
	// its weak entry gone, no provider renews the refused token.
	t.Cleanup(func() { runtime.KeepAlive(p) })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    p,
			Cloud:   provision.NewCloudRuAdapter(http.DefaultClient, "https://compute.api.cloud.ru"),
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// waitTools are the tools of the registry an agent waits with
// (wait_seconds in the schema).
func waitTools(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		if _, ok := props["wait_seconds"]; ok {
			names = append(names, tl.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no tool with wait_seconds: the world checks nothing")
	}
	return names
}

// sleepCall calls name of a sleep world on c as an agent waits: one call,
// wait_seconds well above what the world takes.
func sleepCall(t *testing.T, c *sleptCloud, name string) (string, bool) {
	t.Helper()
	tl, _ := sleepWorld(t, c).Lookup(name)
	return tl.Handler(context.Background(), map[string]any{"operation_id": "op-1", "wait_seconds": 30})
}

// finished reports whether an answer of a wait says the operation is done.
func finished(text string) bool {
	return strings.Contains(text, `"done": true`) || strings.Contains(text, "DONE")
}

// waitedTools are the tools with wait_seconds the world drives; a line of
// sleepWorldOut without a reason or without its tool fails the test.
func waitedTools(t *testing.T) []string {
	t.Helper()
	all := waitTools(t)
	seen := map[string]bool{}
	var driven []string
	for _, name := range all {
		seen[name] = true
		if why, out := sleepWorldOut[name]; out {
			if why == "" {
				t.Errorf("%s: in sleepWorldOut without a reason", name)
			}
			continue
		}
		driven = append(driven, name)
	}
	for name := range sleepWorldOut {
		if !seen[name] {
			t.Errorf("%s: in sleepWorldOut, but no tool with wait_seconds", name)
		}
	}
	for name := range sleepBreaksWait {
		if !seen[name] {
			t.Errorf("%s: in sleepBreaksWait, but no tool with wait_seconds", name)
		}
	}
	return driven
}

// The machine evo runs on sleeps in the middle of a wait (CLAUDE.md §6,
// axes 2 and 3; process backlog 1.6, priority 1): after it wakes, the
// service refuses the token evo still holds valid. The wait takes a new
// token and goes on to the operation's end, not to the 401 of one poll:
// an agent that reads a 401 at the end of the wait for a create does not
// know whether the cluster exists, and creates it again.
func TestWait_TakesANewTokenAfterTheSleep(t *testing.T) {
	for _, name := range waitedTools(t) {
		c := &sleptCloud{after: "done"}
		text, failed := sleepCall(t, c, name)
		if c.refused == 0 {
			t.Errorf("%s: the world did not reach the sleep (%d polls); it checks nothing — %s", name, c.polls, text)
			continue
		}
		broken := failed && strings.Contains(text, "401")
		if why, declared := sleepBreaksWait[name]; declared {
			if !broken {
				t.Errorf("%s: in sleepBreaksWait (%s), but the wait goes on after the sleep; take the line out", name, why)
			}
			continue
		}
		switch {
		case broken:
			t.Errorf("%s: the wait ends with the 401 of one poll after the machine slept (%d exchanges of the key), not with the operation's end — %s", name, c.exchanges, text)
		case failed || !finished(text):
			t.Errorf("%s: the operation finished while the machine slept, the answer does not say so — %s", name, text)
		}
	}
}

// What became of the operation while the machine slept is what the wait
// answers: failed — with the platform's error, not as done; gone — not
// found, not as done and not as still running.
func TestWait_SaysWhatBecameOfTheOperationInTheSleep(t *testing.T) {
	for _, name := range waitedTools(t) {
		for _, after := range []string{"done", "failed", "gone"} {
			c := &sleptCloud{after: after, keepToken: true}
			text, failed := sleepCall(t, c, name)
			if c.polls < 2 {
				t.Errorf("%s (%s): the world did not reach the sleep (%d polls); it checks nothing — %s", name, after, c.polls, text)
				continue
			}
			switch after {
			case "done":
				if failed || !finished(text) {
					t.Errorf("%s: the operation finished while the machine slept, the answer does not say so — %s", name, text)
				}
			case "failed":
				if !strings.Contains(text, "cluster failed") {
					t.Errorf("%s: the operation failed while the machine slept, the answer does not carry the platform's error — %s", name, text)
				}
			case "gone":
				if !failed || !strings.Contains(text, "not found") {
					t.Errorf("%s: the operation is gone after the sleep, the answer does not say not found — %s", name, text)
				}
			}
		}
	}
}
