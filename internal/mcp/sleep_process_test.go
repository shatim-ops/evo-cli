// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// sleepChildEnv names the fake cloud of a child evo mcp process: the test
// binary run again as the server, so that the process the test stops with
// SIGSTOP is a real one — its stdio, its timers, its connections.
const sleepChildEnv = "EVO_TEST_SLEEP_CHILD_CLOUD"

// toCloud sends every request of the child to the fake cloud of the
// parent, keeping the host the child meant in Host.
type toCloud struct{ addr string }

func (c toCloud) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Scheme = "http"
	out.URL.Host = c.addr
	out.Host = r.URL.Host
	return (&http.Transport{}).RoundTrip(out)
}

// TestSleepChild is the child: evo mcp on stdin and stdout, signed in
// through IAM, as the CLI wires it (one provider per process). It does
// nothing unless the parent started it.
func TestSleepChild(t *testing.T) {
	addr := os.Getenv(sleepChildEnv)
	if addr == "" {
		t.Skip("the child of TestStdio_CallSurvivesTheProcessStopped")
	}
	http.DefaultTransport = toCloud{addr: addr}
	p := auth.NewIAMProvider(auth.Credentials{KeyID: "k", Secret: "s"}, nil, auth.FileCache{Path: filepath.Join(t.TempDir(), "token.json")})
	// Held for the test, as cli.tokenProvider holds it for the process:
	// the renewal of a refused token finds live providers only (weak).
	t.Cleanup(func() { runtime.KeepAlive(p) })
	s := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config:  config.Config{ProjectID: "00000000-0000-0000-0000-0000000000f1", KeyID: "k", Secret: "s"},
			Auth:    p,
			Cloud:   provision.NewCloudRuAdapter(http.DefaultClient, "https://compute.api.cloud.ru"),
			Printer: output.NewWithWriter(io.Discard, output.ModeText, false),
		}, nil
	}, "test")
	if err := s.RunContext(context.Background(), os.Stdin, os.Stdout); err != nil {
		t.Fatal(err)
	}
}

// stoppedCloud is IAM and the PostgreSQL operation the child waits for.
// The first poll answers running; then the parent stops the child: once
// it has read that answer (asleep between polls) or with the next poll in
// flight, unanswered until the child goes on. While it is stopped the
// operation finishes and, when refuse, the service starts refusing tok-1
// with 401 "Jwt is expired" (G140-3).
type stoppedCloud struct {
	mu        sync.Mutex
	inFlight  bool
	refuse    bool
	child     *os.Process
	exchanges int
	polls     int
	stopped   bool
	resumed   chan struct{}
}

// stopFor is how long the child stays stopped: longer than the first poll
// interval of the wait (opWaitFirstDelay, 2 s), so its timer is past due
// when the process goes on, as after a sleep of minutes.
const stopFor = 3 * time.Second

// read is how long the child takes to read an answer: the stop after the
// first poll lands in the wait between polls, not on the answer.
const read = 200 * time.Millisecond

func (c *stoppedCloud) stop(t *testing.T) {
	if err := c.child.Signal(syscall.SIGSTOP); err != nil {
		t.Errorf("SIGSTOP: %v", err)
	}
	c.stopped = true
	go func() {
		time.Sleep(stopFor) // the sleep of the machine
		if err := c.child.Signal(syscall.SIGCONT); err != nil {
			t.Errorf("SIGCONT: %v", err)
		}
		close(c.resumed)
	}()
}

func (c *stoppedCloud) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		c.mu.Lock()
		if r.Host == "iam.api.cloud.ru" {
			c.exchanges++
			n := c.exchanges
			c.mu.Unlock()
			_, _ = io.WriteString(w, `{"access_token":"tok-`+strconv.Itoa(n)+`","expires_in":3600}`)
			return
		}
		if c.polls == 0 {
			c.polls = 1
			c.mu.Unlock()
			_, _ = io.WriteString(w, `{"id":"op-1","done":false,"status":"RUNNING"}`)
			if !c.inFlight {
				w.(http.Flusher).Flush()
				go func() {
					time.Sleep(read) // the child reads the answer and sleeps until its next poll
					c.mu.Lock()
					c.stop(t)
					c.mu.Unlock()
				}()
			}
			return
		}
		if c.inFlight && !c.stopped {
			c.stop(t)
			c.mu.Unlock()
			<-c.resumed // answered while the child is stopped, read when it goes on
			_, _ = io.WriteString(w, `{"id":"op-1","done":true,"status":"DONE"}`)
			return
		}
		refused := c.refuse && r.Header.Get("Authorization") == "Bearer tok-1"
		c.mu.Unlock()
		if refused {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":16,"message":"Jwt is expired"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"op-1","done":true,"status":"DONE"}`)
	})
}

// The process of evo mcp is stopped in the middle of a wait (SIGSTOP and
// SIGCONT: a VM paused, a laptop lid closed — the Grok run of 1.5.0-rc.1
// slept about 40 minutes in a Kafka create; process backlog 1.6, priority
// 1, step «b»). After it goes on, the tools/call it was in answers: it does
// not hang on a timer or a connection of before the stop, and the answer
// is the operation's end. With the token refused after the stop the
// answer is still there and honest — the 401 with the way out — and the
// wait that takes a new token instead is TestWait_TakesANewTokenAfterTheSleep
// of internal/tools (sleepBreaksWait).
func TestStdio_CallSurvivesTheProcessStopped(t *testing.T) {
	if os.Getenv(sleepChildEnv) != "" {
		t.Skip("in the child")
	}
	cases := []struct {
		name     string
		inFlight bool
		refuse   bool
	}{
		{name: "asleep between polls"},
		{name: "a poll in flight", inFlight: true},
		{name: "token refused after the stop", refuse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &stoppedCloud{inFlight: tc.inFlight, refuse: tc.refuse, resumed: make(chan struct{})}
			srv := httptest.NewServer(c.handler(t))
			defer srv.Close()

			cmd := exec.Command(os.Args[0], "-test.run=^TestSleepChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), sleepChildEnv+"="+strings.TrimPrefix(srv.URL, "http://"))
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			c.child = cmd.Process
			c.mu.Unlock()
			defer func() {
				_ = cmd.Process.Signal(syscall.SIGCONT)
				_ = stdin.Close()
				_ = cmd.Wait()
			}()

			call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pg_operation","arguments":{"operation_id":"op-1","wait_seconds":60}}}` + "\n"
			if _, err := io.WriteString(stdin, call); err != nil {
				t.Fatal(err)
			}
			answers := make(chan string, 1)
			go func() {
				sc := bufio.NewScanner(stdout)
				sc.Buffer(make([]byte, 1<<20), 1<<20)
				for sc.Scan() {
					if strings.Contains(sc.Text(), `"id":1`) {
						answers <- sc.Text()
						return
					}
				}
				close(answers)
			}()
			var line string
			select {
			case line = <-answers:
			// wall-clock: the call waits up to wait_seconds (60 s); a hung
			// call would hold it to the end, an answer comes seconds after
			// SIGCONT.
			case <-time.After(stopFor + 20*time.Second):
				t.Fatalf("tools/call does not answer after the process went on (stopped: %v); stderr: %s", c.stopped, stderr.String())
			}
			if line == "" {
				t.Fatalf("the child ended without an answer; stderr: %s", stderr.String())
			}
			if !c.stopped {
				t.Fatalf("the world did not stop the child; it checks nothing — %s", line)
			}
			var resp struct {
				Result struct {
					IsError bool `json:"isError"`
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &resp); err != nil || len(resp.Result.Content) == 0 {
				t.Fatalf("not an answer of tools/call: %v — %s", err, line)
			}
			text := resp.Result.Content[0].Text
			switch {
			case tc.refuse && resp.Result.IsError:
				if !strings.Contains(text, "401") || !strings.Contains(text, "repeat") {
					t.Errorf("the 401 after the stop does not say what it is and the way out — %s", text)
				}
			case resp.Result.IsError || !strings.Contains(text, `"done": true`):
				t.Errorf("the operation finished while the process was stopped, the answer does not say so — %s", text)
			}
		})
	}
}
