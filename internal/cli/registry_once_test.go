// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// A run of evo builds the tool registry once: the command trees, the
// completions, the notes of earlier runs and the command that runs read the
// same one. Four builds a run made the package's tests the slowest of CI
// (release plan 1.9, «Реестр инструментов строится один раз»).
func TestRegistry_BuiltOnceARun(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"tools", "list", "--json"},         // reads the registry when it runs
		{"tools", "describe", "vm_list"},    // looks a tool up when it runs
		{"agent", "skill", "--group", "vm"}, // an engine command: calls its tool
		{"vm", "list", "--help"},            // a command of the trees
		{"__complete", "tools", "describe", ""},
	} {
		built := 0
		old := newRegistry
		newRegistry = func() *tools.Registry { built++; return old() }
		execCLI(t, args...)
		newRegistry = old
		if built != 1 {
			t.Errorf("evo %v built the registry %d times, want 1", args, built)
		}
	}
}
