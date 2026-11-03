// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// tabOut runs evo __complete args and returns its candidates (the names,
// without descriptions), its lines of ActiveHelp and its directive.
func tabOut(t *testing.T, args ...string) (cands, help []string, directive string) {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(append([]string{"__complete"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	directive = lines[len(lines)-1]
	for _, l := range lines[:len(lines)-1] {
		if h, ok := strings.CutPrefix(l, "_activeHelp_ "); ok {
			help = append(help, h)
			continue
		}
		name, _, _ := strings.Cut(l, "\t")
		cands = append(cands, name)
	}
	return cands, help, directive
}

// #718, the owner's decision of 31.10: past the positionals, Tab on an
// empty word gives one list — the required flags first, then the others,
// each once — and says in a line of ActiveHelp what is still required,
// then that the call is ready; a flag given is not offered again.
func TestTab_EmptyWordNamesWhatIsStillRequired(t *testing.T) {
	_, _ = withJSONWorld(t)
	t.Setenv("EVO_ACTIVE_HELP", "")
	cands, help, _ := tabOut(t, "disk", "create", "d1", "")
	if len(cands) == 0 || cands[0] != "--size-gb" {
		t.Errorf("disk create d1 <Tab>: want --size-gb first, got %v", cands)
	}
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			t.Errorf("disk create d1 <Tab>: %s twice: %v", c, cands)
		}
		seen[c] = true
	}
	if !seen["--zone"] || len(help) != 1 || !strings.Contains(help[0], "--size-gb") {
		t.Errorf("disk create d1 <Tab>: want the optional flags and one line naming --size-gb, got %v / %v", cands, help)
	}
	cands, help, _ = tabOut(t, "disk", "create", "d1", "--size-gb", "20", "")
	if seen := strings.Join(cands, " "); strings.Contains(seen, "--size-gb") || len(help) != 1 || !strings.Contains(help[0], "--dry-run") {
		t.Errorf("disk create d1 --size-gb 20 <Tab>: want no --size-gb and «ready», got %v / %v", cands, help)
	}
	// A set of RequiredOneOf is one entry, gone once one of it is given.
	_, help, _ = tabOut(t, "dns", "record-create", "z", "www", "")
	if len(help) != 1 || !strings.Contains(help[0], "--value | --values") {
		t.Errorf("dns record-create <Tab>: want «--value | --values» still required, got %v", help)
	}
	_, help, _ = tabOut(t, "dns", "record-create", "z", "www", "--value", "1.2.3.4", "")
	if len(help) != 1 || strings.Contains(help[0], "--value") {
		t.Errorf("dns record-create --value x <Tab>: the pair is done, got %v", help)
	}
}

// EVO_ACTIVE_HELP=0 (and cobra's COBRA_ACTIVE_HELP=0) turns the lines of
// ActiveHelp off; the candidates stay.
func TestTab_ActiveHelpOff(t *testing.T) {
	_, _ = withJSONWorld(t)
	for _, env := range []string{"EVO_ACTIVE_HELP", "COBRA_ACTIVE_HELP"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "0")
			cands, help, _ := tabOut(t, "disk", "create", "d1", "")
			if len(help) != 0 || len(cands) == 0 {
				t.Errorf("%s=0: want candidates and no ActiveHelp, got %v / %v", env, cands, help)
			}
			if _, help, _ := tabOut(t, "disk", "create", "d1", "--size-gb", ""); len(help) != 0 {
				t.Errorf("%s=0: a flag's value has ActiveHelp: %v", env, help)
			}
		})
	}
}

// The value of a flag: the closed list of the schema, a hint for a free
// value (its description and bounds, as the help has them).
func TestTab_FlagValueEnumOrHint(t *testing.T) {
	_, _ = withJSONWorld(t)
	t.Setenv("EVO_ACTIVE_HELP", "")
	cands, _, _ := tabOut(t, "dns", "record-create", "z", "www", "--record-type", "")
	if !strings.Contains(" "+strings.Join(cands, " ")+" ", " cname ") {
		t.Errorf("--record-type <Tab>: want the values of the schema, got %v", cands)
	}
	cands, help, directive := tabOut(t, "disk", "create", "d1", "--size-gb", "")
	if len(cands) != 0 || len(help) != 1 || !strings.Contains(help[0], "16384") || tabOffersFiles(directive) {
		t.Errorf("--size-gb <Tab>: want no candidates, no files and a hint with its bound, got %v / %v / %s", cands, help, directive)
	}
}

// #718, the owner's decision: files only where the registry declares a
// path — the value of a flag too. Every flag of every tool command: Tab on
// its value offers the shell's files iff the registry declares the
// argument a path (CLISpec.Files and CLISpec.Paths, or the file flag of
// CLISpec.SecretFiles).
func TestTab_FlagValueFilesOnlyAtPaths(t *testing.T) {
	_, _ = withJSONWorld(t)
	reg := newRegistry()
	root := newRootCmd()
	checked := 0
	var bad []string
	for _, tl := range reg.Tools() {
		if tl.CLI.Group == "" || tl.CLI.Verb == "" {
			continue
		}
		cmd, _, err := root.Find([]string{tl.CLI.Group, tl.CLI.Verb})
		if err != nil || cmd == root {
			continue
		}
		flagOf := func(arg string) string {
			if n, ok := tl.CLI.Rename[arg]; ok {
				return n
			}
			return strings.ReplaceAll(arg, "_", "-")
		}
		paths := map[string]bool{}
		for a := range tl.CLI.Files {
			paths[flagOf(a)] = true
		}
		for a := range tl.CLI.SecretFiles {
			paths[secretFileFlag(flagOf(a))] = true
		}
		for a := range tl.CLI.Paths {
			paths[flagOf(a)] = true
		}
		cmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Value.Type() == "bool" || f.Hidden {
				return
			}
			checked++
			_, _, directive := tabOut(t, tl.CLI.Group, tl.CLI.Verb, "--"+f.Name, "")
			if files := tabOffersFiles(directive); files != paths[f.Name] {
				bad = append(bad, "evo "+tl.CLI.Group+" "+tl.CLI.Verb+" --"+f.Name+": files="+map[bool]string{true: "yes", false: "no"}[files])
			}
		})
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("Tab on a flag's value offers files though the registry declares no path, or none at a path:\n%s", strings.Join(bad, "\n"))
	}
	if checked < 100 {
		t.Fatalf("%d flags checked: the walk sees nothing", checked)
	}
	_ = cobra.ShellCompDirectiveDefault
}

// deploy plan --compose-file is a path the tool reads (CLISpec.Paths): Tab
// on its value offers the files, as before #718 — the one flag of a tool
// command that took a path and was declared none.
func TestTab_ComposeFileStillOffersFiles(t *testing.T) {
	_, _ = withJSONWorld(t)
	if _, _, directive := tabOut(t, "deploy", "plan", "--compose-file", ""); !tabOffersFiles(directive) {
		t.Errorf("deploy plan --compose-file <Tab>: want the files, got %s", directive)
	}
}
