// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// tabShellPending are the parts of Tab in the shell that are not there yet:
// the part → what is missing. It only shrinks. Empty: its deadline 1.7 is
// met, and a new line comes with a deadline of its own and its plan line
// (release plan 1.7, block B, Tab, #718: T4, T7).
var tabShellPending = map[string]string{}

// zshMenu is the style T4 sets, and zshMenuGuard the read of the person's
// own one it is set under.
const (
	zshMenu      = `zstyle ':completion:*:*:evo:*' menu yes select`
	zshMenuGuard = `zstyle -L ':completion:*:*:evo:*' menu`
)

// T4 (#718, the owner's dogfood of 31.10; process plan 1.7 §5): the
// script of evo completion zsh sets the menu of the candidates for evo —
// the arrows choose, as in bash's Tab Tab — only when the person has no
// style of their own for it (zstyle -L reads it).
func TestTab_ZshScriptSetsTheMenuUnlessThePersonHasOne(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"completion", "zsh"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	script := out.String()
	set := strings.Index(script, zshMenu)
	guard := strings.Index(script, zshMenuGuard)
	ok := set >= 0 && guard >= 0 && guard < set
	_, pending := tabShellPending["zsh menu"]
	switch {
	case !ok && !pending:
		t.Errorf("evo completion zsh: want %q under a check that the person has none (%q before it)", zshMenu, zshMenuGuard)
	case ok && pending:
		t.Error("evo completion zsh sets the menu now: take \"zsh menu\" out of tabShellPending (the list only shrinks)")
	}
}

// fakeEvos are the fake evo built for the package, by version: go build
// takes seconds on a Windows runner, so each version is built once and
// copied into the directory a test puts in PATH. TestMain removes the
// directory (removeFakeEvos).
var fakeEvos struct {
	sync.Mutex
	dir   string
	built map[string]string
}

// removeFakeEvos removes the fake evo the package built.
func removeFakeEvos() {
	fakeEvos.Lock()
	defer fakeEvos.Unlock()
	if fakeEvos.dir != "" {
		_ = os.RemoveAll(fakeEvos.dir)
	}
}

// fakeEvoName is the file name PATH resolves as evo: evo.exe on Windows,
// as PATHEXT finds it.
func fakeEvoName() string {
	if runtime.GOOS == "windows" {
		return "evo.exe"
	}
	return "evo"
}

// builtFakeEvo builds, once per package, an evo stamped with version v
// (-ldflags -X main.version=v), and returns its path. Built with -trimpath,
// as a release is, its build information records no -ldflags: a release's
// version is the main module's, from its git tag, which this module
// outside git has not.
func builtFakeEvo(t *testing.T, v string, trimpath bool) string {
	t.Helper()
	fakeEvos.Lock()
	defer fakeEvos.Unlock()
	key := v
	if trimpath {
		key += "-trimpath"
	}
	if p, ok := fakeEvos.built[key]; ok {
		return p
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		// T7 has no other cover: on CI a missing go fails the run instead
		// of dropping the check without a word (as 753e04ff).
		if os.Getenv("CI") != "" {
			t.Fatal("no go on the CI runner: a fake evo cannot be built, T7 is not tested")
		}
		t.Skip("no go in PATH: a fake evo cannot be built")
	}
	if fakeEvos.dir == "" {
		if fakeEvos.dir, err = os.MkdirTemp("", "fakeevo"); err != nil {
			t.Fatal(err)
		}
		fakeEvos.built = map[string]string{}
	}
	src := filepath.Join(fakeEvos.dir, "src")
	for name, body := range map[string]string{
		"go.mod":  "module example.com/fakeevo\n\ngo 1.21\n",
		"main.go": "package main\n\nvar version = \"dev\"\n\nfunc main() { println(version) }\n",
	} {
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(fakeEvos.dir, key, fakeEvoName())
	args := []string{"build", "-o", path, "-ldflags", "-X main.version=" + v}
	if trimpath {
		args = append(args, "-trimpath")
	}
	build := exec.Command(goBin, append(args, ".")...)
	build.Dir = src
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build a fake evo %s: %v\n%s", v, err, out)
	}
	fakeEvos.built[key] = path
	return path
}

// fakeEvoInPath puts into dir an evo whose build information records
// version v (builtFakeEvo): evo doctor reads the version from the file, it
// never runs it. It returns its path.
func fakeEvoInPath(t *testing.T, dir, v string) string {
	t.Helper()
	return copyFakeEvo(t, dir, builtFakeEvo(t, v, false))
}

// copyFakeEvo copies the built evo at built into dir, under the name PATH
// resolves as evo, and returns its path.
func copyFakeEvo(t *testing.T, dir, built string) string {
	t.Helper()
	body, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fakeEvoName())
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// T7 (#718; process plan 1.7 §5): with two evo in PATH the shell runs one
// and completes from it, the person upgrades the other — evo doctor names
// every other evo in PATH with its version. The world: two fake evo, each
// built with its version; on Windows each an evo.exe, found by PATHEXT as a
// second evo.exe would be.
func TestDoctor_NamesAnotherEvoInPath(t *testing.T) {
	var dirs, paths []string
	for _, v := range []string{"9.8.7", "9.8.6"} {
		dir := t.TempDir()
		paths = append(paths, fakeEvoInPath(t, dir, v))
		dirs = append(dirs, dir)
	}
	t.Setenv("PATH", strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator)))
	var text strings.Builder
	for _, c := range doctorLocal(nil) {
		text.WriteString(c.Name + " " + c.Detail + "\n")
	}
	named := true
	for i, p := range paths {
		if !strings.Contains(text.String(), strings.TrimSuffix(p, ".cmd")) || !strings.Contains(text.String(), []string{"9.8.7", "9.8.6"}[i]) {
			named = false
		}
	}
	_, pending := tabShellPending["second evo"]
	switch {
	case !named && !pending:
		t.Errorf("evo doctor does not name each evo in PATH with its version (%v):\n%s", dirs, text.String())
	case named && pending:
		t.Error("evo doctor names the other evo now: take \"second evo\" out of tabShellPending (the list only shrinks)")
	}
}
