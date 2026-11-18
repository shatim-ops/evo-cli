// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// extraArgSecret stands for a secret given as a positional argument where
// the command takes none, or one more than it takes (a value whose flag was
// left out).
const extraArgSecret = "s3cr3t-27183650904512"

// Every command refuses positional arguments it does not take by their
// count and place, never by their text: the text may be a secret given in
// the wrong place, and the refusal comes before any question of consent
// (#2334, «Риск», conclusion 341). The check is the command's own: its
// Args, the root's included.
func TestEveryCommand_ExtraArgsRefusalDoesNotEchoThem(t *testing.T) {
	root := newRootCmd()
	args := []string{extraArgSecret, extraArgSecret + "-2", extraArgSecret + "-3", extraArgSecret + "-4",
		extraArgSecret + "-5", extraArgSecret + "-6", extraArgSecret + "-7", extraArgSecret + "-8", extraArgSecret + "-9"}
	checked, refused := 0, 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		checked++
		err := c.ValidateArgs(args)
		if c == root && c.Args == nil {
			t.Errorf("evo: the root takes cobra's own check, which names the argument; set its Args")
		}
		if err == nil {
			return // takes any number of arguments: they belong to the call
		}
		refused++
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: the refusal of extra arguments echoes them: %v", c.CommandPath(), err)
		}
	}
	walk(root)
	if refused < 50 {
		t.Fatalf("%d commands, %d refused nine arguments: the walk lost the tree", checked, refused)
	}
}

// The same through the whole CLI, for the commands of a secret value
// (#2334) and the root, in every mode.
func TestSecretCommands_ExtraArgsRefusalDoesNotEchoThem(t *testing.T) {
	for _, line := range [][]string{
		{"secret", "create", "app/db", extraArgSecret},
		{"secret", "add-version", "app/db", extraArgSecret},
		{"secret", extraArgSecret},
		{extraArgSecret},
		{"doctor", extraArgSecret},
	} {
		for _, extra := range [][]string{nil, {"--json"}, {"--dry-run", "--json"}} {
			out, errOut, code := execCLI(t, append(append([]string{}, extra...), line...)...)
			if code == 0 {
				t.Fatalf("%v %v: want a refusal: %s%s", extra, line, out, errOut)
			}
			if strings.Contains(out+errOut, "s3cr3t") {
				t.Errorf("%v %v: the refusal echoes the argument: %s%s", extra, line, out, errOut)
			}
		}
	}
}
