// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// loginCreates are the requests the inventory (scripts/api-contract.json)
// marks as creates of «evo login», as "METHOD path" patterns: the list of
// what the consent guards comes from the one declaration, not from the
// HTTP method.
func loginCreates(t *testing.T) []*regexp.Regexp {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "api-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Services []struct {
			Requests []struct {
				Method, Path string
				Creates      []string
			}
		}
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	param := regexp.MustCompile(`\\\{[^}]*\\\}`)
	var out []*regexp.Regexp
	for _, s := range c.Services {
		for _, r := range s.Requests {
			for _, name := range r.Creates {
				if name == "evo login" {
					p := param.ReplaceAllString(regexp.QuoteMeta(r.Path), `[^/]+`)
					out = append(out, regexp.MustCompile(`^`+r.Method+` `+p+`$`))
				}
			}
		}
	}
	if len(out) < 3 {
		t.Fatalf("the inventory declares %d creates of evo login: the check sees nothing", len(out))
	}
	return out
}

func createsIn(calls []string, creates []*regexp.Regexp) []string {
	var hit []string
	for _, c := range calls {
		for _, re := range creates {
			if re.MatchString(c) {
				hit = append(hit, c)
				break
			}
		}
	}
	return hit
}

// Invariant B (B-3, «Риск», conclusions 24 and R-47): the only consent of
// evo login is POST /grant of the loopback page with the page's own consent
// token. Before it, not one request the inventory marks as a create reaches
// the cloud — with a terminal (the wizard's answers are no consent) and
// without one; a forged or missing token, the cancel and a person who never
// presses the button end the login with nothing created, no key and no
// profile. The positive control grants with the page's token and sees the
// creates, so the check is not blind.
func TestLogin_NoCreateBeforeTheGrant(t *testing.T) {
	creates := loginCreates(t)
	state := regexp.MustCompile(`name="state" value="([^"]+)"`)
	consent := regexp.MustCompile(`name="consent" value="([^"]+)"`)
	type post struct {
		path    string
		consent func(page string) string // "" leaves the field out
	}
	pageToken := func(page string) string {
		if m := consent.FindStringSubmatch(page); m != nil {
			return m[1]
		}
		return ""
	}
	for _, c := range []struct {
		name  string
		tty   bool
		posts []post
		grant bool // the positive control
	}{
		{name: "no terminal, the page left open"},
		{name: "terminal, the wizard answered, the page left open", tty: true},
		{name: "no terminal, forged and missing consent", posts: []post{
			{"/grant", func(string) string { return "forged" }},
			{"/grant", func(string) string { return "" }},
		}},
		{name: "terminal, forged consent", tty: true, posts: []post{
			{"/grant", func(p string) string { return pageToken(p) + "x" }},
		}},
		{name: "no terminal, cancel", posts: []post{{"/cancel", pageToken}}},
		{name: "terminal, cancel", tty: true, posts: []post{{"/cancel", pageToken}}},
		{name: "no terminal, grant with the page's token", grant: true, posts: []post{{"/grant", pageToken}}},
		{name: "terminal, grant with the page's token", tty: true, grant: true, posts: []post{{"/grant", pageToken}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			setupCLI(t)
			w := loginworld.New(t, loginworld.Variant{AnyLoopbackPort: true})
			oldCfg, oldOpen, oldIAM, oldTTY := loginConfig, openBrowser, auth.IAMEndpoint, stdinIsTerminal
			t.Cleanup(func() { loginConfig, openBrowser, auth.IAMEndpoint, stdinIsTerminal = oldCfg, oldOpen, oldIAM, oldTTY })
			auth.IAMEndpoint = w.URL + "/api/v1/auth/token"
			stdinIsTerminal = func() bool { return c.tty }
			loginConfig = func(say func(string, string), open func(string) error) login.Config {
				cfg := login.Default(say, open)
				cfg.Issuer, cfg.OrgBase, cfg.AdminBase, cfg.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
				cfg.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
				cfg.HTTP = w.Client()
				cfg.Ports = []int{0} // no test takes a registered port
				if !c.grant {
					// A test bound on the person's choice, not a wall-clock
					// read: the page is never granted here.
					cfg.Wait = 2 * time.Second
				}
				return cfg
			}
			done := make(chan struct{})
			openBrowser = func(authURL string) error {
				go func() {
					defer close(done)
					cl := w.Client()
					resp, err := cl.Get(authURL)
					if err != nil {
						return
					}
					resp.Body.Close()
					cb, _ := url.Parse(resp.Header.Get("Location"))
					resp, err = cl.Get(cb.String())
					if err != nil {
						return
					}
					b, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					page := string(b)
					m := state.FindStringSubmatch(page)
					if m == nil {
						t.Errorf("the consent page carries no state")
						return
					}
					for _, p := range c.posts {
						if !c.grant {
							if hit := createsIn(w.Calls(), creates); len(hit) != 0 {
								t.Errorf("before %s: creates %v", p.path, hit)
							}
						}
						v := url.Values{"state": {m[1]}, "project": {loginworld.Project}, "audience": {"agent"}, "ttl": {"7"}}
						if tok := p.consent(page); tok != "" {
							v.Set("consent", tok)
						}
						if r, err := cl.PostForm("http://"+cb.Host+p.path, v); err == nil {
							r.Body.Close()
						}
					}
				}()
				return nil
			}

			args := []string{"--json", "login", "--for", "agent", "--key-ttl", "7d"}
			stdin := ""
			if c.tty {
				// No flags: the wizard asks, and its every answer is «yes»
				// or the default — none of them is the consent.
				args, stdin = []string{"login"}, strings.Repeat("y\n\n", 8)
			}
			code, stdout, stderr := runCLI(t, stdin, args...)
			<-done
			hit := createsIn(w.Calls(), creates)
			if c.grant {
				if code != 0 || len(hit) < 3 {
					t.Fatalf("the grant with the page's token: exit %d, creates %v\n%s\n%s", code, hit, stdout, stderr)
				}
				return
			}
			if code == 0 {
				t.Errorf("the login succeeded without the grant\n%s\n%s", stdout, stderr)
			}
			if len(hit) != 0 {
				t.Errorf("creates without the grant: %v (calls %v)", hit, w.Calls())
			}
			if len(w.Accounts()) != 0 || len(w.Permissions()) != 0 || len(w.Keys()) != 0 {
				t.Errorf("left in the cloud: %+v %+v %+v", w.Accounts(), w.Permissions(), w.Keys())
			}
			for _, p := range []string{"default", "agent"} {
				if _, _, found, _ := config.LoadProfile(p); found {
					t.Errorf("a profile %q without the grant", p)
				}
			}
			for _, s := range w.Secrets() {
				if strings.Contains(stdout+stderr, s) {
					t.Errorf("a secret reached the output: %q", s)
				}
			}
		})
	}
}
