// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// loopbackRoutes are the routes of the loopback of evo login, closed: a
// route added to loopback.go fails TestLoopback_ClosedSurface until it is
// named here (B-6). fontRoute is the one route of static files: a face
// embedded in the binary, GET alone, cached (fontCache), never a page.
var loopbackRoutes = map[string]bool{
	"GET /callback": true,
	"POST /grant":   true,
	"POST /cancel":  true,
	fontRoute:       true,
}

const fontRoute = "GET /fonts/<face>.woff2"

// route is the route of a probe: a real embedded face is fontRoute.
func route(probe string) string {
	method, path, _ := strings.Cut(probe, " ")
	if face, ok := strings.CutPrefix(path, "/fonts/"); ok && method == http.MethodGet {
		if _, err := fontFiles.ReadFile("fonts/" + face); err == nil {
			return fontRoute
		}
	}
	return probe
}

// loopbackCSP is the policy of every page: nothing loads but the fonts of
// the loopback itself (SB Sans, embedded: TestFonts_ServedByTheLoopback),
// no script, the form goes to the loopback alone, no frame.
const loopbackCSP = "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; form-action 'self'; frame-ancestors 'none'"

// ownFont is the one url() a page may carry: a face the loopback serves
// from the binary (fontRoute); any other /fonts/ path is 404 like the rest.
var ownFont = regexp.MustCompile(`url\(/fonts/[A-Za-z]+\.woff2\)`)

// offSite finds in a page what loads or links another origin: a script, a
// stylesheet or image, url() and @import of CSS, and an address of a link,
// source or form that is not a path of the loopback.
var offSite = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<\s*(script|link|img|iframe|object|embed|base|meta\s+http-equiv)\b`),
	regexp.MustCompile(`(?i)url\s*\(|@import`),
	regexp.MustCompile(`(?i)\b(href|src|action|formaction|srcset)\s*=\s*["']?\s*(?:[a-z][a-z0-9+.-]*:|//)`),
}

// surfaceProbes are requests no page makes: every method on the routes and
// beside them. Only the routes answer otherwise than 404 (403: no state).
var surfaceProbes = func() []string {
	var out []string
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH"} {
		for _, p := range []string{"/", "/callback", "/grant", "/cancel", "/callback/", "/grant/x", "/favicon.ico",
			"/fonts/x.woff2", "/fonts/SBSansText.woff2", "/fonts/../fonts.go", "/static/x.css", "/debug/pprof/", "/%2e%2e/etc/passwd", "/CALLBACK"} {
			out = append(out, m+" "+p)
		}
	}
	return out
}()

type loopbackResponse struct {
	what   string
	status int
	header http.Header
	body   string
}

// B-6, the loopback of evo login as a surface: the routes are a closed
// list and any other method or path is 404; a foreign Host is 403; every
// answer to the loopback's own Host is no-referrer and no-store; every page carries exactly loopbackCSP and DENY; no page loads
// or links another origin. Walked on the consent, the result, the cancel
// and the error of a refused login, with the probes sent while the consent
// waits — they do not end the login.
func TestLoopback_ClosedSurface(t *testing.T) {
	for _, c := range []struct {
		name   string
		v      loginworld.Variant
		action string // the button the browser presses, "" — none (the login ends at the callback)
		probe  bool
	}{
		{"grant, with the probes", loginworld.Variant{}, "/grant", true},
		{"cancel", loginworld.Variant{}, "/cancel", false},
		{"no right: the error page", loginworld.Variant{Denied: true}, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, c.v)
			var got []loopbackResponse
			ended := make(chan struct{})
			open := func(authURL string) error {
				go func() {
					defer close(ended)
					got = surfaceBrowse(t, w.Client(), authURL, c.action, c.probe)
				}()
				return nil
			}
			cfg := worldConfig(w, io.Discard, open)
			cfg.Tell = func(string) {}
			_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
			<-ended
			if (err == nil) != (c.action == "/grant") {
				t.Fatalf("login: %v — the run did not take the path it walks", err)
			}
			pages, probes := 0, 0
			for _, r := range got {
				// A foreign Host is refused before anything else: plain
				// text, 403 (checked by surfaceBrowse), no headers of ours.
				if r.what == "a foreign Host" {
					if strings.Contains(r.body, "<") {
						t.Errorf("%s: a refusal with markup: %.100q", r.what, r.body)
					}
					continue
				}
				// A face: the one answer that is cached, binary, not a page.
				if strings.HasPrefix(r.what, "probe ") && route(strings.TrimPrefix(r.what, "probe ")) == fontRoute {
					probes++
					if h := r.header; r.status != http.StatusOK || h.Get("Content-Type") != "font/woff2" || h.Get("X-Content-Type-Options") != "nosniff" ||
						h.Get("Cache-Control") != fontCache || h.Get("Referrer-Policy") != "no-referrer" {
						t.Errorf("%s: %d %v — a face of the binary, cached, nosniff", r.what, r.status, h)
					}
					continue
				}
				if r.header.Get("Referrer-Policy") != "no-referrer" || r.header.Get("Cache-Control") != "no-store" {
					t.Errorf("%s: Referrer-Policy %q, Cache-Control %q", r.what, r.header.Get("Referrer-Policy"), r.header.Get("Cache-Control"))
				}
				if strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
					pages++
					if csp := r.header.Get("Content-Security-Policy"); csp != loopbackCSP {
						t.Errorf("%s: CSP %q, want %q", r.what, csp, loopbackCSP)
					}
					if r.header.Get("X-Frame-Options") != "DENY" {
						t.Errorf("%s: X-Frame-Options %q", r.what, r.header.Get("X-Frame-Options"))
					}
					body := ownFont.ReplaceAllString(r.body, "")
					for _, re := range offSite {
						if m := re.FindString(body); m != "" {
							t.Errorf("%s: the page loads or links another origin: %q", r.what, m)
						}
					}
					continue
				}
				if strings.Contains(r.body, "<") {
					t.Errorf("%s: a refusal with markup: %.100q", r.what, r.body)
				}
				if !strings.HasPrefix(r.what, "probe ") {
					continue
				}
				probes++
				route := route(strings.TrimPrefix(r.what, "probe "))
				switch {
				case loopbackRoutes[route] && r.status != http.StatusForbidden:
					t.Errorf("%s: %d — a route without the state is 403", r.what, r.status)
				case !loopbackRoutes[route] && r.status != http.StatusNotFound:
					t.Errorf("%s: %d — not a route of the loopback, want 404", r.what, r.status)
				}
			}
			if pages < 1 || c.probe && probes != len(surfaceProbes) {
				t.Errorf("%d pages, %d probes: the walk sees nothing", pages, probes)
			}
		})
	}
}

// surfaceBrowse is the person's browser: the provider, the callback, the
// probes while the consent waits (with a foreign Host too), then the button
// action with the page's own state and token. It keeps every answer of
// the loopback.
func surfaceBrowse(t *testing.T, c *http.Client, authURL, action string, probe bool) []loopbackResponse {
	var out []loopbackResponse
	keep := func(what string, resp *http.Response, err error) string {
		if err != nil {
			t.Errorf("%s: %v", what, err)
			return ""
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		out = append(out, loopbackResponse{what, resp.StatusCode, resp.Header, string(b)})
		return string(b)
	}
	resp, err := c.Get(authURL)
	if err != nil {
		t.Errorf("the provider: %v", err)
		return nil
	}
	resp.Body.Close()
	cb, _ := url.Parse(resp.Header.Get("Location"))
	resp, err = c.Get(cb.String())
	page := keep("the callback", resp, err)
	m := stateField.FindStringSubmatch(page)
	if m == nil {
		return out
	}
	base := "http://" + cb.Host
	if probe {
		for _, p := range surfaceProbes {
			method, path, _ := strings.Cut(p, " ")
			req, _ := http.NewRequest(method, base+path, strings.NewReader("state=wrong"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := c.Do(req)
			keep("probe "+p, resp, err)
		}
		// DNS rebinding: a name that resolves to the loopback is refused.
		req, _ := http.NewRequest(http.MethodGet, base+"/callback", nil)
		req.Host = "evil.example:" + cb.Port()
		resp, err := c.Do(req)
		if err == nil && resp.StatusCode != http.StatusForbidden {
			t.Errorf("a foreign Host: %d, want 403", resp.StatusCode)
		}
		keep("a foreign Host", resp, err)
	}
	f := url.Values{"state": {m[1]}, "audience": {"me"}, "ttl": {"30"}}
	if tok := consentField.FindStringSubmatch(page); tok != nil {
		f.Set("consent", tok[1])
	}
	if p := checkedProject.FindStringSubmatch(page); p != nil {
		f.Set("project", p[1])
	}
	resp, err = c.PostForm(base+action, f)
	keep("the "+action+" page", resp, err)
	return out
}
