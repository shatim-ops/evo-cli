// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/login"
)

// localDryRun is how a local command runs under --dry-run in
// TestLocalCommands_DryRunChangesNothing: its arguments, or why it is not
// run there.
type localDryRun struct {
	args  []string
	stdin string
	skip  string
	// noYes: run without --yes, which the command refuses on its own.
	noYes bool
}

// localDryRuns are the local commands (localCommands) → their run under
// --dry-run. A new local command without a line here fails the test.
var localDryRuns = map[string]localDryRun{
	"evo configure":             {args: []string{"configure", "--key-id", "k2", "--secret-stdin"}, stdin: "s2\n"},
	"evo login":                 {args: []string{"login"}},
	"evo logout":                {args: []string{"logout"}},
	"evo configure list":        {args: []string{"configure", "list"}},
	"evo configure delete":      {args: []string{"configure", "delete", "default"}},
	"evo configure s3":          {args: []string{"configure", "s3", "--tenant-id", "t"}},
	"evo project":               {args: []string{"project"}},
	"evo project use":           {args: []string{"project", "use", "22222222-2222-2222-2222-222222222222"}},
	"evo init":                  {args: []string{"init", "--non-interactive"}},
	"evo verify":                {skip: "TCP and HTTP checks of a deployed application: no write anywhere"},
	"evo version":               {args: []string{"version"}},
	"evo completion":            {args: []string{"completion"}},
	"evo completion bash":       {args: []string{"completion", "bash"}},
	"evo completion zsh":        {args: []string{"completion", "zsh"}},
	"evo completion fish":       {args: []string{"completion", "fish"}},
	"evo completion powershell": {args: []string{"completion", "powershell"}},
	"evo mcp":                   {skip: "a server on stdio: every write is a tool behind the gate of the registry (TestReadOnly_*, D35)"},
	"evo mcp healthcheck":       {args: []string{"mcp", "healthcheck"}},
	"evo tools":                 {args: []string{"tools"}},
	"evo tools list":            {args: []string{"tools", "list"}},
	"evo tools describe":        {args: []string{"tools", "describe", "disk_delete"}},
	"evo tools call":            {skip: "a tool behind the gate of the registry, which reads --dry-run (tools.go, confirm.Options)"},
	"evo agent":                 {args: []string{"agent"}},
	"evo agent setup":           {args: []string{"agent", "setup"}},
	"evo agent skill":           {args: []string{"agent", "skill"}},
	"evo diag":                  {args: []string{"diag"}},
	"evo diag on":               {args: []string{"diag", "on"}},
	"evo diag off":              {args: []string{"diag", "off"}},
	"evo diag status":           {args: []string{"diag", "status"}},
	"evo diag export":           {args: []string{"diag", "export"}},
	"evo crew":                  {args: []string{"crew"}},
	"evo help":                  {args: []string{"help"}},
}

// treeSum is the sum of every file under root: its path, mode and content.
func treeSum(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		v := info.Mode().String()
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s := sha256.Sum256(b)
			v += " " + hex.EncodeToString(s[:])
		}
		out[rel] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// writesTransport records every request that is not a read.
type writesTransport struct {
	mu     sync.Mutex
	writes []string
	next   http.RoundTripper
}

func (w *writesTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasSuffix(r.URL.Path, "/token") {
		w.mu.Lock()
		w.writes = append(w.writes, r.Method+" "+r.URL.Path)
		w.mu.Unlock()
	}
	// The test leaves this machine for no host: only the servers of the test.
	if h := r.URL.Hostname(); h != "127.0.0.1" && h != "localhost" {
		return nil, errors.New("the test reaches no host but 127.0.0.1: " + h)
	}
	return w.next.RoundTrip(r)
}

// Release audit 1.8 (A18-3, 10.11): «evo logout --dry-run --yes» deleted
// the key in the cloud and the profile on disk — the consent of logout did
// not read --dry-run, as the consent of the tools does. --dry-run is the
// person's «show, change nothing» on every command: a local command under
// --dry-run (with --yes, which would skip the question) changes no file of
// evo — the configuration, the home, the working directory — sends no
// write to the cloud and opens no browser; it shows the plan or refuses.
// Every local command is a line of localDryRuns.
func TestLocalCommands_DryRunChangesNothing(t *testing.T) {
	for _, c := range localCommands {
		if _, ok := localDryRuns[c.Path]; !ok {
			t.Errorf("local command %q has no line in localDryRuns: how it runs under --dry-run, or why it is not run", c.Path)
		}
	}
	paths := make([]string, 0, len(localDryRuns))
	for p := range localDryRuns {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		r := localDryRuns[p]
		found := false
		for _, c := range localCommands {
			found = found || c.Path == p
		}
		if !found {
			t.Errorf("%q is not a local command any more: drop the line", p)
			continue
		}
		if r.skip != "" {
			continue
		}
		t.Run(p, func(t *testing.T) {
			setupCLI(t)
			home := os.Getenv("HOME")
			// A profile evo login made: logout and configure delete have
			// a key and a profile to delete.
			if err := config.SaveProfile("default", config.Profile{ProjectID: "11111111-1111-1111-1111-111111111111"}, config.Credentials{
				KeyID: fakeKeyID, Secret: fakeSecret,
				Login: &config.LoginKey{ServiceAccountID: "sa-1", AccessKeyID: "ak-1"},
			}); err != nil {
				t.Fatal(err)
			}
			tr := &writesTransport{next: http.DefaultTransport}
			oldClient, oldOpen, oldCfg := httpClient, openBrowser, loginConfig
			t.Cleanup(func() { httpClient, openBrowser, loginConfig = oldClient, oldOpen, oldCfg })
			httpClient = &http.Client{Transport: tr}
			opened := false
			openBrowser = func(string) error { opened = true; return nil }
			// The IAM of login and logout (iamadmin has its own client):
			// a server here that records what it is sent.
			admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					tr.mu.Lock()
					tr.writes = append(tr.writes, r.Method+" "+r.URL.Path)
					tr.mu.Unlock()
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(admin.Close)
			loginConfig = func(say func(string, string), open func(string) error) login.Config {
				c := oldCfg(say, open)
				c.HTTP = httpClient
				c.AdminBase, c.OrgBase, c.Issuer, c.KeyToken = admin.URL, admin.URL, admin.URL, admin.URL+"/token"
				return c
			}
			before := treeSum(t, home)
			flags := []string{"--json", "--dry-run", "--yes"}
			if r.noYes {
				flags = flags[:2]
			}
			code, stdout, stderr := runCLI(t, r.stdin, append(flags, r.args...)...)
			after := treeSum(t, home)
			var changed []string
			// Caches of evo are not the person's state: the token of the
			// key and the lock of the journal a reader takes.
			cache := func(f string) bool {
				return strings.Contains(f, string(filepath.Separator)+"cache") || strings.HasSuffix(f, "journal.lock")
			}
			for f, v := range after {
				if before[f] != v && !cache(f) {
					changed = append(changed, f)
				}
			}
			for f := range before {
				if _, ok := after[f]; !ok && !cache(f) {
					changed = append(changed, f+" (deleted)")
				}
			}
			sort.Strings(changed)
			// Under --dry-run the consent is never given: «confirmed» is
			// the action going on.
			confirmed := strings.Contains(stdout+stderr, `"status":"confirmed"`)
			if len(changed) > 0 || len(tr.writes) > 0 || opened || confirmed {
				t.Errorf("evo %s %s: exit %d; files changed %v; writes to the cloud %v; browser opened %v; consent given %v\n%.400s\n%.400s",
					strings.Join(flags[1:], " "), strings.Join(r.args, " "), code, changed, tr.writes, opened, confirmed, stdout, stderr)
			}
		})
	}
}
