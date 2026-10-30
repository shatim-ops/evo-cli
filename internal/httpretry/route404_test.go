// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
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
