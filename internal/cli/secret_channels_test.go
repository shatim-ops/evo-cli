// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// secretIntake are the commands that take an evo key secret, with the
// test that walks every channel for it. A command with a flag that reads a
// secret fails TestSecretChannels_EveryIntakeWalked until it is named here.
// evo login takes no secret by a flag: its walk is TestSecretChannels_Login.
var secretIntake = map[string]string{
	"configure": "TestSecretChannels_Configure",
	// The password of a PostgreSQL user: stdout, stderr, the preview and
	// errors; files and the journal are not walked — process backlog,
	// release-plan 1.8 A0-5 («затем login», the class grows to it).
	"pg user-create": "TestSecretFiles_SameArgumentAsValueFlag (output only)",
}

// notSecretFlags are flags whose name reads as a secret but which take
// none, with the reason.
var notSecretFlags = map[string]string{
	"allow-secret-export": "a policy switch of the MCP server",
	"page-token":          "a cursor of the next page of audit events",
	"embed-token":         "a switch: the kubeconfig carries the token by the user's choice, behind consent (k8s_kubeconfig)",
}

// secretFlag finds a flag that takes a secret: its value, a file or stdin.
func secretFlag(name string) bool {
	if notSecretFlags[name] != "" {
		return false
	}
	for _, w := range []string{"secret", "password", "token"} {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// secretForms are the spellings of s a leak can take: as is, base64 and
// hex (a body or a header dumped whole), URL-escaped (a query or a form).
func secretForms(s string) []string {
	return []string{
		s,
		base64.StdEncoding.EncodeToString([]byte(s)),
		base64.RawURLEncoding.EncodeToString([]byte(s)),
		hex.EncodeToString([]byte(s)),
		url.QueryEscape(s),
	}
}

// leaks names the spelling of secret in text, or "".
func leaks(text, secret string) string {
	for _, f := range secretForms(secret) {
		if strings.Contains(text, f) {
			return f
		}
	}
	return ""
}

// secretOnlyIn walks every file under root — config directory, credentials,
// token cache, journal, state of the working directory — and fails for each
// one other than allowed that holds secret in any form; after names the
// call the walk follows. It returns the number of files walked. A call
// later in the scenario can rewrite a file (the token cache), so the walk
// follows every call, not just the last.
func secretOnlyIn(t *testing.T, after, root, secret string, allowed ...string) int {
	t.Helper()
	keep := map[string]bool{}
	for _, a := range allowed {
		keep[filepath.Clean(a)] = true
	}
	n := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		if keep[filepath.Clean(p)] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if f := leaks(string(b), secret); f != "" {
			rel, _ := filepath.Rel(root, p)
			t.Errorf("after %s: file %s holds the secret (%q)", after, rel, f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A0-5 (release-plan 1.8; #563, #488): the secret of evo configure goes to
// credentials.yaml and nowhere else — not stdout, stderr, --json, the
// journal, the token cache, other files of the configuration directory or
// the state of the working directory — when IAM accepts it, refuses it and
// when a later command reads it from the environment.
func TestSecretChannels_Configure(t *testing.T) {
	fc := setupCLI(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if code, out, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: exit %d: %s%s", code, out, errOut)
	}
	const secret = "A05-only-in-credentials-7f3c9e"
	type call struct {
		name   string
		status int // IAM answer; 0 — a token
		stdin  string
		args   []string
		code   int
	}
	configure := []string{"configure", "--key-id", fakeKeyID, "--secret-stdin", "--project-id", manualProject}
	calls := []call{
		{"refused by IAM", http.StatusUnauthorized, secret + "\n", configure, clerrors.ExitAuth},
		{"refused by IAM, --json", http.StatusUnauthorized, secret + "\n", append([]string{"--json"}, configure...), clerrors.ExitAuth},
		{"accepted", 0, secret + "\n", configure, 0},
		{"accepted again, --json", 0, secret + "\n", append([]string{"--json"}, configure...), 0},
		{"whoami", 0, "", []string{"whoami"}, 0},
		{"whoami --json", 0, "", []string{"--json", "whoami"}, 0},
		{"doctor --json", 0, "", []string{"--json", "doctor"}, 0},
		// The copy of the journal lands in the working directory: the walk reads it.
		{"diag export", 0, "", []string{"diag", "export", "--out", "journal-copy.jsonl"}, 0},
	}
	files := 0
	for _, c := range calls {
		fc.iamStatus = c.status
		code, out, errOut := runCLI(t, c.stdin, c.args...)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d — the call did not take the path it walks: %.300s%.300s", c.name, code, c.code, out, errOut)
		}
		if f := leaks(out, secret); f != "" {
			t.Errorf("%s (exit %d): stdout holds the secret (%q)", c.name, code, f)
		}
		if f := leaks(errOut, secret); f != "" {
			t.Errorf("%s (exit %d): stderr holds the secret (%q)", c.name, code, f)
		}
		files = secretOnlyIn(t, c.name, home, secret, config.CredentialsPath())
	}
	if b, err := os.ReadFile(config.CredentialsPath()); err != nil || !strings.Contains(string(b), secret) {
		t.Errorf("credentials.yaml does not hold the secret (%v): the walk sees nothing", err)
	}
	if files < 4 {
		t.Errorf("%d files under the home: the walk sees nothing", files)
	}
	// The same key from the environment, refused by IAM: the error names
	// the key ID, never the secret.
	t.Setenv("CLOUDRU_KEY_ID", fakeKeyID)
	t.Setenv("CLOUDRU_SECRET", secret+"-env")
	fc.iamStatus = http.StatusUnauthorized
	for _, args := range [][]string{{"whoami"}, {"--json", "whoami"}} {
		code, out, errOut := runCLI(t, "", args...)
		if f := leaks(out+errOut, secret+"-env"); f != "" {
			t.Errorf("%v with the secret in the environment (exit %d): output holds it (%q)", args, code, f)
		}
	}
	secretOnlyIn(t, "whoami with the secret in the environment", home, secret+"-env")
}

// Every command with a flag that takes a secret is walked by a test of
// secretIntake, and every line there names a command that still takes one.
func TestSecretChannels_EveryIntakeWalked(t *testing.T) {
	found := map[string][]string{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if secretFlag(f.Name) {
				found[c.CommandPath()] = append(found[c.CommandPath()], f.Name)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	if len(found) == 0 {
		t.Fatal("no command takes a secret: the scan sees nothing")
	}
	var bad []string
	for path, flags := range found {
		name := strings.TrimPrefix(path, "evo ")
		if secretIntake[name] == "" {
			bad = append(bad, path+" (--"+strings.Join(flags, ", --")+") takes a secret: walk its channels and name the test in secretIntake")
		}
	}
	for name := range secretIntake {
		if found["evo "+name] == nil {
			bad = append(bad, "secretIntake names "+name+", which takes no secret flag: remove the line")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}
