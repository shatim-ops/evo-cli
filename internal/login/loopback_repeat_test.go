// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// closedConn is the error of a request to a loopback evo has closed, in
// the words of Linux and macOS and of Windows (WSAECONNREFUSED «actively
// refused», WSAECONNRESET «forcibly closed»: main, windows-latest, 10.11),
// and of Go when the loopback closes the connection while the answer is
// read («use of closed network connection»: #1666, ubuntu-latest, 13.11).
var closedConn = regexp.MustCompile(`connection refused|connection reset|EOF|actively refused|forcibly closed|use of closed network connection`)

// Every form a closed loopback has been seen in on a runner is one
// closedConn knows: a form it does not know fails the repeat test as a
// defect of evo, though the loopback was only closed.
func TestLoopback_ClosedConnKnowsTheSeenForms(t *testing.T) {
	for _, e := range []string{
		"dial tcp 127.0.0.1:1: connect: connection refused",
		"read tcp 127.0.0.1:2->127.0.0.1:1: read: connection reset by peer",
		"Post \"http://127.0.0.1:1/cancel\": EOF",
		"No connection could be made because the target machine actively refused it.",
		"An existing connection was forcibly closed by the remote host.",
		"Post \"http://127.0.0.1:1/cancel\": read tcp 127.0.0.1:2->127.0.0.1:1: use of closed network connection",
	} {
		if !closedConn.MatchString(e) {
			t.Errorf("closedConn does not know %q", e)
		}
	}
}

// replay is one request a browser sends to the loopback again.
type replay struct {
	first  string // the person's action on the consent page: /grant or /cancel
	second string // the request sent again: a route of the loopback
	// together: the second is sent at the same time as the first, not
	// after its answer.
	together bool
}

// reply is an answer of the loopback.
type reply struct {
	status int
	body   string
}

func send(c *http.Client, method, u string, form url.Values) reply {
	var resp *http.Response
	var err error
	if method == http.MethodGet {
		resp, err = c.Get(u)
	} else {
		resp, err = c.PostForm(u, form)
	}
	if err != nil {
		return reply{status: -1, body: err.Error()}
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return reply{resp.StatusCode, string(b)}
}

// Plan 1.8, #1095, rounds 1–2 and 4 of the audit (Grok: Ctrl+C and a double
// click during the grant), class of #1100: a request the browser sends to
// the loopback again — after the answer or at the same time, the same or
// another action — does not change the end of the login. There is one end:
// every answer 200 after the consent is the same page, the login returns
// what that page says, and the world holds at most one account and one key
// (a grant) or none (a cancel); a repeat is the same page or 403, never an
// error of its own. Every route of the loopback is in the matrix.
func TestLoopback_RepeatedRequestKeepsTheEnd(t *testing.T) {
	// The routes are the closed list of TestLoopback_ClosedSurface; the
	// fonts are static, never a step of the login.
	var routes []string
	for r := range loopbackRoutes {
		if r != fontRoute {
			routes = append(routes, r)
		}
	}
	sort.Strings(routes)
	if len(routes) < 3 {
		t.Fatalf("%d routes of the loopback: the matrix sees nothing", len(routes))
	}
	var cases []replay
	for _, r := range routes {
		method, path, _ := strings.Cut(r, " ")
		if (path == "/callback") != (method == http.MethodGet) {
			t.Errorf("route %s of the loopback: the matrix of repeats knows GET /callback and POST of the consent; add how to repeat it", r)
			continue
		}
		for _, first := range []string{"/grant", "/cancel"} {
			for _, together := range []bool{false, true} {
				cases = append(cases, replay{first, path, together})
			}
		}
	}
	for _, c := range cases {
		name := c.first + "+" + c.second
		if c.together {
			name += " together"
		}
		t.Run(name, func(t *testing.T) {
			var logged bytes.Buffer
			log.SetOutput(&logged)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			w := newWorld(t, loginworld.Variant{})
			var out bytes.Buffer
			st := &memStore{}
			ended := make(chan struct{})
			var replies []reply
			open := func(authURL string) error {
				go func() {
					defer close(ended)
					// A connection per request: a keep-alive one the
					// loopback's Shutdown closes as idle is an EOF of the
					// client, not an answer of evo.
					cl := w.Client()
					if tr, ok := cl.Transport.(*http.Transport); ok {
						tr = tr.Clone()
						tr.DisableKeepAlives = true
						cl.Transport = tr
					}
					resp, err := cl.Get(authURL)
					if err != nil {
						return
					}
					resp.Body.Close()
					cb := resp.Header.Get("Location")
					u, _ := url.Parse(cb)
					base := "http://" + u.Host
					first := send(cl, http.MethodGet, cb, nil)
					page := first.body
					m, tok := stateField.FindStringSubmatch(page), consentField.FindStringSubmatch(page)
					if m == nil || tok == nil {
						t.Errorf("no consent page: %d %.200s", first.status, page)
						return
					}
					f := url.Values{"state": {m[1]}, "consent": {tok[1]}, "audience": {"me"}, "ttl": {"30"}, "project": {pickProject(page)}}
					again := func() reply {
						if c.second == "/callback" {
							return send(cl, http.MethodGet, cb, nil)
						}
						return send(cl, http.MethodPost, base+c.second, f)
					}
					var mu sync.Mutex
					var wg sync.WaitGroup
					wg.Add(1)
					go func() {
						defer wg.Done()
						r := send(cl, http.MethodPost, base+c.first, f)
						mu.Lock()
						replies = append(replies, r)
						mu.Unlock()
					}()
					if !c.together {
						wg.Wait()
					}
					r := again()
					wg.Wait()
					mu.Lock()
					replies = append(replies, r)
					mu.Unlock()
				}()
				return nil
			}
			res, err := Login(context.Background(), worldConfig(w, &out, open), Choice{}, st)
			<-ended
			var end string
			for i, r := range replies {
				switch {
				case i == 1 && c.second != "/callback" && r.status == http.StatusForbidden:
					// A double click on the page (round 4 of the audit):
					// the browser shows the answer to the second click.
					t.Errorf("a second %s while the loopback is up answers 403 — the browser shows it instead of the end of the login: %.200s", c.second, r.body)
				case r.status == http.StatusOK && end == "":
					end = r.body
				case r.status == http.StatusOK && r.body != end:
					t.Errorf("two ends of one login: the repeat shows another page\nfirst: %.300s\nrepeat: %.300s", end, r.body)
				case r.status == -1 && closedConn.MatchString(r.body):
					// The login ended and evo closed the loopback (B-5):
					// the repeat meets nothing and changes nothing. A
					// connection still in the listener's queue is closed
					// with it (EOF, reset); a panic of a handler, which
					// would close it too, fails the test by the log.
				case r.status != http.StatusOK && r.status != http.StatusForbidden:
					t.Errorf("a repeat answers %d: %.200s — the end page, 403 (a callback) or a closed loopback", r.status, r.body)
				}
			}
			if strings.Contains(logged.String(), "panic") {
				t.Errorf("a handler of the loopback panicked:\n%s", logged.String())
			}
			done := strings.Contains(end, tDone())
			switch {
			case end == "":
				t.Errorf("no answer 200: %+v", replies)
			case done:
				if err != nil || len(w.Accounts()) != 1 || len(w.Keys()) != 1 || len(st.saved) != 1 {
					t.Errorf("the page says done; login: %v, accounts %d, keys %d, saved %d", err, len(w.Accounts()), len(w.Keys()), len(st.saved))
				}
				if res.KeyID == "" {
					t.Errorf("the page says done, the result has no key")
				}
			default:
				if err == nil || len(w.Accounts()) != 0 || len(w.Keys()) != 0 || len(st.saved) != 0 {
					t.Errorf("the page says not done; login: %v, accounts %d, keys %d, saved %d\n%.300s", err, len(w.Accounts()), len(w.Keys()), len(st.saved), end)
				}
			}
			if !c.together && (done != (c.first == "/grant")) {
				t.Errorf("%s answered first, and the end is %v", c.first, map[bool]string{true: "done", false: "not done"}[done])
			}
		})
	}
}

// Release audit 1.8 (A18-13): the wait of the login ends while «Grant
// access» is being written. The grant holds the loopback; the end of the
// wait must answer what the grant made, not «nothing was created» beside a
// key in the cloud — as a Ctrl-C does (#1100, D3).
func TestLogin_WaitEndsDuringTheGrantAnswersItsResult(t *testing.T) {
	var cfgWait time.Duration = 300 * time.Millisecond // wall-clock: a share of the production wait, to end the test
	w := newWorld(t, loginworld.Variant{BeforeKey: func() {
		time.Sleep(2 * cfgWait) // wall-clock: the wait ends while the key is being made
	}})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	st := &memStore{}
	cfg := worldConfig(w, &out, b.open)
	cfg.Wait = cfgWait
	res, err := Login(context.Background(), cfg, Choice{}, st)
	<-b.ended
	keys := len(w.Keys())
	switch {
	case keys == 1 && (err != nil || len(st.saved) != 1 || res.KeyID == ""):
		t.Fatalf("the wait ended in the grant: a key is in the cloud, and the login answers err %v; saved %d; key %q", err, len(st.saved), res.KeyID)
	case keys == 0 && err == nil:
		t.Fatalf("no key in the cloud, and the login succeeded")
	case keys > 1:
		t.Fatalf("%d keys", keys)
	}
}

// Release audit 1.8, findings 14 and 34 (R3-09; R4-1/2, AUTH-51): the
// exchange of the code cut by the network ends the login with nothing made
// and an answer that is not a success; a login broken at any write and
// rolled back leaves the project so that the next login succeeds and holds
// exactly one account, one key and one role of evo.
func TestLogin_BrokenLoginLeavesARepeatableProject(t *testing.T) {
	t.Run("the exchange of the code cut", func(t *testing.T) {
		w := newWorld(t, loginworld.Variant{TokenDrop: true})
		r := login(t, w, &browser{}, Choice{}, nil, nil)
		if r.err == nil || len(w.Accounts()) != 0 || len(w.Keys()) != 0 || len(r.saved) != 0 {
			t.Fatalf("err %v; accounts %d, keys %d, saved %d", r.err, len(w.Accounts()), len(w.Keys()), len(r.saved))
		}
	})
	for _, step := range []loginworld.Step{loginworld.StepAccount, loginworld.StepPermission, loginworld.StepKey, loginworld.StepKeyCheck} {
		t.Run("broken at "+string(step)+", then again", func(t *testing.T) {
			w := newWorld(t, loginworld.Variant{FailAt: step})
			if r := login(t, w, &browser{}, Choice{}, nil, nil); r.err == nil {
				t.Fatalf("the login broken at %s succeeded", step)
			}
			w.Heal()
			r := login(t, w, &browser{}, Choice{}, nil, nil)
			if r.err != nil {
				t.Fatalf("the login after the broken one: %v", r.err)
			}
			if a, k, p := len(w.Accounts()), len(w.Keys()), len(w.Permissions()); a != 1 || k != 1 || p != 1 {
				t.Errorf("after a broken login and a good one: accounts %d, keys %d, roles %d — want 1, 1, 1", a, k, p)
			}
		})
	}
}
