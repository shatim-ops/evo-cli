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
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// evo login and evo logout on the world of B-2: the key the person grants
// is kept in the profile with what logout needs (R-25), no secret reaches
// the output, and logout deletes that key by its id with its own token.
// grantingWorld points evo login at the world of B-2 in variant v with a
// person who grants the choice of the flags --for <aud> --key-ttl 7d.
func grantingWorld(t *testing.T, v loginworld.Variant, aud login.Audience) *loginworld.World {
	t.Helper()
	v.AnyLoopbackPort = true
	w := loginworld.New(t, v)
	oldCfg, oldOpen, oldIAM := loginConfig, openBrowser, auth.IAMEndpoint
	t.Cleanup(func() { loginConfig, openBrowser, auth.IAMEndpoint = oldCfg, oldOpen, oldIAM })
	auth.IAMEndpoint = w.URL + "/api/v1/auth/token"
	loginConfig = func(say func(string, string), open func(string) error) login.Config {
		c := login.Default(say, open)
		c.Issuer, c.OrgBase, c.AdminBase, c.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
		c.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
		c.HTTP = w.Client()
		c.Ports = []int{0} // no test takes a registered port
		return c
	}
	state := regexp.MustCompile(`name="state" value="([^"]+)"`)
	consent := regexp.MustCompile(`name="consent" value="([^"]+)"`)
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
			m, tok := state.FindSubmatch(page), consent.FindSubmatch(page)
			if m == nil || tok == nil {
				return
			}
			// The flags chose whom and 7 days: the page offers them
			// checked, and the person grants.
			if !strings.Contains(string(page), `value="`+string(aud)+`" checked`) || !strings.Contains(string(page), `value="7" checked`) {
				t.Errorf("the page does not carry the choice of the flags")
			}
			r, err := c.PostForm("http://"+cb.Host+"/grant", url.Values{"state": {string(m[1])}, "consent": {string(tok[1])},
				"project": {loginworld.Project}, "audience": {string(aud)}, "ttl": {"7"}})
			if err == nil {
				r.Body.Close()
			}
		}()
		return nil
	}
	return w
}

func TestLoginLogout_OnTheWorld(t *testing.T) {
	setupCLI(t)
	w := grantingWorld(t, loginworld.Variant{}, login.ForAgent)

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
	// The tokens the key was exchanged for live on: logout says until when.
	if code != 0 || !strings.Contains(stdout, `"key": "deleted"`) || !strings.Contains(stdout, `"tokens_valid_until"`) {
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
	// The text result after a login that found the account: the role's
	// term, the account reused, the check; no previous key — logout
	// deleted it.
	code, stdout, stderr = runCLI(t, "", "login", "--for", "agent", "--key-ttl", "7d")
	if code != 0 {
		t.Fatalf("login again: exit %d\n%s\n%s", code, stdout, stderr)
	}
	role := login.TResultRole(time.Now(), false) // the date is the world's: the line without it
	for _, want := range []string{role[:len(role)-len("02.01.2006")], login.TResultReused("evo-agent", ""), login.TResultCheck("agent")} {
		if !strings.Contains(stdout+stderr, want) {
			t.Errorf("no %q in the result:\n%s\n%s", want, stdout, stderr)
		}
	}
}

// evo login takes no --yes: access is the page's «Grant access» alone
// («Риск», conclusion 24); refused before any request.
func TestLogin_RefusesYes(t *testing.T) {
	setupCLI(t)
	called := false
	oldOpen := openBrowser
	t.Cleanup(func() { openBrowser = oldOpen })
	openBrowser = func(string) error { called = true; return nil }
	code, stdout, stderr := runCLI(t, "", "--json", "--yes", "login", "--project", "p", "--for", "agent", "--key-ttl", "7d")
	if code != 1 || !strings.Contains(stderr+stdout, `"reason":"USAGE"`) || called {
		t.Fatalf("exit %d, browser %v\n%s\n%s", code, called, stdout, stderr)
	}
}

// A flag the login does not offer is USAGE before any request.
func TestLogin_FlagsRefusedBeforeAnyRequest(t *testing.T) {
	// «7» without «d» too (plan 1.8, #1087, audit of 06.11): the flag is 7d.
	for _, args := range [][]string{{"--key-ttl", "365d"}, {"--key-ttl", "0"}, {"--key-ttl", "7"}, {"--key-ttl", "30"}, {"--for", "root"}} {
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

// R-48: the preselected project is CLOUDRU_PROJECT_ID, else the project of
// the profile of now, each with its source.
func TestLogin_PreselectSource(t *testing.T) {
	setupCLI(t)
	t.Setenv("CLOUDRU_PROJECT_ID", "")
	if id, from, _ := loginPreselect(""); id != "" || from != "" {
		t.Errorf("no profile, no variable: %q %q", id, from)
	}
	if err := config.SaveProfile("default", config.Profile{ProjectID: "p-prof"}, config.Credentials{}); err != nil {
		t.Fatal(err)
	}
	if id, from, env := loginPreselect(""); id != "p-prof" || from != login.TFromProfile("default") || env {
		t.Errorf("profile: %q %q", id, from)
	}
	t.Setenv("CLOUDRU_PROJECT_ID", "p-env")
	if id, from, env := loginPreselect(""); id != "p-env" || from != login.TFromEnv() || !env {
		t.Errorf("variable: %q %q", id, from)
	}
}

// Plan 1.8, #1087 (audit of 06.11): two evo login at once lived on two
// ports, and the person could finish the wrong one (R-47). While one waits
// the other refuses before the browser.
func TestLogin_OneAtATime(t *testing.T) {
	setupCLI(t)
	unlock, err := state.FileLock(filepath.Join(config.Dir(), "login.lock"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	called := false
	oldOpen := openBrowser
	t.Cleanup(func() { openBrowser = oldOpen })
	openBrowser = func(string) error { called = true; return nil }
	code, stdout, stderr := runCLI(t, "", "--json", "login")
	if code != 1 || !strings.Contains(stderr+stdout, `"reason":"STATE_LOCKED"`) || !strings.Contains(stderr+stdout, "evo login") || called {
		t.Fatalf("exit %d, browser %v\n%s\n%s", code, called, stdout, stderr)
	}
	// The lock is the configuration directory's, not the computer's: the
	// refusal names the directory (findings ledger 1.8, № 131).
	dir, _ := json.Marshal(config.Dir())
	if strings.Contains(stderr+stdout, "this computer") || !strings.Contains(stderr+stdout, strings.Trim(string(dir), `"`)) {
		t.Errorf("the refusal does not name the configuration directory:\n%s\n%s", stdout, stderr)
	}
}

// Release plan 1.8, «B-5, дефекты флагов и параллельного входа» (а) (Grok
// audit 06.11, #1087): --key-ttl 7 without «d» was taken and opened the
// browser; the term is days with «d» (7d, 30d, 90d), and a bare number is
// USAGE before any request, as 0d and abc are.
func TestLogin_KeyTTLWithoutUnitRefused(t *testing.T) {
	for _, ttl := range []string{"7", "30", "90", "0d", "abc"} {
		_, err := loginChoice("", "", ttl)
		if err == nil {
			t.Errorf("--key-ttl %s accepted: the term is days with «d»", ttl)
			continue
		}
		if r := clerrors.Reason(err); r != clerrors.CodeUsage {
			t.Errorf("--key-ttl %s: %s, want USAGE", ttl, r)
		}
	}
	for _, ttl := range []string{"7d", "30d", "90d"} {
		if _, err := loginChoice("", "", ttl); err != nil {
			t.Errorf("--key-ttl %s refused: %v", ttl, err)
		}
	}
}

// worldNoPerson points evo login at the world of B-2 with a browser that
// only records it was opened; nobody comes, the wait is short.
func worldNoPerson(t *testing.T) *bool {
	t.Helper()
	w := loginworld.New(t, loginworld.Variant{AnyLoopbackPort: true})
	oldCfg, oldOpen := loginConfig, openBrowser
	t.Cleanup(func() { loginConfig, openBrowser = oldCfg, oldOpen })
	loginConfig = func(say func(string, string), open func(string) error) login.Config {
		c := login.Default(say, open)
		c.Issuer, c.OrgBase, c.AdminBase, c.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
		c.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
		c.HTTP = w.Client()
		c.Ports = []int{0}
		c.Wait = 200 * time.Millisecond // wall-clock: a share of the production wait, to end the test
		return c
	}
	called := new(bool)
	openBrowser = func(string) error { *called = true; return nil }
	return called
}

// Plan 1.8, #1095, rounds 1–2 of the audit (Grok): what evo login can tell
// before the browser it tells — and opens no browser.
func TestLogin_RefusedBeforeTheBrowser(t *testing.T) {
	for _, c := range []struct {
		name   string
		env    map[string]string
		args   []string
		reason string
	}{
		{"--dry-run: no preview of a consent", nil, []string{"--dry-run", "login"}, clerrors.CodeUsage},
		{"--for ''", nil, []string{"login", "--for", ""}, clerrors.CodeUsage},
		{"--project of spaces", nil, []string{"login", "--project", "   "}, clerrors.CodeUsage},
		{"--project with a line break", nil, []string{"login", "--project", "a\nb"}, clerrors.CodeUsage},
		{"--project too long", nil, []string{"login", "--project", strings.Repeat("p", 300)}, clerrors.CodeUsage},
		{"--project-id, even a UUID: the project is --project", nil, []string{"login", "--project-id", "00000000-0000-4000-8000-000000000001"}, clerrors.CodeUsage},
		{"--region: login grants no zone", nil, []string{"--region", "ru.AZ-2", "login"}, clerrors.CodeUsage},
		{"--project-id '': named, so not skipped", nil, []string{"login", "--project-id", ""}, clerrors.CodeUsage},
		{"the key «for me» into the profile agent (C24)", map[string]string{"CLOUDRU_PROFILE": "agent"}, []string{"login", "--for", "me"}, clerrors.CodeUsage},
	} {
		t.Run(c.name, func(t *testing.T) {
			setupCLI(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			called := worldNoPerson(t)
			code, stdout, stderr := runCLI(t, "", append([]string{"--json"}, c.args...)...)
			if code == 0 || !strings.Contains(stderr+stdout, `"reason":"`+c.reason+`"`) || *called {
				t.Fatalf("exit %d, browser %v\n%s\n%s", code, *called, stdout, stderr)
			}
		})
	}
}

// Plan 1.8, #1095 (б): a configuration directory evo cannot write to is
// found before the browser, not after the key is made.
func TestLogin_UnwritableConfigDirBeforeTheBrowser(t *testing.T) {
	setupCLI(t)
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfigDir, filepath.Join(file, "evo"))
	called := worldNoPerson(t)
	code, stdout, stderr := runCLI(t, "", "--json", "login")
	if code != clerrors.ExitAuth || !strings.Contains(stderr+stdout, `"reason":"CONFIG_INVALID"`) || *called {
		t.Fatalf("exit %d, browser %v\n%s\n%s", code, *called, stdout, stderr)
	}
}

// Plan 1.8, #1095 (и): a key already in the profile, or keys in the
// environment, are named before the browser.
func TestLogin_WarnsOfAKeyInTheProfileAndTheEnvironment(t *testing.T) {
	ru(t)
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	t.Setenv("CLOUDRU_KEY_ID", "k")
	t.Setenv("CLOUDRU_SECRET", "s")
	worldNoPerson(t)
	_, stdout, stderr := runCLI(t, "", "login", "--for", "me")
	out := stdout + stderr
	for _, want := range []string{"уже есть ключ доступа", "важнее профиля"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q before the browser:\n%s", want, out)
		}
	}
}

// Plan 1.8, #1095 (о): evo login takes no --yes, and its help does not
// offer one.
func TestLogin_HelpOffersNoYes(t *testing.T) {
	setupCLI(t)
	_, stdout, _ := runCLI(t, "", "login", "--help")
	// The text says «there is no --yes»: the flag's own line is what must go.
	if strings.Contains(stdout, "-y, --yes") {
		t.Errorf("the help of evo login offers --yes:\n%s", stdout)
	}
	_, stdout, _ = runCLI(t, "", "vm", "list", "--help")
	if !strings.Contains(stdout, "-y, --yes") {
		t.Errorf("--yes is gone from the help of the other commands too")
	}
}

// Plan 1.8, #1095 (п), round 2: with --for agent the page offered the
// project of the profile default, while the key goes to the profile agent
// with another project. The project picked beforehand is the one of the
// profile the key goes to.
func TestLogin_PreselectFromTheProfileOfTheKey(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-default")
	saveProfile(t, agentProfile, "p-agent")
	if id, _, _ := loginPreselectFor(loginStore{}, login.ForAgent); id != "p-agent" {
		t.Errorf("--for agent: preselected %q, want the project of the profile agent", id)
	}
	if id, _, _ := loginPreselectFor(loginStore{}, login.ForMe); id != "p-default" {
		t.Errorf("--for me: preselected %q, want the project of the profile default", id)
	}
	if id, _, _ := loginPreselectFor(loginStore{}, ""); id != "p-default" {
		t.Errorf("no --for: preselected %q, want the project of «Мне», the default of the page", id)
	}
}

// A key the console revoked: IAM no longer exchanges it, so nothing can be
// revoked with it, and the profile with a dead key helps nobody. logout
// deletes the profile and says what it could not check, not an error with
// the profile kept (plan 1.8, B-5 round 3 (в); audit #1104).
func TestLogout_KeyIAMRejectsDeletesTheProfile(t *testing.T) {
	setupCLI(t)
	w := loginworld.New(t, loginworld.Variant{AnyLoopbackPort: true})
	oldCfg, oldIAM := loginConfig, auth.IAMEndpoint
	t.Cleanup(func() { loginConfig, auth.IAMEndpoint = oldCfg, oldIAM })
	auth.IAMEndpoint = w.URL + "/api/v1/auth/token"
	loginConfig = func(say func(string, string), open func(string) error) login.Config {
		c := login.Default(say, open)
		c.AdminBase = w.URL
		return c
	}
	creds := config.Credentials{}
	creds.ForKey("revoked-key-0001", "secret-0001")
	creds.Login = &config.LoginKey{ServiceAccountID: "sa-1", AccessKeyID: "ak-1"}
	if err := config.SaveProfile("default", config.Profile{ProjectID: loginworld.Project}, creds); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI(t, "", "--json", "--yes", "logout")
	// Both ids, as when a key was not deleted: the console lists the key
	// by its id in IAM, the preview named its Key ID («Риск», conclusion 42).
	if code != 0 || !strings.Contains(stdout, `"key": "rejected"`) || !strings.Contains(stdout, `"note"`) ||
		!strings.Contains(stdout, `"access_key_id": "ak-1"`) || !strings.Contains(stdout, `"key_id": "revo****0001"`) ||
		!strings.Contains(stdout, "ak-1)") {
		t.Fatalf("logout of a revoked key: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if _, _, found, _ := config.LoadProfile("default"); found {
		t.Errorf("logout kept the profile of a key IAM rejects")
	}
	if strings.Contains(stdout+stderr, "secret-0001") {
		t.Errorf("the secret reached the output")
	}
}

// A18-26, «Риск», conclusion 59: an unbounded role the account evo held is
// named so on the CLI — role_unbounded in --json without role_expires_at,
// «with no end date» in the terminal, not the key's term.
func TestLogin_UnboundedRole(t *testing.T) {
	setupCLI(t)
	grantingWorld(t, loginworld.Variant{Perm: loginworld.PermForever}, login.ForMe)
	code, stdout, stderr := runCLI(t, "", "--json", "login", "--for", "me", "--key-ttl", "7d")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil {
		t.Fatalf("login: exit %d (%v)\n%s\n%s", code, err, stdout, stderr)
	}
	if _, has := got["role_expires_at"]; has || got["role_unbounded"] != true || got["reused_account"] != true {
		t.Errorf("an unbounded role in --json: %s", stdout)
	}

	setupCLI(t)
	grantingWorld(t, loginworld.Variant{Perm: loginworld.PermForever}, login.ForMe)
	code, stdout, stderr = runCLI(t, "", "login", "--for", "me", "--key-ttl", "7d")
	if code != 0 || !strings.Contains(stdout+stderr, login.TResultRole(time.Time{}, true)) {
		t.Errorf("an unbounded role in the terminal: exit %d\n%s\n%s", code, stdout, stderr)
	}
}
