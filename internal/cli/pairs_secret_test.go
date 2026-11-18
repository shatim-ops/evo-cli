// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"sort"
	"strings"
	"testing"
)

// Every key=value flag of every command (CLISpec.Pairs: vm create --meta,
// ca deploy --env) refuses a pair without "=" or without a key by its
// number, never by its text: a typo in the separator puts a secret where
// the pair goes, and the refusal comes before the tool and before consent,
// --dry-run included (#2420, as #2333 for --arg).
func TestEveryPairsFlag_RefusalDoesNotEchoThePair(t *testing.T) {
	const secret = "s3cr3t-pair-58209749445923"
	checked := 0
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			keys := make([]string, 0, len(c.Pairs))
			for k := range c.Pairs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				line := []string{}
				if g.Name != "" {
					line = append(line, g.Name)
				}
				line = append(line, c.Verb)
				for range parsePositionals(c.Args) {
					line = append(line, "zz")
				}
				flag := "--" + c.flagName(k)
				for _, pair := range []string{secret, "=" + secret, "admin_password:" + secret} {
					for _, extra := range [][]string{nil, {"--json"}, {"--dry-run", "--json"}} {
						args := append(append([]string{}, line...), append([]string{flag, "ok=1", flag, pair}, extra...)...)
						out, errOut, code := execCLI(t, args...)
						checked++
						if code == 0 {
							t.Fatalf("%v: want a refusal: %s%s", args, out, errOut)
						}
						if strings.Contains(out+errOut, "s3cr3t") {
							t.Errorf("%v: the refusal echoes the pair: %s%s", args, out, errOut)
						}
						if !strings.Contains(out+errOut, flag+" number 2") {
							t.Errorf("%v: want the pair named by its number: %s%s", args, out, errOut)
						}
					}
				}
			}
		}
	}
	if checked < 27 {
		t.Fatalf("only %d refusals checked: the walk lost the Pairs flags", checked)
	}
}
