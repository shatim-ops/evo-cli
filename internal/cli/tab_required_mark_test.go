// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Tab names the required flags first by cobra's "required" annotation
// (markTabRequired), and the value tabRequired keeps the refusal of a run
// without them the tool's own (tools.CheckArgs): with the value "true"
// cobra would refuse first, with its own text and outside the parity of
// CLI and MCP. No flag of the tree carries "true".
func TestTab_RequiredMarkLeavesRefusalToTool(t *testing.T) {
	marked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			v, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
			if !ok {
				return
			}
			marked++
			if len(v) != 1 || v[0] != tabRequired {
				t.Errorf("evo %s --%s: required annotation %q, want %q: cobra would refuse the run itself", c.CommandPath(), f.Name, v, tabRequired)
			}
		})
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(newRootCmd())
	if marked == 0 {
		t.Fatal("no flag of the tree is marked required: Tab has nothing to put first")
	}
}
