// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/journal"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/tools"
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
	// writes: without --dry-run the command writes a file of evo in this
	// world — the proof that under --dry-run the world reaches the point
	// of the write and «nothing changed» is the command's, not the world's
	// refusal before it (full audit 1.8, FB180-6).
	writes bool
	// world adds what the command needs to reach its write: a project the
	// key sees, a journal with a record, a Compute API of its own (its
	// base, "" for the world's).
	world func(t *testing.T, fc *fakeCloud) (compute string)
}

// localDryRuns are the local commands (localCommands) → their run under
// --dry-run. A new local command without a line here fails the test.
var localDryRuns = map[string]localDryRun{
	"evo configure":             {args: []string{"configure", "--key-id", "k2", "--secret-stdin"}, stdin: "s2\n", writes: true},
	"evo login":                 {args: []string{"login"}},
	"evo logout":                {args: []string{"logout"}, writes: true},
	"evo configure list":        {args: []string{"configure", "list"}},
	"evo configure delete":      {args: []string{"configure", "delete", "default"}, writes: true},
	"evo configure s3":          {args: []string{"configure", "s3", "--tenant-id", "t"}, writes: true},
	"evo project":               {args: []string{"project"}},
	"evo project use":           {args: []string{"project", "use", "22222222-2222-2222-2222-222222222222"}, writes: true, world: worldProjects},
	"evo init":                  {args: []string{"init", "--non-interactive"}, writes: true, world: worldZones},
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
	"evo agent setup":           {args: []string{"agent", "setup"}},
	"evo diag":                  {args: []string{"diag"}},
	"evo diag on":               {args: []string{"diag", "on"}, writes: true},
	"evo diag off":              {args: []string{"diag", "off"}},
	"evo diag status":           {args: []string{"diag", "status"}},
	"evo diag export":           {args: []string{"diag", "export"}, writes: true, world: worldJournal},
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
	// to are the hosts of Cloud.ru → the host of the test's server that
	// answers for it: evo init makes its adapter on the production base.
	to map[string]string
}

func (w *writesTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasSuffix(r.URL.Path, "/token") {
		w.mu.Lock()
		w.writes = append(w.writes, r.Method+" "+r.URL.Path)
		w.mu.Unlock()
	}
	if host, ok := w.to[r.URL.Host]; ok {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = "http", host, host
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
			got := runLocal(t, r, true)
			if len(got.changed) > 0 || len(got.writes) > 0 || got.opened || got.confirmed {
				t.Errorf("evo %s %s: exit %d; files changed %v; writes to the cloud %v; browser opened %v; consent given %v\n%.400s\n%.400s",
					strings.Join(got.flags[1:], " "), strings.Join(r.args, " "), got.code, got.changed, got.writes, got.opened, got.confirmed, got.stdout, got.stderr)
			}
		})
	}
}

// localRun is what a local command did in the world of the test.
type localRun struct {
	flags             []string
	code              int
	stdout, stderr    string
	changed, writes   []string
	opened, confirmed bool
}

// runLocal runs the local command r in the world of the test — a profile
// evo login made, an IAM here — with --yes, and under --dry-run if dryRun.
func runLocal(t *testing.T, r localDryRun, dryRun bool) localRun {
	t.Helper()
	fc := setupCLI(t)
	home := os.Getenv("HOME")
	compute := tools.ComputeBaseURL
	if r.world != nil {
		if c := r.world(t, fc); c != "" {
			compute = c
		}
	}
	// A profile evo login made: logout and configure delete have
	// a key and a profile to delete.
	if err := config.SaveProfile("default", config.Profile{ProjectID: "11111111-1111-1111-1111-111111111111"}, config.Credentials{
		KeyID: fakeKeyID, Secret: fakeSecret,
		Login: &config.LoginKey{ServiceAccountID: "sa-1", AccessKeyID: "ak-1"},
	}); err != nil {
		t.Fatal(err)
	}
	// The servers of the test are on 127.0.0.1: no proxy of the
	// environment stands between (a proxy made init fail before its write).
	direct := http.DefaultTransport.(*http.Transport).Clone()
	direct.Proxy = nil
	cu, _ := url.Parse(compute)
	tr := &writesTransport{next: direct, to: map[string]string{"compute.api.cloud.ru": cu.Host}}
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
	flags := []string{"--json", "--yes"}
	if dryRun {
		flags = []string{"--json", "--dry-run", "--yes"}
	}
	if r.noYes {
		flags = flags[:len(flags)-1]
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
	return localRun{flags: flags, code: code, stdout: stdout, stderr: stderr, changed: changed, writes: tr.writes, opened: opened, confirmed: confirmed}
}

// Full audit 1.8, FB180-6: TestLocalCommands_DryRunChangesNothing passed
// for project use, init and diag export because the world refused them
// before the point of the write — no project in the world, no zones, an
// empty journal — and «nothing changed» held by itself. A local command
// that writes (localDryRun.writes) changes a file of evo in this world
// without --dry-run: the same world under --dry-run then tests the
// command, not the world.
func TestLocalCommands_WorldReachesTheWrite(t *testing.T) {
	paths := make([]string, 0, len(localDryRuns))
	for p, r := range localDryRuns {
		if r.writes {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		r := localDryRuns[p]
		t.Run(p, func(t *testing.T) {
			got := runLocal(t, r, false)
			if len(got.changed) == 0 && len(got.writes) == 0 {
				t.Errorf("evo %s %s wrote nothing without --dry-run (exit %d): the world refuses it before the write, so its --dry-run line tests nothing\n%.400s\n%.400s",
					strings.Join(got.flags[1:], " "), strings.Join(r.args, " "), got.code, got.stdout, got.stderr)
			}
		})
	}
}

// worldProjects: the key sees the project evo project use names.
func worldProjects(_ *testing.T, fc *fakeCloud) string {
	fc.orgProject = `[{"id":"11111111-1111-1111-1111-111111111111","name":"one"},{"id":"22222222-2222-2222-2222-222222222222","name":"two"}]`
	return ""
}

// worldZones: the Compute API evo init reads — a zone and an Ubuntu image.
func worldZones(t *testing.T, _ *fakeCloud) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/availability-zones":
			fmt.Fprint(w, `[{"id":"33333333-3333-3333-3333-333333333333","name":"ru.AZ-1","enabled":true}]`)
		case "/api/v1/images":
			fmt.Fprint(w, `{"items":[{"id":"44444444-4444-4444-4444-444444444444","name":"ubuntu-22.04","status":"ready","type":"public"}],"total":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// worldJournal: a session journal with one record, for evo diag export.
func worldJournal(t *testing.T, _ *fakeCloud) string {
	p := journal.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"tool":"whoami","ms":1,"bytes":10}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return ""
}
