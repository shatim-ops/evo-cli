// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
