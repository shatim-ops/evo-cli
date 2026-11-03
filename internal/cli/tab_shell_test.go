// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tabShellPending are the parts of Tab in the shell that are not there
// yet: the part → what is missing. It only shrinks. Closes by 1.7 (release
// plan 1.7, block B, Tab, #718: T4, T7).
var tabShellPending = map[string]string{
	"zsh menu":   "evo completion zsh sets no menu of the candidates (zstyle … menu yes select)",
	"second evo": "evo doctor does not name another evo in PATH",
}

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

// fakeEvoInPath writes into dir an evo that answers version v: a shell script,
// on Windows a .cmd that PATHEXT finds as evo. It returns its path.
func fakeEvoInPath(t *testing.T, dir, v string) string {
	t.Helper()
	name, body := "evo", "#!/bin/sh\necho 'evo version "+v+"'\necho '{\"version\":\""+v+"\"}'\n"
	if runtime.GOOS == "windows" {
		name, body = "evo.cmd", "@echo off\r\necho evo version "+v+"\r\necho {\"version\":\""+v+"\"}\r\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// T7 (#718; process plan 1.7 §5): with two evo in PATH the shell runs one
// and completes from it, the person upgrades the other — evo doctor names
// every other evo in PATH with its version. The world: two fake evo, each
// answering its version; on Windows each an evo.cmd, found by PATHEXT as a
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
