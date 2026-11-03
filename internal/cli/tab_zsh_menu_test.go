// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The menu style goes before the script's call of _evo: zsh loads _evo on
// the first Tab and runs the file — a style set after the call would give
// the menu from the second Tab on. With --no-descriptions too.
func TestTab_ZshMenuBeforeTheCall(t *testing.T) {
	for _, args := range [][]string{{"completion", "zsh"}, {"completion", "zsh", "--no-descriptions"}} {
		root := newRootCmd()
		root.SetArgs(args)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		script := out.String()
		style, call := strings.Index(script, zshMenu), strings.Index(script, zshCallMark)
		if style < 0 || call < 0 || style > call || strings.Count(script, zshMenu) != 1 {
			t.Errorf("evo %s: the menu style (at %d) once, before the call of _evo (at %d)", strings.Join(args, " "), style, call)
		}
		if noDesc := len(args) == 3; noDesc != strings.Contains(script, "__completeNoDesc") {
			t.Errorf("evo %s: --no-descriptions not honoured", strings.Join(args, " "))
		}
	}
}
