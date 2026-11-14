// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A 404 the service did not write is a route that is not there, not a
// resource that is gone (D63): the client sees a 502, which no client reads
// as «not found». The service's own 404 — the forms api-probe 33 and 34
// saw, 2 of 2 — passes whole, and so does any 404 to a HEAD or of a client
// that keeps its 404s.
func TestRouteNotFoundAsBadGateway(t *testing.T) {
	long := `{"code":5,"message":"resource not found","details":"` + strings.Repeat("x", maxServiceBody) + `"}`
	for _, c := range []struct {
		name, method, ctype, body string
		keep                      bool
		want                      int
	}{
		{"compute array", http.MethodDelete, "application/json", `[{"message":"not found"}]`, false, 404},
		{"code and message", http.MethodDelete, "application/json", `{"code":5,"message":"resource not found","details":[]}`, false, 404},
		{"VPC NotFound", http.MethodGet, "application/json", `{"code":5,"message":"NotFound"}`, false, 404},
		{"longer than read", http.MethodDelete, "application/json", long, false, 404},
		{"empty", http.MethodDelete, "", "", false, 502},
		{"html", http.MethodGet, "text/html", "<html>404</html>", false, 502},
		{"plain text", http.MethodDelete, "text/plain", "404 page not found", false, 502},
		{"error_msg", http.MethodDelete, "application/json", `{"error_msg":"no route"}`, false, 502},
		{"grpc-gateway routing", http.MethodDelete, "application/json", `{"code":5,"message":"Not Found","details":[]}`, false, 502},
		{"HEAD", http.MethodHead, "", "", false, 404},
		{"kept", http.MethodGet, "", "", true, 404},
	} {
		rt := rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": {c.ctype}},
				Body: io.NopCloser(strings.NewReader(c.body)), Request: r}, nil
		})
		cl := &http.Client{Transport: &Transport{Base: rt, Policy: Policy{Keep404: c.keep}}}
		req, _ := http.NewRequest(c.method, "https://api.example/v1/things/x", nil)
		resp, err := cl.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.want)
		}
		if c.want == 404 && string(body) != c.body {
			t.Errorf("%s: the service's body did not pass whole (%d of %d bytes)", c.name, len(body), len(c.body))
		}
		if c.want == 502 && !strings.Contains(string(body), "route or the address") {
			t.Errorf("%s: body %s", c.name, body)
		}
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// FB160-1 (audit of Fable 1.6): the answer to the request sent again with
// the renewed token goes through RouteNotFoundAsBadGateway too — a 404 the
// service did not write after a 401 is a route, not a gone resource (D63),
// whichever path of RoundTrip the answer took.
func TestRouteNotFoundAfterTheRenewedToken(t *testing.T) {
	OnTokenRenewed(func(_ context.Context, tok string) (string, bool) { return tok + "-new", true })
	t.Cleanup(func() { OnTokenRenewed(nil) })
	rt := rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "Bearer old" {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": {"text/plain"}},
			Body: io.NopCloser(strings.NewReader("404 page not found")), Request: r}, nil
	})
	req, _ := http.NewRequest(http.MethodDelete, "https://api.example/v1/things/x", nil)
	req.Header.Set("Authorization", "Bearer old")
	resp, err := (&Transport{Base: rt}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || resp.Header.Get(Replaced) == "" {
		t.Errorf("after the renewed token: status %d, %s %q; want 502 replaced", resp.StatusCode, Replaced, resp.Header.Get(Replaced))
	}
}

// brokenAfter is a body that gives data and then err, in the same read as
// the data's last bytes or in a read of its own, and io.EOF after that:
// a connection that broke once (#1746, #1769).
type brokenAfter struct {
	data     string
	err      error
	ownRead  bool
	off, end int
}

func (b *brokenAfter) Read(p []byte) (int, error) {
	if b.off < len(b.data) {
		n := copy(p, b.data[b.off:])
		b.off += n
		if b.off == len(b.data) && !b.ownRead {
			b.end++
			return n, b.err
		}
		return n, nil
	}
	if b.end++; b.end == 1 {
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *brokenAfter) Close() error { return nil }

// The service's 404 that RouteNotFoundAsBadGateway passes on is the body
// the API gave, its error included: a read that broke does not end as a
// whole answer (external review, #1769; the same class as #1746).
func TestRouteNotFound_ReadErrorReachesTheCaller(t *testing.T) {
	service := `{"code":5,"message":"resource not found","details":[]}`
	edge := `{"code":5,"message":"resource not found","details":"` + strings.Repeat("x", maxServiceBody) + `"}`
	for _, c := range []struct {
		name, body string
		ownRead    bool
		want       int
	}{
		{"error with the bytes", service, false, 404},
		{"error in a read of its own", service, true, 404},
		// Cut short of the read, the service's JSON is not whole: the
		// answer is replaced (D63), which is its own expected result.
		{"limit -1", edge[:maxServiceBody-1], false, 502},
		{"limit", edge[:maxServiceBody], false, 404},
		{"limit +1", edge[:maxServiceBody+1], true, 404},
	} {
		rt := rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": {"application/json"}},
				Body: &brokenAfter{data: c.body, err: io.ErrUnexpectedEOF, ownRead: c.ownRead}, Request: r}, nil
		})
		resp := RouteNotFoundAsBadGateway(httptestRequest(), mustRT(t, rt))
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.want)
		}
		if c.want == 404 && (string(b) != c.body || err != io.ErrUnexpectedEOF) {
			t.Errorf("%s: the caller read %d of %d bytes, %v; want all of them and %v", c.name, len(b), len(c.body), err, io.ErrUnexpectedEOF)
		}
	}
}

func httptestRequest() *http.Request {
	req, _ := http.NewRequest(http.MethodDelete, "https://api.example/v1/things/x", nil)
	return req
}

func mustRT(t *testing.T, rt http.RoundTripper) *http.Response {
	t.Helper()
	resp, err := rt.RoundTrip(httptestRequest())
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
