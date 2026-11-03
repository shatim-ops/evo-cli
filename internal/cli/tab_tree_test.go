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
)

// tabFileDefault are the commands whose Tab on an empty word falls back to
// the shell's files (cobra's ShellCompDirectiveDefault) though nothing of
// theirs names a path: the command's path → true. It only shrinks. Closes
// by 1.7 (release plan 1.7, block B, Tab, T1, #718: one point — no file
// completion unless the registry declares the argument a path).
var tabFileDefault = map[string]bool{
	"agent setup":          true,
	"agent skill":          true,
	"ar create":            true,
	"ar delete":            true,
	"ar get":               true,
	"ar list":              true,
	"ar operation":         true,
	"audit events":         true,
	"billing balance":      true,
	"billing consumption":  true,
	"billing finops":       true,
	"ca delete":            true,
	"ca deploy":            true,
	"ca get":               true,
	"ca list":              true,
	"ca logs":              true,
	"ca start":             true,
	"ca stop":              true,
	"cert import":          true,
	"cert issue-le":        true,
	"cert list":            true,
	"cert operation":       true,
	"configure delete":     true,
	"configure list":       true,
	"deploy apply":         true,
	"deploy list":          true,
	"deploy plan":          true,
	"destroy":              true,
	"diag export":          true,
	"diag off":             true,
	"diag on":              true,
	"diag status":          true,
	"disk create":          true,
	"disk list":            true,
	"disk types":           true,
	"dns operation":        true,
	"dns record-delete":    true,
	"dns record-get":       true,
	"dns zone-create":      true,
	"dns zones":            true,
	"doctor":               true,
	"iam service-accounts": true,
	"image create":         true,
	"image list":           true,
	"init":                 true,
	"k8s addon-versions":   true,
	"k8s addons":           true,
	"k8s create":           true,
	"k8s list":             true,
	"k8s options":          true,
	"k8s plan":             true,
	"kafka create":         true,
	"kafka list":           true,
	"kafka operation":      true,
	"kafka specs":          true,
	"kafka versions":       true,
	"lb create":            true,
	"lb list":              true,
	"lb operation":         true,
	"logs":                 true,
	"marketplace list":     true,
	"mcp":                  true,
	"metrics history":      true,
	"metrics scrape":       true,
	"org projects":         true,
	"org quotas":           true,
	"pg create":            true,
	"pg list":              true,
	"pg operation":         true,
	"pg specs":             true,
	"pg versions":          true,
	"project list":         true,
	"project use":          true,
	"redis create":         true,
	"redis list":           true,
	"redis operation":      true,
	"redis specs":          true,
	"redis versions":       true,
	"s3 buckets":           true,
	"secret create":        true,
	"secret list":          true,
	"snat create":          true,
	"snat list":            true,
	"stack apply":          true,
	"stack destroy":        true,
	"stack plan":           true,
	"stack status":         true,
	"status":               true,
	"tg create":            true,
	"tg list":              true,
	"tools list":           true,
	"verify":               true,
	"version":              true,
	"vm create":            true,
	"vm flavors":           true,
	"vm list":              true,
	"vm zones":             true,
	"vpc close-ports":      true,
	"vpc create":           true,
	"vpc fip-attach":       true,
	"vpc fips":             true,
	"vpc list":             true,
	"vpc open-ports":       true,
	"vpc operation":        true,
	"vpc security-groups":  true,
	"vpc subnet-create":    true,
	"vpc subnets":          true,
	"whoami":               true,
}

// T1 (#718, the owner's dogfood of 31.10; process plan 1.7 §5): Tab on an
// empty word of every command of the tree gives candidates or says there
// are none (ShellCompDirectiveNoFileComp); the shell's files only where an
// argument is a path. A command that falls back to the files is a line of
// tabFileDefault; a line whose command no longer does fails too.
func TestTab_EveryCommandCompletesOrSaysNone(t *testing.T) {
	_, _ = withJSONWorld(t)
	var paths [][]string
	var walk func(c *cobra.Command, p []string)
	walk = func(c *cobra.Command, p []string) {
		for _, s := range c.Commands() {
			if s.Hidden || s.Name() == "help" || strings.HasPrefix(s.Name(), "__") {
				continue
			}
			q := append(append([]string{}, p...), s.Name())
			paths = append(paths, q)
			walk(s, q)
		}
	}
	walk(newRootCmd(), nil)
	if len(paths) < 100 {
		t.Fatalf("%d commands in the tree: the walk sees nothing", len(paths))
	}
	seen := map[string]bool{}
	var bad []string
	for _, p := range paths {
		root := newRootCmd()
		root.SetArgs(append(append([]string{"__complete"}, p...), ""))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err != nil {
			t.Fatalf("__complete %v: %v", p, err)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		directive := lines[len(lines)-1]
		name := strings.Join(p, " ")
		seen[name] = true
		fileDefault := directive == ":0"
		switch {
		case fileDefault && !tabFileDefault[name]:
			bad = append(bad, "evo "+name+": Tab on an empty word offers the shell's files — complete the argument, or say there is nothing (ShellCompDirectiveNoFileComp)")
		case !fileDefault && tabFileDefault[name]:
			bad = append(bad, "evo "+name+": in tabFileDefault, but Tab no longer offers files; take the line out (the list only shrinks)")
		}
	}
	for name := range tabFileDefault {
		if !seen[name] {
			bad = append(bad, "evo "+name+": in tabFileDefault, no such command")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	t.Logf("commands: %d, falling back to files: %d (Closes by 1.7)", len(paths), len(tabFileDefault))
}
