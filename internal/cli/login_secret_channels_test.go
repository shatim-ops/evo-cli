// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// A0-5 (release-plan 1.8), evo login: it takes no secret by a flag, so
// secretIntake does not name it; this walk is its line. The secrets of the
// login — the code, the person's and the account's tokens, the state and
// the consent token of the page — reach no file under the home and no
// output; the secret of the key it grants lives in credentials.yaml alone
// while the profile does, and nowhere after evo logout. The walk follows
// every call: the login granted, the profile used (whoami, doctor), the
// journal exported, logout, and a login cancelled on the page.
func TestSecretChannels_Login(t *testing.T) {
	setupCLI(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if code, out, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: exit %d: %s%s", code, out, errOut)
	}
	w := loginworld.New(t, loginworld.Variant{AnyLoopbackPort: true})
	// wall-clock: not read; the cancelled login ends on the page's cancel,
	// the bound only keeps a broken page from hanging the test.
	loginOnWorld(t, w, 10*time.Second)

	stateRe := regexp.MustCompile(`name="state" value="([^"]+)"`)
	consentRe := regexp.MustCompile(`name="consent" value="([^"]+)"`)
	var mu sync.Mutex
	var pageSecrets []string // the state and the consent token, as the page carried them
	isState := map[string]bool{}
	action := "/grant"
	done := make(chan struct{}, 1)
	openBrowser = func(authURL string) error {
		go func() {
			defer func() { done <- struct{}{} }()
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
			st, tok := stateRe.FindSubmatch(b), consentRe.FindSubmatch(b)
			if st == nil || tok == nil {
				t.Errorf("the consent page carries no state or token")
				return
			}
			mu.Lock()
			pageSecrets = append(pageSecrets, string(st[1]), string(tok[1]))
			isState[string(st[1])] = true
			path := action
			mu.Unlock()
			r, err := cl.PostForm("http://"+cb.Host+path, url.Values{"state": {string(st[1])}, "consent": {string(tok[1])},
				"project": {loginworld.Project}, "audience": {"agent"}, "ttl": {"7"}})
			if err == nil {
				r.Body.Close()
			}
		}()
		return nil
	}

	login := []string{"login", "--for", "agent", "--key-ttl", "7d"}
	type call struct {
		name    string
		args    []string
		code    int
		browser bool
	}
	calls := []call{
		{"login granted", login, 0, true},
		{"whoami", []string{"--profile", "agent", "whoami"}, 0, false},
		{"whoami --json", []string{"--json", "--profile", "agent", "whoami"}, 0, false},
		{"doctor --json", []string{"--json", "--profile", "agent", "doctor"}, -1, false},
		// The copy of the journal lands in the home: the walk reads it.
		{"diag export", []string{"diag", "export", "--out", filepath.Join(home, "journal-copy.jsonl")}, 0, false},
		{"logout", []string{"--yes", "--profile", "agent", "logout"}, 0, false},
		{"login cancelled, --json", append([]string{"--json"}, login...), 1, true},
	}
	var keySecret string
	files := 0
	for _, c := range calls {
		if c.name == "login cancelled, --json" {
			mu.Lock()
			action = "/cancel"
			mu.Unlock()
		}
		code, out, errOut := runCLI(t, "", c.args...)
		if c.browser {
			<-done
		}
		if c.code >= 0 && (code == 0) != (c.code == 0) {
			t.Errorf("%s: exit %d — the call did not take the path it walks: %.300s%.300s", c.name, code, out, errOut)
		}
		if c.name == "login granted" {
			if keys := w.Keys(); len(keys) == 1 {
				keySecret = keys[0].Secret
			} else {
				t.Fatalf("login granted no key: %+v\n%s%s", keys, out, errOut)
			}
		}
		mu.Lock()
		secrets := append(append([]string(nil), w.Secrets()...), pageSecrets...)
		mu.Unlock()
		profileKept := c.name != "logout" && c.name != "login cancelled, --json"
		for _, s := range secrets {
			// The state rides in the address of the provider's page, which
			// evo prints for a browser that did not open (OAuth 2.0, the
			// front channel): the output may name it, a file may not.
			if f := leaks(out+errOut, s); f != "" && !isState[s] {
				t.Errorf("%s (exit %d): the output holds a secret of the login (%q)", c.name, code, f)
			}
			switch {
			case s == keySecret && profileKept:
				files = secretOnlyIn(t, c.name, home, s, config.CredentialsPath())
			case strings.HasPrefix(s, "satoken-") && profileKept:
				// The account's token of the key, cached for the profile
				// as after evo configure.
				files = secretOnlyIn(t, c.name, home, s, config.TokenCachePath("agent"))
			default:
				files = secretOnlyIn(t, c.name, home, s)
			}
		}
		if c.name == "login granted" {
			if b, err := os.ReadFile(config.CredentialsPath()); err != nil || !strings.Contains(string(b), keySecret) {
				t.Fatalf("credentials.yaml does not hold the key's secret (%v): the walk sees nothing", err)
			}
		}
	}
	mu.Lock()
	n := len(pageSecrets)
	mu.Unlock()
	if keySecret == "" || n != 4 || len(w.Secrets()) < 4 {
		t.Errorf("the walk saw %d secrets of the world and %d of the pages: it sees nothing", len(w.Secrets()), n)
	}
	if files < 4 {
		t.Errorf("%d files under the home: the walk sees nothing", files)
	}
}
