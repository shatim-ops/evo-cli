// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// healthWorld answers /healthz 200, /down with an error of the network and
// any other path 404.
type healthWorld struct{}

// healthRequests counts the requests healthWorld got.
var healthRequests int

func (healthWorld) RoundTrip(req *http.Request) (*http.Response, error) {
	healthRequests++
	switch req.URL.Path {
	case "/down":
		return nil, errors.New("connection refused")
	case "/healthz":
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	}
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusNotFound)
	return rec.Result(), nil
}

// evo mcp healthcheck names the server it probed without the secrets of
// its address: the password of the userinfo and the query (a token) stay
// out of the answer, the error and its --json envelope, whether the
// address comes from --url or MCP_HEALTHCHECK_URL (Астра #2381).
func TestMCPHealthcheck_URLSecretsNotEchoed(t *testing.T) {
	old := httpretry.DefaultBase
	httpretry.DefaultBase = healthWorld{}
	t.Cleanup(func() { httpretry.DefaultBase = old })
	const pw, tok = "s3cr3tPw2381", "t0ken2381"
	addr := func(path string) string { return "http://user:" + pw + "@127.0.0.1:18080" + path + "?token=" + tok }
	for _, c := range []struct {
		name string
		args []string
		env  string
		ok   bool
		// asks says the address is one of HTTP: the request is sent.
		asks bool
	}{
		{"healthy, --json", []string{"--json", "mcp", "healthcheck", "--url", addr("/healthz")}, "", true, true},
		{"healthy, env", []string{"--json", "mcp", "healthcheck"}, addr("/healthz"), true, true},
		{"404", []string{"mcp", "healthcheck", "--url", addr("/missing")}, "", false, true},
		{"404, --json", []string{"--json", "mcp", "healthcheck", "--url", addr("/missing")}, "", false, true},
		{"network", []string{"mcp", "healthcheck", "--url", addr("/down")}, "", false, true},
		{"not a URL", []string{"mcp", "healthcheck", "--url", "http://user:" + pw + "@[::1" + "?token=" + tok}, "", false, false},
		// No scheme of HTTP or no host: the value would turn into the
		// scheme, the path or the host of the request and come back in its
		// error («Риск», 368).
		{"bare", []string{"mcp", "healthcheck", "--url", pw}, "", false, false},
		{"path only", []string{"mcp", "healthcheck", "--url", "/" + pw}, "", false, false},
		{"no scheme", []string{"mcp", "healthcheck", "--url", pw + "/healthz"}, "", false, false},
		{"no host", []string{"mcp", "healthcheck", "--url", "http:/" + pw}, "", false, false},
		{"value as scheme", []string{"mcp", "healthcheck", "--url", pw + ":8080/healthz"}, "", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MCP_HEALTHCHECK_URL", c.env)
			healthRequests = 0
			out, errOut, code := execCLI(t, c.args...)
			if !c.asks && healthRequests > 0 {
				t.Errorf("an address that is not one of HTTP was asked: %d requests", healthRequests)
			}
			if (code == 0) != c.ok {
				t.Fatalf("exit %d, want ok=%v: %s%s", code, c.ok, out, errOut)
			}
			for _, secret := range []string{pw, tok} {
				if strings.Contains(out+errOut, secret) {
					t.Errorf("the output repeats %q of the address:\nstdout: %s\nstderr: %s", secret, out, errOut)
				}
			}
			if c.ok && !strings.Contains(out, "127.0.0.1:18080/healthz") {
				t.Errorf("the answer no longer names the server it probed: %s", out)
			}
		})
	}
}

// A server that answers with a redirect whose Location does not parse
// puts the Location into the error of net/http whole — the query of the
// request with it, when the server copies it there. The error of the
// healthcheck says neither the user, the password nor the query of the
// address, whatever Location the server gives — empty, relative, one that
// does not parse, one of another host — on a real HTTP client (Астра,
// #2381 reopened; «Риск», conclusions 392 and the condition of rc.2).
func TestMCPHealthcheck_RedirectErrorHidesTheAddressSecrets(t *testing.T) {
	const user, pw, tok = "us3rTok2381r", "s3cr3tPw2381r", "t0ken2381r"
	for name, location := range map[string]func(r *http.Request) string{
		"empty":    func(*http.Request) string { return "" },
		"relative": func(r *http.Request) string { return "/elsewhere?" + r.URL.RawQuery },
		"does not parse": func(r *http.Request) string {
			return "http://127.0.0.1:bad/healthz?" + r.URL.RawQuery + "#" + pw + "&" + user
		},
		"another host": func(r *http.Request) string {
			return "http://" + user + ":" + pw + "@127.0.0.1:1/healthz?" + r.URL.RawQuery
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Location", location(r))
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(srv.Close)
			old := httpretry.DefaultBase
			httpretry.DefaultBase = http.DefaultTransport
			t.Cleanup(func() { httpretry.DefaultBase = old })
			t.Setenv("MCP_HEALTHCHECK_URL", "")
			addr := strings.Replace(srv.URL, "http://", "http://"+user+":"+pw+"@", 1) + "/redirect?token=" + tok
			for _, args := range [][]string{{"mcp", "healthcheck", "--url", addr}, {"--json", "mcp", "healthcheck", "--url", addr}} {
				out, errOut, code := execCLI(t, args...)
				if code == 0 {
					t.Fatalf("%v: a redirect the server breaks answers healthy: %s", args, out)
				}
				for _, secret := range []string{user, pw, tok} {
					if strings.Contains(out+errOut, secret) {
						t.Errorf("%v: the error repeats %q of the address:\n%s%s", args, secret, out, errOut)
					}
				}
			}
		})
	}
}

// The error of a request of the healthcheck does not print the text of
// the error of net/http at all: it names the class of the failure and the
// address probed, without its user, password, query and fragment. A
// server that copies them into a Location that does not parse — encoded,
// with the query reordered — got them back in either form, which no list
// of replacements holds (Астра #2511, after #2482; «Риск», the condition
// of #2511).
func TestMCPHealthcheck_ErrorNamesTheClassNotTheTextOfTheNetwork(t *testing.T) {
	const user, pw = "USER2511/x", "PASS2511/x"
	params := [][2]string{{"token", "QUERY2511/x"}, {"other", "OTHER2511 y"}, {"k2511key", "v"}}
	const frag = "FRAG2511/x"
	enc := url.QueryEscape
	var forms []string
	for _, s := range []string{user, pw, frag, "k2511key"} {
		forms = append(forms, s, enc(s), url.PathEscape(s))
	}
	for _, p := range params {
		forms = append(forms, p[1], enc(p[1]), url.PathEscape(p[1]))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		q := strings.Split(r.URL.RawQuery, "&")
		slices.Reverse(q)
		w.Header().Set("Location", "http://"+enc(u)+":"+enc(p)+"@127.0.0.1:bad/end?"+strings.Join(q, "&")+"#"+enc(frag))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	old := httpretry.DefaultBase
	httpretry.DefaultBase = http.DefaultTransport
	t.Cleanup(func() { httpretry.DefaultBase = old })
	t.Setenv("MCP_HEALTHCHECK_URL", "")
	var query []string
	for _, p := range params {
		query = append(query, enc(p[0])+"="+enc(p[1]))
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	addr := "http://" + url.UserPassword(user, pw).String() + "@" + host + "/start?" + strings.Join(query, "&") + "#" + enc(frag)
	for _, args := range [][]string{{"mcp", "healthcheck", "--url", addr}, {"--json", "mcp", "healthcheck", "--url", addr}} {
		out, errOut, code := execCLI(t, args...)
		if code == 0 {
			t.Fatalf("%v: a redirect that does not parse answers healthy: %s", args, out)
		}
		for _, f := range forms {
			if strings.Contains(out+errOut, f) {
				t.Errorf("%v: the error repeats %q:\n%s%s", args, f, out, errOut)
			}
		}
		if strings.Contains(out+errOut, "Location") || strings.Contains(out+errOut, ":bad") {
			t.Errorf("%v: the error prints the text of the error of net/http:\n%s%s", args, out, errOut)
		}
		if !strings.Contains(out+errOut, host+"/start") {
			t.Errorf("%v: the error no longer names the address probed:\n%s%s", args, out, errOut)
		}
	}
}

// Each class of a failure of the request is named by evo's words; the
// text of the error it wraps is in none of them, and stays in the chain.
func TestMCPHealthcheck_FailureClasses(t *testing.T) {
	const secret = "SECRET2511"
	inner := func(e error) error { return fmt.Errorf("Get %q: %w", "http://u:"+secret+"@h/?t="+secret, e) }
	for want, err := range map[string]error{
		"no answer in time":                        inner(context.DeadlineExceeded),
		"the host does not resolve":                inner(&net.DNSError{Err: secret, Name: "h"}),
		"connection refused":                       inner(&net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}),
		"the TLS certificate is not trusted":       inner(&tls.CertificateVerificationError{Err: errors.New(secret)}),
		"the address of a redirect does not parse": inner(fmt.Errorf("failed to parse Location header %q: bad", secret)),
		"the request failed":                       inner(errors.New(secret)),
	} {
		h := healthFailure{err, "http://h/healthz"}
		if got := h.Error(); got != "http://h/healthz: "+want {
			t.Errorf("%v: %q, want the class %q", err, got, want)
		}
		if !errors.Is(h, err) {
			t.Errorf("%q: the error it wraps left the chain", want)
		}
	}
}

// statusWorld answers every request with a status whose reason phrase the
// server filled with the address it was asked: the user, the password and
// the query of the request, as a server may.
type statusWorld struct{ code int }

func (s statusWorld) RoundTrip(req *http.Request) (*http.Response, error) {
	u, p, _ := req.BasicAuth()
	rec := httptest.NewRecorder()
	rec.WriteHeader(s.code)
	resp := rec.Result()
	resp.Status = fmt.Sprintf("%d %s:%s %s", s.code, u, p, req.URL.RawQuery)
	return resp, nil
}

// An answer other than 200 is named by its code alone: the reason phrase
// is the server's text, and a server may put the token of the query, the
// user or the password of the address there; the healthcheck printed it
// whole into stderr and the --json envelope (Астра, #2381 reopened on the
// rc.2 candidate).
func TestMCPHealthcheck_StatusIsTheCodeNotTheTextOfTheServer(t *testing.T) {
	const user, pw, tok = "us3r2381s", "s3cr3tPw2381s", "t0ken2381s"
	t.Setenv("MCP_HEALTHCHECK_URL", "")
	old := httpretry.DefaultBase
	t.Cleanup(func() { httpretry.DefaultBase = old })
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusFound} {
		httpretry.DefaultBase = statusWorld{code}
		addr := "http://" + user + ":" + pw + "@127.0.0.1:18080/healthz?token=" + tok
		for _, args := range [][]string{{"mcp", "healthcheck", "--url", addr}, {"--json", "mcp", "healthcheck", "--url", addr}} {
			out, errOut, exit := execCLI(t, args...)
			if exit == 0 {
				t.Fatalf("%d %v: answers healthy: %s", code, args, out)
			}
			for _, secret := range []string{user, pw, tok} {
				if strings.Contains(out+errOut, secret) {
					t.Errorf("%d %v: the error repeats %q the server put in its status:\n%s%s", code, args, secret, out, errOut)
				}
			}
			if !strings.Contains(out+errOut, fmt.Sprint(code)) || !strings.Contains(out+errOut, "127.0.0.1:18080/healthz") {
				t.Errorf("%d %v: the error no longer names the code and the address probed:\n%s%s", code, args, out, errOut)
			}
		}
	}
}

// The same on a real HTTP client: a local server writes its status line by
// hand, the query and the userinfo of the request in the reason phrase, as
// Астра's probe did (#2381, the comment on the rc.2 candidate).
func TestMCPHealthcheck_StatusOfARealServerIsTheCodeOnly(t *testing.T) {
	const user, pw, tok = "us3r2381h", "s3cr3tPw2381h", "t0ken2381h"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		fmt.Fprintf(buf, "HTTP/1.1 503 %s %s:%s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", r.URL.RawQuery, u, p)
		_ = buf.Flush()
	}))
	t.Cleanup(srv.Close)
	old := httpretry.DefaultBase
	httpretry.DefaultBase = http.DefaultTransport
	t.Cleanup(func() { httpretry.DefaultBase = old })
	t.Setenv("MCP_HEALTHCHECK_URL", "")
	host := strings.TrimPrefix(srv.URL, "http://")
	addr := "http://" + user + ":" + pw + "@" + host + "/status?token=" + tok
	for _, args := range [][]string{{"mcp", "healthcheck", "--url", addr}, {"--json", "mcp", "healthcheck", "--url", addr}} {
		out, errOut, code := execCLI(t, args...)
		if code == 0 {
			t.Fatalf("%v: a 503 answers healthy: %s", args, out)
		}
		for _, secret := range []string{user, pw, tok} {
			if strings.Contains(out+errOut, secret) {
				t.Errorf("%v: the error repeats %q of the reason phrase:\n%s%s", args, secret, out, errOut)
			}
		}
		if !strings.Contains(out+errOut, "HTTP 503") || !strings.Contains(out+errOut, host+"/status") {
			t.Errorf("%v: the error no longer names the code and the address probed:\n%s%s", args, out, errOut)
		}
	}
}
