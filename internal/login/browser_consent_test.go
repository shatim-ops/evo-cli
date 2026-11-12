// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package login

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Gate rc.1 of 1.8, Grok F1 (2 of 2): in a real browser «Выдать доступ»
// and «Отмена» of the consent page were refused 403, while every world
// test passed — the world's browser is Go, and it sends the Origin a test
// gives it, not the one the page's referrer policy makes a browser send.
// Here the consent page is the loopback's, in headless Chromium: the
// browser follows the provider's redirect, loads the page and presses the
// button as a person does. «Выдать доступ» ends the login with the key;
// «Отмена» ends it cancelled. A refused form leaves the login waiting.
//
// Chromium: $EVO_TEST_CHROME, or one on the PATH or of Playwright. Without
// one the test is skipped, but not on Linux in CI (GitHub's ubuntu image
// has Google Chrome): there the class «works in the world, not in a
// browser» must stay held (release plan 1.9, «мир страниц входа в
// браузере», #1206).
func TestConsent_InABrowserTheFormIsAccepted(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		if runtime.GOOS == "linux" && os.Getenv("CI") != "" {
			t.Fatal("no Chromium on the Linux runner: the consent page is held in a browser here (set EVO_TEST_CHROME)")
		}
		t.Skip("no Chromium: set EVO_TEST_CHROME")
	}
	for _, c := range []struct {
		name   string
		button string // the selector of the button the person presses
		want   func(t *testing.T, w *loginworld.World, err error)
	}{
		{"grant", `form button.primary`, func(t *testing.T, w *loginworld.World, err error) {
			if err != nil || len(w.Keys()) != 1 {
				t.Errorf("«Выдать доступ» in the browser: err %v, keys %d — the form was refused", err, len(w.Keys()))
			}
		}},
		{"cancel", `form button[formaction="/cancel"]`, func(t *testing.T, w *loginworld.World, err error) {
			var ce *clierrors.CLIError
			if !errors.As(err, &ce) || ce.Code != clierrors.CodeUsage || ce.Message != eCancelled() || len(w.Keys()) != 0 {
				t.Errorf("«Отмена» in the browser: err %v, keys %d — the form was refused", err, len(w.Keys()))
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, loginworld.Variant{})
			br := startChrome(t, chrome)
			var out bytes.Buffer
			opened := make(chan string, 1)
			cfg := worldConfig(w, &out, func(authURL string) error {
				// The provider's part stays the world's: its redirect to
				// the loopback is what the browser opens.
				resp, err := w.Client().Get(authURL)
				if err != nil {
					return err
				}
				resp.Body.Close()
				opened <- resp.Header.Get("Location")
				return nil
			})
			cfg.Wait = 10 * time.Second
			type result struct{ err error }
			done := make(chan result, 1)
			go func() {
				_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
				done <- result{err}
			}()
			var callback string
			select {
			case callback = <-opened:
			case r := <-done:
				t.Fatalf("the login ended before the browser: %v\n%s", r.err, out.String())
			}
			br.navigate(t, callback)
			// The project: the only one is chosen; otherwise the first
			// that can be — as a person picks one.
			br.eval(t, `(()=>{const s=document.querySelector('#project');if(s&&!s.value){const o=[...s.options].find(o=>o.value&&!o.disabled);if(o){s.value=o.value;s.dispatchEvent(new Event('change',{bubbles:true}))}}})()`)
			got := br.eval(t, fmt.Sprintf(`(()=>{const b=document.querySelector(%q);if(!b)return 'no button';b.click();return 'clicked '+b.textContent.trim()})()`, c.button))
			if got == "no button" {
				t.Fatalf("no button %s on the consent page:\n%.800s", c.button, br.eval(t, `document.body.innerText`))
			}
			select {
			case r := <-done:
				c.want(t, w, r.err)
			case <-time.After(cfg.Wait + 5*time.Second):
				t.Fatalf("%s: the login still waits — the browser's form was refused:\n%.400s", got, br.eval(t, `document.body.innerText`))
			}
		})
	}
}

// findChrome is a Chromium of $EVO_TEST_CHROME, the PATH or Playwright.
func findChrome() string {
	if p := os.Getenv("EVO_TEST_CHROME"); p != "" {
		return p
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	for _, g := range []string{"/opt/pw-browsers/chromium-*/chrome-linux/chrome", "/opt/pw-browsers/chromium-*/chrome-linux64/chrome"} {
		if m, _ := filepath.Glob(g); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	return ""
}

// cdp is Chromium over --remote-debugging-pipe: JSON messages, each ended
// by NUL, commands on fd 3 of the browser, answers on its fd 4. One page,
// one session; enough to navigate, wait for the load and evaluate.
type cdp struct {
	in      io.Writer
	mu      sync.Mutex
	id      int
	waiting map[int]chan json.RawMessage
	events  chan string
	session string
	// log is the browser's stderr and exited is closed when it ends: a
	// browser that does not answer says why (three red ci on ubuntu,
	// 11–12.11, «no answer of the browser» with nothing else).
	log    *lockedBuf
	exited chan struct{}
	err    error
}

// lockedBuf is a buffer the browser's stderr is copied into while the test
// reads it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// state is what is known of the browser when it does not answer: whether
// it ended, and the end of its stderr.
func (c *cdp) state() string {
	st := "still running"
	select {
	case <-c.exited:
		st = fmt.Sprintf("exited: %v", c.err)
	default:
	}
	l := c.log.String()
	if len(l) > 1500 {
		l = "…" + l[len(l)-1500:]
	}
	return fmt.Sprintf("browser %s; its stderr:\n%s", st, l)
}

func startChrome(t *testing.T, bin string) *cdp {
	t.Helper()
	cmdR, cmdW, err := os.Pipe() // evo → browser (its fd 3)
	if err != nil {
		t.Fatal(err)
	}
	resR, resW, err := os.Pipe() // browser → evo (its fd 4)
	if err != nil {
		t.Fatal(err)
	}
	// No network of the browser's own at start (updates of components,
	// sync, field trials, metrics): the world is the loopback's, and a
	// browser waiting on a host the runner cannot reach would not answer.
	cmd := exec.Command(bin, "--headless=new", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--no-default-browser-check", "--disable-background-networking", "--disable-component-update",
		"--disable-sync", "--disable-default-apps", "--disable-domain-reliability", "--metrics-recording-only",
		"--no-pings", "--disable-features=Translate,OptimizationHints,MediaRouter",
		"--remote-debugging-pipe", "--user-data-dir="+t.TempDir(), "about:blank")
	cmd.ExtraFiles = []*os.File{cmdR, resW}
	log := &lockedBuf{}
	cmd.Stderr = log
	// Its own process group: the cleanup ends the browser's helpers too,
	// before its profile directory is removed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	cmdR.Close()
	resW.Close()
	c := &cdp{in: cmdW, waiting: map[int]chan json.RawMessage{}, events: make(chan string, 64), log: log, exited: make(chan struct{})}
	go func() {
		c.err = cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		cmdW.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-c.exited
	})
	go c.read(resR)
	var target struct{ TargetID string }
	c.call(t, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &target)
	var att struct{ SessionID string }
	c.call(t, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &att)
	c.session = att.SessionID
	c.call(t, c.session, "Page.enable", nil, nil)
	return c
}

func (c *cdp) read(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		msg, err := br.ReadBytes(0)
		if err != nil {
			return
		}
		var m struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(msg[:len(msg)-1], &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if ch != nil {
				if len(m.Error) > 0 {
					ch <- append(json.RawMessage(`{"__error":`), append(m.Error, '}')...)
				} else {
					ch <- m.Result
				}
			}
			continue
		}
		select {
		case c.events <- m.Method:
		default:
		}
	}
}

func (c *cdp) call(t *testing.T, session, method string, params any, out any) {
	t.Helper()
	c.mu.Lock()
	c.id++
	id := c.id
	ch := make(chan json.RawMessage, 1)
	c.waiting[id] = ch
	c.mu.Unlock()
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	if _, err := c.in.Write(append(b, 0)); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	select {
	case res := <-ch:
		var e struct {
			Error json.RawMessage `json:"__error"`
		}
		if json.Unmarshal(res, &e) == nil && len(e.Error) > 0 {
			t.Fatalf("%s: %s", method, e.Error)
		}
		if out != nil {
			if err := json.Unmarshal(res, out); err != nil {
				t.Fatalf("%s: %v", method, err)
			}
		}
	case <-c.exited:
		t.Fatalf("%s: the browser ended before it answered: %s", method, c.state())
	case <-time.After(20 * time.Second):
		t.Fatalf("%s: no answer of the browser: %s", method, c.state())
	}
}

// navigate opens u and waits for the page's load.
func (c *cdp) navigate(t *testing.T, u string) {
	t.Helper()
	for len(c.events) > 0 {
		<-c.events
	}
	c.call(t, c.session, "Page.navigate", map[string]any{"url": u}, nil)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if ev == "Page.loadEventFired" {
				return
			}
		case <-deadline:
			t.Fatalf("the page %s did not load", u)
		}
	}
}

// eval is a JavaScript expression's value, as a string.
func (c *cdp) eval(t *testing.T, expr string) string {
	t.Helper()
	var r struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	c.call(t, c.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}, &r)
	return fmt.Sprint(r.Result.Value)
}
