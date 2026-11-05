// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// evo login and evo logout on the world of B-2: the key the person grants
// is kept in the profile with what logout needs (R-25), no secret reaches
// the output, and logout deletes that key by its id with its own token.
func TestLoginLogout_OnTheWorld(t *testing.T) {
	setupCLI(t)
	w := loginworld.New(t, loginworld.Variant{})
	oldCfg, oldOpen, oldIAM := loginConfig, openBrowser, auth.IAMEndpoint
	t.Cleanup(func() { loginConfig, openBrowser, auth.IAMEndpoint = oldCfg, oldOpen, oldIAM })
	auth.IAMEndpoint = w.URL + "/api/v1/auth/token"
	loginConfig = func(say func(string, string), open func(string) error) login.Config {
		c := login.Default(say, open)
		c.Issuer, c.OrgBase, c.AdminBase, c.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
		c.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
		c.HTTP = w.Client()
		return c
	}
	state := regexp.MustCompile(`name="state" value="([^"]+)"`)
	openBrowser = func(authURL string) error {
		go func() {
			c := w.Client()
			resp, err := c.Get(authURL)
			if err != nil {
				return
			}
			resp.Body.Close()
			cb, _ := url.Parse(resp.Header.Get("Location"))
			resp, err = c.Get(cb.String())
			if err != nil {
				return
			}
			page, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			m := state.FindSubmatch(page)
			if m == nil {
				return
			}
			// The flags chose the agent and 7 days: the page offers them
			// checked, and the person grants.
			if !strings.Contains(string(page), `value="agent" checked`) || !strings.Contains(string(page), `value="7" checked`) {
				t.Errorf("the page does not carry the choice of the flags")
			}
			r, err := c.PostForm("http://"+cb.Host+"/grant", url.Values{"state": {string(m[1])},
				"project": {loginworld.Project}, "audience": {"agent"}, "ttl": {"7"}})
			if err == nil {
				r.Body.Close()
			}
		}()
		return nil
	}

	code, stdout, stderr := runCLI(t, "", "--json", "login", "--for", "agent", "--key-ttl", "7d")
	if code != 0 {
		t.Fatalf("login: exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, s := range w.Secrets() {
		if strings.Contains(stdout+stderr, s) {
			t.Fatalf("a secret reached the output: %q", s)
		}
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || got["service_account"] != "evo-agent" || got["project_id"] != loginworld.Project ||
		got["profile"] != "agent" || got["reused_account"] != false || got["revoked_key_id"] != nil || got["project_name"] == "" {
		t.Fatalf("answer %q: %v", stdout, err)
	}
	// A key for an agent goes to the profile «agent», not the person's.
	if _, _, found, _ := config.LoadProfile("default"); found {
		t.Errorf("the key for an agent went to the profile default")
	}
	_, creds, _, err := config.LoadProfile("agent")
	keys := w.Keys()
	if err != nil || creds.Login == nil || len(keys) != 1 || creds.Login.AccessKeyID != keys[0].ID || creds.KeyID != keys[0].KeyID ||
		creds.Secret != keys[0].Secret || creds.Login.KeyExpiresAt == "" {
		t.Fatalf("profile %+v (%v); keys %+v", creds.Login, err, keys)
	}

	code, stdout, stderr = runCLI(t, "", "--json", "--yes", "--profile", "agent", "logout")
	if code != 0 || !strings.Contains(stdout, `"key": "deleted"`) {
		t.Fatalf("logout: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if len(w.Keys()) != 0 {
		t.Errorf("logout left keys %+v", w.Keys())
	}
	if _, _, found, _ := config.LoadProfile("agent"); found {
		t.Errorf("logout kept the profile")
	}
	// The account and its role stay (contract B-1, section 3).
	if len(w.Accounts()) != 1 || len(w.Permissions()) != 1 {
		t.Errorf("logout touched the account or its role: %+v %+v", w.Accounts(), w.Permissions())
	}
}

// A flag the login does not offer is USAGE before any request.
func TestLogin_FlagsRefusedBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{{"--key-ttl", "365d"}, {"--key-ttl", "0"}, {"--for", "root"}} {
		if _, err := loginChoice("", argOf(args, "--for"), argOf(args, "--key-ttl")); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if c, err := loginChoice("p", "agent", "90d"); err != nil || c.Days != 90 || c.For != login.ForAgent || c.Project != "p" {
		t.Errorf("%+v %v", c, err)
	}
}

// The profile of the key: the one --profile or CLOUDRU_PROFILE names, else
// «agent» for an agent and the default one for the person.
func TestLogin_ProfileOfTheAudience(t *testing.T) {
	if got := (loginStore{}).Profile(login.ForAgent); got != "agent" {
		t.Errorf("agent: %q", got)
	}
	if got := (loginStore{}).Profile(login.ForMe); got != config.DefaultProfile {
		t.Errorf("me: %q", got)
	}
	if got := (loginStore{name: "work"}).Profile(login.ForAgent); got != "work" {
		t.Errorf("named: %q", got)
	}
}

func argOf(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// evo agent setup warns when the profile's key was made by evo login: its
// role may delete (release plan 1.8, B-3); no word for any other key.
func TestAgentSetup_WarnsOfTheLoginKey(t *testing.T) {
	if w := loginKeyWarning("default", config.Credentials{KeyID: "k"}); w != "" {
		t.Errorf("a key of evo configure warned: %q", w)
	}
	w := loginKeyWarning("default", config.Credentials{KeyID: "k", Login: &config.LoginKey{AccessKeyID: "ak"}})
	if !strings.Contains(w, "evo login") || !strings.Contains(w, auth.LoginRoleDisplayName) {
		t.Errorf("warning %q", w)
	}
}
