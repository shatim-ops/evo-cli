// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Check the terminal document the reader receives against CLISpec. A
// regenerated card can still be wrong if the generator omitted a root verb.
// TestAgentSkill_CardOfEveryGroupWithNoTerminalInIt checks the MCP-only form.
func TestGroupCardCommandsMatchCLISpec(t *testing.T) {
	registry := tools.New(nil)
	for _, group := range tools.Groups() {
		t.Run(group, func(t *testing.T) {
			doc, err := os.ReadFile(filepath.Join(cardsDir, group+".md"))
			if err != nil {
				t.Fatal(err)
			}
			want := make(map[string]string)
			for _, tool := range registry.Tools() {
				if tool.Group == group {
					want[tool.Name] = toolCommandCell(tool)
				}
			}
			for _, line := range strings.Split(string(doc), "\n") {
				if !strings.HasPrefix(line, "| `") {
					continue
				}
				cells := strings.SplitN(line, "|", 4)
				if len(cells) < 4 {
					t.Fatalf("incomplete tool row: %q", line)
				}
				name := strings.Trim(strings.TrimSpace(cells[1]), "`")
				command, ok := want[name]
				if !ok {
					t.Errorf("unexpected or repeated tool %q", name)
					continue
				}
				if cell := strings.TrimSpace(cells[2]); cell != command {
					t.Errorf("%s: got command %q, want %q", name, cell, command)
				}
				delete(want, name)
			}
			for name := range want {
				t.Errorf("missing row for %s", name)
			}
		})
	}
}
