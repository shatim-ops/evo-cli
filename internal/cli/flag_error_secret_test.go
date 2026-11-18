// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const flagSecret = "s3cr3tF1ag27183650904512"

// flagForms are the command lines that give a flag of c a value it cannot
// take, or give an unknown flag: the value glued to a shorthand, --f=v,
// --f v, and the value itself where a flag's name goes.
func flagForms(c *cobra.Command) [][]string {
	forms := [][]string{
		{"--" + flagSecret},
		{"--" + flagSecret + "=x"},
		{"-Q" + flagSecret},
		{"---" + flagSecret},
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		switch f.Value.Type() {
		case "string", "stringArray":
			return // any text is a value of these
		case "stringSlice":
			// A slice is read as CSV: an open quote is refused (Астра,
			// #2368).
			forms = append(forms, []string{"--" + f.Name + `="` + flagSecret}, []string{"--" + f.Name, `"` + flagSecret})
			return
		}
		forms = append(forms, []string{"--" + f.Name + "=" + flagSecret})
		if f.NoOptDefVal == "" {
			forms = append(forms, []string{"--" + f.Name, flagSecret})
		}
		if f.Shorthand != "" {
			forms = append(forms, []string{"-" + f.Shorthand + flagSecret})
		}
	})
	return forms
}

// Every refusal of the flags of every command names the flag and the place
// of the argument, never the text typed: a value, or what stands where a
// flag's name goes, may be a secret (#2343, «Риск», to #2341).
func TestEveryCommand_FlagRefusalDoesNotEchoTheValue(t *testing.T) {
	root := newRootCmd()
	root.InitDefaultHelpFlag()
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		for _, form := range flagForms(c) {
			err := c.ParseFlags(form)
			if err == nil {
				continue
			}
			checked++
			args := append(strings.Fields(c.CommandPath())[1:], form...)
			if msg := flagError(err, args).Error(); strings.Contains(msg, "s3cr3t") || !strings.Contains(msg, "argument") {
				t.Errorf("%s %v: want the flag and its place, not the value: %s", c.CommandPath(), form, msg)
			}
		}
	}
	walk(root)
	if checked < 500 {
		t.Fatalf("only %d refusals of flags checked: the walk lost the tree", checked)
	}
}

// The same through the whole CLI: the forms of #2343 and an unknown flag
// before the command (leadingUnknownFlag), in every mode.
func TestFlagRefusal_DoesNotEchoTheValue(t *testing.T) {
	setupCLI(t)
	for _, line := range [][]string{
		{"-p" + flagSecret, "vm", "list"},
		{"vm", "list", "-p" + flagSecret},
		{"--json=" + flagSecret, "vm", "list"},
		{"vm", "list", "--json=" + flagSecret},
		{"--" + flagSecret, "vm", "list"},
		{"vm", "list", "--" + flagSecret},
		{"vm", "list", "---" + flagSecret},
		{"secret", "create", "app/db", "--" + flagSecret},
	} {
		for _, extra := range [][]string{nil, {"--json"}, {"--dry-run"}} {
			newProcess()
			code, out, errOut := runCLI(t, "", append(append([]string{}, extra...), line...)...)
			if code == 0 {
				t.Fatalf("%v %v: want a refusal: %s%s", extra, line, out, errOut)
			}
			if strings.Contains(out+errOut, "s3cr3t") {
				t.Errorf("%v %v: the refusal echoes the value: %s%s", extra, line, out, errOut)
			}
		}
	}
}
