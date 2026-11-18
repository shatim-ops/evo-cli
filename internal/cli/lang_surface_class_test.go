// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// quotedValue is a value a text of evo quotes in single quotes — a
// category of the catalogue of the platform ('Сети'), not a word of evo;
// the quote opens after a space or a parenthesis (not «console's»).
var quotedValue = regexp.MustCompile(`(^|[\s(])'[^'\n]*'`)

// Every local command — the commands without a tool (localCommands), whose
// words the parity of the registry does not check — answers a person whose
// language is Russian in English under --json, run as evo and as the legacy
// alias: the machine interface of the CLI is English, from its first line
// on stderr to its answer (D6, refinement 14.11). The class, not one test
// per message: the rename notice, the warning of the migration and the
// refusal of the healthcheck went past the tests of the registry one by one
// (Астра, #2440–#2445; #2442).
func TestLocalCommands_JSONIsEnglishFromTheFirstLine(t *testing.T) {
	paths := make([]string, 0, len(localDryRuns))
	for p, r := range localDryRuns {
		if r.skip == "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	ran := 0
	for _, alias := range []bool{false, true} {
		for _, p := range paths {
			r := localDryRuns[p]
			name := p
			if alias {
				name = "alias " + p
			}
			t.Run(name, func(t *testing.T) {
				russianCLI(t)
				legacyAliasRun = alias
				t.Cleanup(func() { legacyAliasRun = false })
				newProcess()
				got := runLocal(t, r, true)
				ran++
				for _, line := range strings.Split(got.stderr+"\n"+got.stdout, "\n") {
					if cyrillic.MatchString(quotedValue.ReplaceAllString(line, "$1")) {
						t.Errorf("evo %s %s (exit %d): Russian under --json: %.300q", strings.Join(got.flags, " "), strings.Join(r.args, " "), got.code, line)
						break
					}
				}
			})
		}
	}
	if ran < 2*20 {
		t.Errorf("%d runs of local commands, want at least 40: the world no longer reaches them", ran)
	}
}

// The usage line of every command is the same in both languages and is
// English: a word of Russian in it reaches the English help (evo project
// use named its argument «имя», Астра #2445).
func TestHelp_EveryUseLineIsEnglish(t *testing.T) {
	var walk func(c *cobra.Command)
	n := 0
	walk = func(c *cobra.Command) {
		n++
		if cyrillic.MatchString(c.Use) {
			t.Errorf("%s: the usage line has Russian: %q", c.CommandPath(), c.Use)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	if n < 100 {
		t.Errorf("%d commands walked, want the whole tree", n)
	}
}
