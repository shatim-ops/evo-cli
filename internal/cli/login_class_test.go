// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// loginFlagCheck is what evo login can tell of a flag without the cloud:
// the values it refuses before the browser, or why there is nothing to
// refuse.
type loginFlagCheck struct {
	bad  [][]string
	none string
}

// loginFlagChecks are the flags evo login takes, its own and inherited:
// the flag → its check. A new flag without a line here fails
// TestLogin_EveryLocalCheckBeforeTheBrowser.
var loginFlagChecks = map[string]loginFlagCheck{
	"project":    {bad: [][]string{{"--project", "   "}, {"--project", "a\nb"}, {"--project", strings.Repeat("p", 300)}}},
	"for":        {bad: [][]string{{"--for", ""}, {"--for", "root"}}},
	"key-ttl":    {bad: [][]string{{"--key-ttl", "365d"}, {"--key-ttl", "0"}, {"--key-ttl", "7"}, {"--key-ttl", "-7d"}}},
	"profile":    {bad: [][]string{{"--profile", "../x"}, {"--profile", "a/b"}, {"--profile", "a b"}}},
	"dry-run":    {bad: [][]string{{"--dry-run"}}},
	"project-id": {bad: [][]string{{"--project-id", "not-a-uuid"}}},
	"region":     {bad: [][]string{{"--region", "a\nb"}}},

	"no-browser": {none: "a switch: the link is printed instead of opening the browser"},
	"yes":        {none: "hidden from the help of evo login: the consent is the page's"},
	"json":       {none: "a switch of the output"},
	"verbose":    {none: "a switch of the output"},
	"no-color":   {none: "a switch of the output"},
	"help":       {none: "prints the help and exits"},
}

// Plan 1.8, #1095, rounds 1–2 and 4 of the audit (Grok), class of #1100:
// what evo login can tell without the cloud — a value of a flag it would
// refuse anyway — it refuses before the browser, with USAGE or
// CONFIG_INVALID, and the browser is not opened. Every flag evo login takes
// is a line of loginFlagChecks: its bad values, or why there are none.
func TestLogin_EveryLocalCheckBeforeTheBrowser(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"login"})
	if err != nil || cmd.Name() != "login" {
		t.Fatalf("evo login not found: %v", err)
	}
	flags := map[string]bool{}
	visit := func(f *pflag.Flag) { flags[f.Name] = true }
	cmd.InitDefaultHelpFlag()
	cmd.LocalFlags().VisitAll(visit)
	cmd.InheritedFlags().VisitAll(visit)
	if len(flags) < 8 {
		t.Fatalf("%d flags of evo login read: the scan sees nothing", len(flags))
	}
	names := make([]string, 0, len(flags))
	for n := range flags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c, ok := loginFlagChecks[n]
		switch {
		case !ok:
			t.Errorf("--%s of evo login has no line in loginFlagChecks: name its bad values, refused before the browser, or why there are none", n)
		case len(c.bad) == 0 && c.none == "":
			t.Errorf("--%s: neither bad values nor why there are none", n)
		}
	}
	for n := range loginFlagChecks {
		if !flags[n] {
			t.Errorf("--%s is not a flag of evo login any more: drop the line", n)
		}
	}
	for _, n := range names {
		for _, args := range loginFlagChecks[n].bad {
			t.Run(strings.Join(args, "="), func(t *testing.T) {
				setupCLI(t)
				called := worldNoPerson(t)
				code, stdout, stderr := runCLI(t, "", append([]string{"--json", "login"}, args...)...)
				out := stderr + stdout
				if code == 0 || *called || !(strings.Contains(out, `"reason":"USAGE"`) || strings.Contains(out, `"reason":"CONFIG_INVALID"`)) {
					t.Errorf("%q: exit %d, browser opened %v — a value evo can tell bad without the cloud is refused before the browser\n%s", args, code, *called, out)
				}
			})
		}
	}
}
