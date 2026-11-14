// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// TestMain runs the tests in English: they assert the English texts.
// Tests of the Russian output set the language themselves.
func TestMain(m *testing.M) {
	i18n.SetLang("en")
	// The check of port 22 of deploy_apply gets no answer without a
	// connect: the fake clouds' documentation addresses are not dialled,
	// whatever network the tests run in (#1707).
	deployplan.DialSSH = func(context.Context, string, time.Duration) error { return sshNoAnswer{} }
	// The looks for a floating IP whose create was cut go at the world's
	// pace, not a production second each (deployplan.FIPLookupInterval).
	deployplan.FIPLookupInterval = time.Millisecond
	// The tests run in a home of their own: a test that does not set HOME
	// (stackHome) writes the stack jobs and the files of evo there, not
	// in the home of whoever runs them (Астра; «Процесс», #1797). The home
	// is empty after the run, or the run fails: a test that writes there
	// did not isolate itself.
	home, err := os.MkdirTemp("", "evo-tools-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	code := m.Run()
	if left := filesUnder(home); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "a test wrote to the home of the run, not to one of its own (stackHome, t.Setenv HOME):\n  %s\n", strings.Join(left, "\n  "))
		if code == 0 {
			code = 1
		}
	}
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// filesUnder is the files under dir, relative to it.
func filesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	return out
}
