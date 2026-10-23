// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/agentconf"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// -update-golden rewrites the .want files of testdata/agent from what
// 'evo agent setup' writes; review the diff before committing it.
var updateGolden = flag.Bool("update-golden", false, "rewrite internal/cli/testdata/agent/*/*.want")

// agentGoldenDir is absolute: the tests run in a temporary working
// directory (setupAgent).
var agentGoldenDir = func() string {
	dir, err := filepath.Abs(filepath.Join("testdata", "agent"))
	if err != nil {
		panic(err)
	}
	return dir
}()

// fakeBearerToken is an MCP_BEARER_TOKEN no file may contain.
const fakeBearerToken = "fake-bearer-token-0123456789abcdef0123456789"

// setupAgent isolates 'evo agent setup': temporary project (the working
// directory) and home (HOME, USERPROFILE and the user configuration
// directory of every OS), a fake key in the environment, no terminal. It
// returns the home directory.
func setupAgent(t *testing.T) string {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") || strings.HasPrefix(k, "EVOLUTION_") {
			t.Setenv(k, "")
		}
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	for _, d := range []string{home, work} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv(config.EnvConfigDir, filepath.Join(home, "evo-config"))
	t.Setenv("CLOUDRU_KEY_ID", fakeKeyID)
	t.Setenv("CLOUDRU_SECRET", fakeSecret)
	t.Setenv(envBearerToken, fakeBearerToken)
	t.Chdir(work)
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })
	newProcess()
	return home
}

// clientPath is the client's file of the scope, after setupAgent.
func clientPath(t *testing.T, c agentClient, s agentScope) string {
	t.Helper()
	p, err := c.config[s].path()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// readFixture reads a fixture with "\n" line endings, whatever git made of
// them.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agentGoldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// golden compares got with the fixture name (or rewrites it).
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(agentGoldenDir, name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want := readFixture(t, name); !bytes.Equal(got, want) {
		t.Errorf("%s differs (regenerate: go test ./internal/cli -run TestAgentSetup_Golden -update-golden)\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func putFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Every client with a file, every case of acceptance criterion 1 of the
// spec: an empty project, a file with other servers (kept), the same evo
// (nothing changes, exit 0), another evo (refused without --force, replaced
// with it), a broken file (refused with the line, left as it is), and the
// entry of an HTTP server. A client without a file prints its entry.
func TestAgentSetup_Golden(t *testing.T) {
	for _, c := range agentClients {
		t.Run(c.name, func(t *testing.T) {
			if len(c.config) == 0 {
				setupAgent(t)
				for name, args := range map[string][]string{"print": nil, "http": {"--http", "https://agent.example.com/mcp"}} {
					code, stdout, stderr := runCLI(t, "", append([]string{"agent", "setup", "--client", c.name}, args...)...)
					if code != 0 {
						t.Fatalf("%s: exit %d: %s", name, code, stderr)
					}
					golden(t, c.name+"/"+name+".want", []byte(stdout))
				}
				return
			}
			setup := func(t *testing.T, in []byte) string {
				setupAgent(t)
				path := clientPath(t, c, c.defaultScope())
				if in != nil {
					putFile(t, path, in, 0o600)
				}
				return path
			}
			run := func(t *testing.T, args ...string) (int, string, string) {
				return runCLI(t, "", append([]string{"agent", "setup", "--client", c.name}, args...)...)
			}

			t.Run("empty", func(t *testing.T) {
				path := setup(t, nil)
				if code, _, stderr := run(t, "--yes"); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				golden(t, c.name+"/empty.want", fileBytes(t, path))
			})
			t.Run("foreign", func(t *testing.T) {
				path := setup(t, readFixture(t, c.name+"/foreign.in"))
				if code, _, stderr := run(t, "--yes"); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				golden(t, c.name+"/foreign.want", fileBytes(t, path))
			})
			t.Run("same", func(t *testing.T) {
				in := readFixture(t, c.name+"/foreign.want")
				path := setup(t, in)
				code, stdout, stderr := run(t) // no --yes: nothing to ask
				if code != 0 || !strings.Contains(stdout, "nothing changed") {
					t.Fatalf("exit %d: %s%s", code, stdout, stderr)
				}
				if !bytes.Equal(fileBytes(t, path), in) {
					t.Error("the file changed")
				}
			})
			t.Run("other", func(t *testing.T) {
				in := readFixture(t, c.name+"/other.in")
				path := setup(t, in)
				code, _, stderr := run(t, "--yes")
				if code != clerrors.ExitGeneral || !strings.Contains(stderr, "different evo entry") || !strings.Contains(stderr, "--force") {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				if !bytes.Equal(fileBytes(t, path), in) {
					t.Fatal("the file changed without --force")
				}
				if code, _, stderr := run(t, "--yes", "--force"); code != 0 {
					t.Fatalf("--force: exit %d: %s", code, stderr)
				}
				golden(t, c.name+"/other.want", fileBytes(t, path))
			})
			t.Run("broken", func(t *testing.T) {
				in := readFixture(t, c.name+"/broken.in")
				path := setup(t, in)
				code, _, stderr := run(t, "--yes", "--force")
				if code != clerrors.ExitAuth || !strings.Contains(stderr, "line ") || !strings.Contains(stderr, "left as it is") {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				if !bytes.Equal(fileBytes(t, path), in) {
					t.Error("the broken file changed")
				}
			})
			t.Run("http", func(t *testing.T) {
				path := setup(t, nil)
				if code, _, stderr := run(t, "--yes", "--http", "http://127.0.0.1:8080/mcp"); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				golden(t, c.name+"/http.want", fileBytes(t, path))
			})
		})
	}
}

// What the files hold is what the client reads: the golden files parse as
// the client's format, with the entry under the client's servers key.
func TestAgentSetup_GoldenParse(t *testing.T) {
	for _, c := range agentClients {
		if c.format != formatJSON || len(c.config) == 0 {
			continue
		}
		for _, name := range []string{"empty", "foreign", "other", "http"} {
			data := readFixture(t, c.name+"/"+name+".want")
			if c.comments {
				// JSONC: what the file has beside the entry is checked by
				// agentconf; here the entry is read the same way.
				r, err := agentconf.JSON(data, c.servers, mcpServerName, nil, nil, true)
				if err != nil || r.State == agentconf.Added {
					t.Errorf("%s/%s: no entry (%v)", c.name, name, err)
				}
				continue
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Errorf("%s/%s: %v", c.name, name, err)
				continue
			}
			if servers, _ := doc[c.servers].(map[string]any); servers[mcpServerName] == nil {
				t.Errorf("%s/%s: no %s.%s", c.name, name, c.servers, mcpServerName)
			}
		}
	}
}

// Acceptance criterion 3: neither the Cloud.ru key nor the bearer token of
// the MCP server goes into a file or the printed entry, on any client and
// transport.
func TestAgentSetup_NoSecretInTheFile(t *testing.T) {
	for _, c := range agentClients {
		for _, http := range []bool{false, true} {
			setupAgent(t)
			args := []string{"agent", "setup", "--client", c.name, "--yes"}
			if http {
				args = append(args, "--http", "http://127.0.0.1:8080/mcp")
			}
			code, stdout, stderr := runCLI(t, "", args...)
			if code != 0 {
				t.Fatalf("%s: exit %d: %s", c.name, code, stderr)
			}
			texts := []string{stdout, stderr}
			for _, s := range c.scopes() {
				if b, err := os.ReadFile(clientPath(t, c, s)); err == nil {
					texts = append(texts, string(b))
				}
			}
			for _, text := range texts {
				for _, secret := range []string{fakeKeyID, fakeSecret, fakeBearerToken} {
					if strings.Contains(text, secret) {
						t.Errorf("%s (http %v): %q is in the output or the file:\n%s", c.name, http, secret, text)
					}
				}
			}
		}
	}
}

// Acceptance criterion 2: without a terminal and without --yes nothing is
// written (exit 3); --dry-run only shows the diff (exit 0).
func TestAgentSetup_ConsentAndDryRun(t *testing.T) {
	c, _ := lookupClient("claude-code")
	setupAgent(t)
	path := clientPath(t, c, scopeProject)

	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code")
	if code != clerrors.ExitConfirmationRequired || !strings.Contains(stderr, "+++ "+path) {
		t.Fatalf("no --yes: exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("written without consent: %v", err)
	}

	code, stdout, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--dry-run")
	if code != 0 || !strings.Contains(stdout, `+      "command": "evo",`) {
		t.Fatalf("--dry-run: exit %d: %s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("written on --dry-run: %v", err)
	}

	code, stdout, _ = runCLI(t, "", "agent", "setup", "--client", "claude-code", "--dry-run", "--json")
	var dry struct {
		Status  string
		Preview struct {
			Details struct {
				Files []struct{ Path, Status, Diff string }
			}
		}
	}
	if err := json.Unmarshal([]byte(stdout), &dry); err != nil || code != 0 || dry.Status != "dry_run" ||
		len(dry.Preview.Details.Files) != 1 || dry.Preview.Details.Files[0].Status != "created" {
		t.Fatalf("--dry-run --json: exit %d, %v: %s", code, err, stdout)
	}
}

// At a terminal the person answers; a file the client rewrote while the
// person read the diff is not overwritten.
func TestAgentSetup_Prompt(t *testing.T) {
	c, _ := lookupClient("cursor")
	setupAgent(t)
	path := clientPath(t, c, scopeProject)
	confirm.StdinIsTTY = func() bool { return true }
	oldIn := confirm.Stdin
	t.Cleanup(func() { confirm.Stdin = oldIn })

	confirm.Stdin = strings.NewReader("n\n")
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor"); code != clerrors.ExitConfirmationRequired {
		t.Fatalf("answer n: exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("written after n")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	confirm.Stdin = &rewriteOnRead{path: path, answer: "y\n"}
	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor")
	if code != clerrors.ExitGeneral || !strings.Contains(stderr, "changed while evo waited") {
		t.Fatalf("file rewritten meanwhile: exit %d: %s", code, stderr)
	}
	if got := string(fileBytes(t, path)); got != "{}" {
		t.Fatalf("the client's file was overwritten: %s", got)
	}

	confirm.Stdin = strings.NewReader("д\n")
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor"); code != 0 {
		t.Fatalf("answer д: exit %d: %s", code, stderr)
	}
	if !strings.Contains(string(fileBytes(t, path)), `"evo"`) {
		t.Fatal("not written after д")
	}
}

// rewriteOnRead is a person who answers while the client rewrites its file.
type rewriteOnRead struct {
	path, answer string
	done         bool
}

func (r *rewriteOnRead) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		if err := os.WriteFile(r.path, []byte("{}"), 0o600); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.answer)
	r.answer = r.answer[n:]
	return n, nil
}

// The mode of a file that is there is kept; a new file is 0600. A symbolic
// link is not written through.
func TestAgentSetup_ModeAndSymlink(t *testing.T) {
	c, _ := lookupClient("claude-code")
	setupAgent(t)
	path := clientPath(t, c, scopeProject)
	putFile(t, path, []byte("{}\n"), 0o644)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
			t.Errorf("mode of an existing file: %04o, want 0644", st.Mode().Perm())
		}
		user := clientPath(t, c, scopeUser)
		if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--scope", "user", "--yes"); code != 0 {
			t.Fatalf("--scope user: exit %d: %s", code, stderr)
		}
		if st, _ := os.Stat(user); st.Mode().Perm() != 0o600 {
			t.Errorf("mode of a new file: %04o, want 0600", st.Mode().Perm())
		}
	}

	target := filepath.Join(t.TempDir(), "elsewhere.json")
	putFile(t, target, []byte("{}\n"), 0o600)
	cur := clientPath(t, mustClient(t, "cursor"), scopeProject)
	if err := os.MkdirAll(filepath.Dir(cur), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cur); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor", "--yes")
	if code != clerrors.ExitAuth || !strings.Contains(stderr, "symbolic link") {
		t.Fatalf("symlink: exit %d: %s", code, stderr)
	}
	if got := string(fileBytes(t, target)); got != "{}\n" {
		t.Errorf("written through the link: %s", got)
	}
}

func mustClient(t *testing.T, name string) agentClient {
	t.Helper()
	c, ok := lookupClient(name)
	if !ok {
		t.Fatalf("no client %s", name)
	}
	return c
}

// Without a key there is nothing for the server to work with: exit 2 and
// the hint to run 'evo configure'. Printing needs no key: the entry may be
// for another machine.
func TestAgentSetup_NeedsKey(t *testing.T) {
	setupAgent(t)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--yes")
	if code != clerrors.ExitAuth || !strings.Contains(stderr, "evo configure") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if code, stdout, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--print", "json"); code != 0 || !strings.Contains(stdout, `"mcpServers"`) {
		t.Fatalf("--print: exit %d: %s%s", code, stdout, stderr)
	}
	saveProfile(t, "default", manualProject)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--yes"); code != 0 {
		t.Fatalf("with a profile: exit %d: %s", code, stderr)
	}
}

// The options reach the entry: the defaults of the owner's decisions,
// --toolsets all, --read-only either way, a profile other than the default,
// and the refusals of bad values.
func TestAgentSetup_Options(t *testing.T) {
	setupAgent(t)
	for _, tc := range []struct {
		args []string
		want string // in the printed entry
		code int
	}{
		{args: []string{"--client", "cursor", "--print", "json"}, want: `"args": [
        "mcp",
        "--toolsets",
        "deploy,vm,vpc,disk,image"
      ]`},
		{args: []string{"--client", "cursor", "--print", "toml", "--toolsets", "all"}, want: `args = ["mcp"]`},
		{args: []string{"--client", "cursor", "--print", "toml", "--read-only", "--toolsets", "VM,k8s,vm"}, want: `args = ["mcp", "--read-only", "--toolsets", "vm,k8s"]`},
		{args: []string{"--client", "gigaagent", "--read-only=false", "--toolsets", "all"}, want: `"args": [
        "mcp"
      ]`},
		{args: []string{"--profile", "staging", "--client", "codex", "--print", "toml"}, want: `args = ["--profile", "staging", "mcp", "--toolsets"`},
		{args: []string{"--client", "codex", "--print", "json"}, want: `"mcpServers": {`},
		{args: []string{"--client", "codex", "--print", "toml", "--json"}, want: `"config": "[mcp_servers.evo]\ncommand = \"evo\"`},
		{args: []string{"--client", "nope"}, want: "unknown client", code: clerrors.ExitGeneral},
		{args: nil, want: "--client is required", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--print", "yaml"}, want: "json or toml", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--toolsets", "bogus"}, want: "unknown toolset", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--http", "127.0.0.1:8080"}, want: "not an http(s) URL", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--scope", "global"}, want: "project or user", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--print", "json", "--skill"}, want: "evo agent skill", code: clerrors.ExitGeneral},
		{args: []string{"--client", "cursor", "--scope", "user", "--skill", "--yes"}, want: "no rules file", code: clerrors.ExitGeneral},
	} {
		code, stdout, stderr := runCLI(t, "", append([]string{"agent", "setup"}, tc.args...)...)
		if code != tc.code || !strings.Contains(stdout+stderr, tc.want) {
			t.Errorf("%v: exit %d, want %d and %q:\n%s%s", tc.args, code, tc.code, tc.want, stdout, stderr)
		}
	}
}

// --skill puts the rules where the client reads them, with the same
// safety: SKILL.md is all evo's; in AGENTS.md only the evo section is, and
// the person's text around it stays.
func TestAgentSetup_Skill(t *testing.T) {
	setupAgent(t)
	root := newRootCmd()
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := string(fileBytes(t, filepath.Join(".claude", "skills", "evo", "SKILL.md"))); got != agentSkill(root, false) {
		t.Errorf("SKILL.md is not 'evo agent skill':\n%s", got)
	}

	mine := "# Our project\n\nUse tabs.\n"
	putFile(t, "AGENTS.md", []byte(mine), 0o644)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor", "--skill", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := string(fileBytes(t, "AGENTS.md"))
	if !strings.HasPrefix(got, mine+"\n"+agentconf.SectionBeginPrefix) || !strings.Contains(got, agentSkillBody(root, false)) {
		t.Errorf("AGENTS.md:\n%s", got)
	}
	if code, stdout, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor", "--skill"); code != 0 || strings.Count(stdout, "nothing changed") != 2 {
		t.Errorf("again: exit %d: %s%s", code, stdout, stderr)
	}
}

const (
	agentSkillDocPath = repoRoot + "/docs/agent-skill.md"
	agentSkillHint    = "go test ./internal/cli -run TestAgentSkillDoc -update-docs"
)

// docs/agent-skill.md is what 'evo agent skill' prints (spec §2.3), as
// docs/tools.md is the registry.
func TestAgentSkillDoc(t *testing.T) {
	got := []byte(agentSkill(newRootCmd(), false))
	if *updateDocs {
		if err := os.WriteFile(agentSkillDocPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(agentSkillDocPath)
	if err != nil {
		t.Fatalf("%v; generate it: %s", err, agentSkillHint)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("docs/agent-skill.md differs from 'evo agent skill' (first difference at line %d); regenerate: %s",
			firstDiffLine(got, want), agentSkillHint)
	}
	code, stdout, _ := runCLI(t, "", "agent", "skill")
	if code != 0 || stdout != string(got) {
		t.Errorf("evo agent skill: exit %d, not the document", code)
	}
	var doc struct{ Skill string }
	code, stdout, _ = runCLI(t, "", "agent", "skill", "--json")
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 || doc.Skill != string(got) {
		t.Errorf("evo agent skill --json: exit %d, %v", code, err)
	}
}

// The rules name commands there are: the local ones are local commands,
// the examples parse with their flags.
func TestAgentSkill_CommandsExist(t *testing.T) {
	local := map[string]bool{}
	for _, lc := range localCommands {
		local[lc.Path] = true
	}
	for _, p := range skillLocalCommands {
		if !local[p] {
			t.Errorf("skillLocalCommands: %s is not in localCommands", p)
		}
	}
	root := newRootCmd()
	for _, ex := range skillExamples {
		checkExampleLine(t, root, strings.SplitN(ex, "#", 2)[0])
	}
	body := agentSkillBody(root, false)
	for _, want := range []string{"`logs`", "`s3_get`", "`evo k8s kubectl`", "`evo vm …`", "`evo deploy plan`", "| `vm` |"} {
		if !strings.Contains(body, want) {
			t.Errorf("the rules lack %s", want)
		}
	}
	if strings.Contains(body, "list-images") {
		t.Error("the rules name a hidden command")
	}
}

// Each client of the table is complete: a format, a servers key, a file or
// --print, an entry for both transports, the documentation of the format.
func TestAgentClients_Table(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range agentClients {
		if seen[c.name] || c.name == "" || c.title == "" || c.servers == "" || c.urlKey == "" {
			t.Errorf("%+v: incomplete or twice", c)
		}
		seen[c.name] = true
		if c.format != formatJSON && c.format != formatTOML {
			t.Errorf("%s: format %q", c.name, c.format)
		}
		if len(c.config) > 0 && !strings.HasPrefix(c.docs, "https://") {
			t.Errorf("%s writes a file without a link to its format", c.name)
		}
		for s := range c.skill {
			if _, ok := c.config[s]; !ok {
				t.Errorf("%s: rules of scope %s without a configuration there", c.name, s)
			}
		}
		if !slices.Contains(c.scopes(), c.defaultScope()) && len(c.scopes()) > 0 {
			t.Logf("%s: no %s file, uses %s", c.name, c.defaultScope(), c.scopes()[0])
		}
	}
}

// A file saved with a UTF-8 byte order mark (Notepad, PowerShell's
// Out-File) is read past the mark and keeps it, as a secret file does
// (B12-7); the next run finds evo there.
func TestAgentSetup_ByteOrderMark(t *testing.T) {
	setupAgent(t)
	const bom = "\uFEFF"
	for client, old := range map[string]string{
		"cursor": bom + "{\r\n  \"mcpServers\": {}\r\n}\r\n",
		"codex":  bom + "model = \"o3\"\r\n",
	} {
		path := clientPath(t, mustClient(t, client), scopeProject)
		putFile(t, path, []byte(old), 0o600)
		if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", client, "--scope", "project", "--yes"); code != 0 {
			t.Fatalf("%s: exit %d: %s", client, code, stderr)
		}
		got := string(fileBytes(t, path))
		if !strings.HasPrefix(got, bom) || strings.Count(got, bom) != 1 || !strings.Contains(got, "evo") || strings.Count(got, "\n") != strings.Count(got, "\r\n") {
			t.Errorf("%s:\n%q", client, got)
		}
		if code, stdout, stderr := runCLI(t, "", "agent", "setup", "--client", client, "--scope", "project", "--yes"); code != 0 || !strings.Contains(stdout, "nothing changed") {
			t.Errorf("%s again: exit %d: %s%s", client, code, stdout, stderr)
		}
	}
}

// Without --scope a client gets the file it reads by default: the
// project's (D22), except Codex, which reads .codex/config.toml of a
// trusted project only, so its default is the user's file.
func TestAgentSetup_DefaultScope(t *testing.T) {
	for client, want := range map[string]agentScope{"claude-code": scopeProject, "cursor": scopeProject, "vscode": scopeProject, "gemini": scopeProject, "codex": scopeUser} {
		setupAgent(t)
		c := mustClient(t, client)
		if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", client, "--yes"); code != 0 {
			t.Fatalf("%s: exit %d: %s", client, code, stderr)
		}
		for _, s := range c.scopes() {
			_, err := os.Stat(clientPath(t, c, s))
			if written := err == nil; written != (s == want) {
				t.Errorf("%s: %s file written=%v, want the %s file", client, s, written, want)
			}
		}
	}
}

// The keys the user adds to evo's entry (env, a timeout) are theirs: the
// next run finds evo unchanged, and --force replaces evo's keys only.
func TestAgentSetup_KeepsUsersKeys(t *testing.T) {
	for client, users := range map[string][2]string{
		"claude-code": {`{"mcpServers": {"evo": {"command": "evo", "args": ["mcp", "--toolsets", "deploy,vm,vpc,disk,image"], "env": {"CLOUDRU_PROFILE": "x"}, "timeout": 60000}}}`, `"timeout":60000`},
		"codex":       {"[mcp_servers.evo]\ncommand = \"evo\"\nargs = [\"mcp\", \"--toolsets\", \"deploy,vm,vpc,disk,image\"]\ntool_timeout_sec = 600\n\n[mcp_servers.evo.env]\nCLOUDRU_PROFILE = \"x\"\n", "tool_timeout_sec = 600"},
	} {
		setupAgent(t)
		c := mustClient(t, client)
		path := clientPath(t, c, c.defaultScope())
		putFile(t, path, []byte(users[0]), 0o600)
		if code, stdout, stderr := runCLI(t, "", "agent", "setup", "--client", client); code != 0 || !strings.Contains(stdout, "nothing changed") {
			t.Fatalf("%s: exit %d: %s%s", client, code, stdout, stderr)
		}
		if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", client, "--toolsets", "k8s", "--force", "--yes"); code != 0 {
			t.Fatalf("%s --force: exit %d: %s", client, code, stderr)
		}
		got := string(fileBytes(t, path))
		if !strings.Contains(got, "CLOUDRU_PROFILE") || !strings.Contains(got, users[1]) || !strings.Contains(got, `"k8s"`) || strings.Contains(got, "deploy,vm") {
			t.Errorf("%s after --force:\n%s", client, got)
		}
	}
}

// The rules are evo's: after an upgrade the next run refreshes them without
// --force (with the diff and consent all the same), and the rules name the
// version that wrote them. Another evo entry of the client's file still
// needs --force, and then nothing is written.
func TestAgentSetup_SkillRefresh(t *testing.T) {
	setupAgent(t)
	oldVersion := version
	t.Cleanup(func() { version = oldVersion })
	version = "1.3.0"
	if !strings.Contains(agentSkill(newRootCmd(), false), "evo 1.3.0") {
		t.Errorf("the rules do not name the version:\n%s", agentSkill(newRootCmd(), false))
	}
	skill := filepath.Join(".claude", "skills", "evo", "SKILL.md")
	putFile(t, skill, []byte("---\nname: evo\n---\n# rules of evo 1.2.0\n"), 0o600)
	putFile(t, "AGENTS.md", []byte("# Mine\n\n"+agentconf.SectionBeginPrefix+" -->\nold rules\n<!-- END evo -->\n"), 0o600)
	for _, client := range []string{"claude-code", "cursor"} {
		if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", client, "--skill", "--yes"); code != 0 {
			t.Fatalf("%s: exit %d: %s", client, code, stderr)
		}
	}
	if got := string(fileBytes(t, skill)); got != agentSkill(newRootCmd(), false) {
		t.Errorf("SKILL.md not refreshed:\n%s", got)
	}
	if got := string(fileBytes(t, "AGENTS.md")); strings.Contains(got, "old rules") || !strings.HasPrefix(got, "# Mine\n") {
		t.Errorf("AGENTS.md not refreshed:\n%s", got)
	}

	version = "1.3.1"
	before := fileBytes(t, skill)
	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill", "--toolsets", "k8s", "--yes")
	if code != clerrors.ExitGeneral || !strings.Contains(stderr, "--force") || !bytes.Equal(fileBytes(t, skill), before) {
		t.Errorf("another evo entry: exit %d, rules written=%v: %s", code, !bytes.Equal(fileBytes(t, skill), before), stderr)
	}
}

// --read-only bounds the server only: an agent with a terminal changed the
// cloud with the CLI instead (live check of #217, 23.10). The rules of a
// read-only agent forbid every change, through the CLI too, and drop the
// example with --yes; the setup says the boundary is a read-only key.
// Without --read-only the rules and the hints stay as they were.
func TestAgentSetup_ReadOnlyRules(t *testing.T) {
	setupAgent(t)
	root := newRootCmd()
	ro, rw := agentSkill(root, true), agentSkill(root, false)
	for _, want := range []string{"## This agent is read-only", "CLI", "terminal", "viewer"} {
		if !strings.Contains(ro, want) {
			t.Errorf("read-only rules lack %q:\n%s", want, ro)
		}
	}
	if strings.Contains(ro, "--yes --json") {
		t.Errorf("read-only rules show a change with --yes:\n%s", ro)
	}
	if strings.Contains(rw, "## This agent is read-only") || !strings.Contains(rw, "--yes --json") {
		t.Errorf("rules without --read-only changed:\n%s", rw)
	}
	if code, stdout, _ := runCLI(t, "", "agent", "skill", "--read-only"); code != 0 || stdout != ro {
		t.Errorf("evo agent skill --read-only: exit %d, not the read-only rules", code)
	}

	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--read-only", "--skill", "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := string(fileBytes(t, filepath.Join(".claude", "skills", "evo", "SKILL.md"))); got != ro {
		t.Errorf("SKILL.md of a read-only setup is not the read-only rules:\n%s", got)
	}
	if !strings.Contains(stderr, "only the MCP server") || !strings.Contains(stderr, "viewer") {
		t.Errorf("no warning that --read-only bounds the server only: %s", stderr)
	}
	if code, _, stderr = runCLI(t, "", "agent", "setup", "--client", "cursor", "--yes"); code != 0 || strings.Contains(stderr, "only the MCP server") {
		t.Errorf("without --read-only: exit %d: %s", code, stderr)
	}
}

// fakeEvo puts an executable named evo in a directory of its own and makes
// that directory the PATH; target, when set, is linked instead of a script.
func fakeEvo(t *testing.T, target string) string {
	t.Helper()
	dir := t.TempDir()
	name := "evo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if target != "" {
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("no symbolic links here: %v", err)
		}
	} else {
		putFile(t, path, []byte("#!/bin/sh\necho evo 1.2.1\n"), 0o755)
	}
	t.Setenv("PATH", dir)
	return path
}

// The client starts evo from its PATH. Another evo there (an older one from
// install.sh before the one that ran the setup) would serve the agent
// silently (live check of #217, 23.10): the setup names both. The evo that
// runs is not warned about.
func TestAgentSetup_AnotherEvoInPath(t *testing.T) {
	setupAgent(t)
	other := fakeEvo(t, "")
	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--yes")
	if code != 0 || !strings.Contains(stderr, other) || !strings.Contains(stderr, "not this evo") {
		t.Errorf("another evo in PATH: exit %d: %s", code, stderr)
	}

	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	fakeEvo(t, self)
	code, _, stderr = runCLI(t, "", "agent", "setup", "--client", "cursor", "--yes")
	if code != 0 || strings.Contains(stderr, "not this evo") || strings.Contains(stderr, "not in PATH") {
		t.Errorf("this evo in PATH: exit %d: %s", code, stderr)
	}
}

// The rules are a file of the project like any other: a new one is 0644.
// The client's file stays 0600 when new: the user may add a token to it.
func TestAgentSetup_RulesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	setupAgent(t)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "cursor", "--skill", "--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(".claude", "skills", "evo", "SKILL.md"): 0o644,
		"AGENTS.md":                          0o644,
		".mcp.json":                          0o600,
		filepath.Join(".cursor", "mcp.json"): 0o600,
	} {
		if st, err := os.Stat(path); err != nil || st.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %04o (%v)", path, st.Mode().Perm(), want, err)
		}
	}
}
