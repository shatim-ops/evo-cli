// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Agent run M2 (Ц-1): the server was in .mcp.json with other toolsets,
// and --skill refused — the only way out, --force, would have narrowed the
// toolsets the agent works with. The refusal names a way that leaves the
// entry: --skill-only writes the rules alone and never compares the
// client's entry (the release's choice 03.11: no exit code changes).
func TestAgentSetup_SkillOnlyLeavesTheClientEntry(t *testing.T) {
	setupAgent(t)
	if code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--toolsets", "pg,s3", "--yes"); code != 0 {
		t.Fatalf("first setup: exit %d: %s", code, stderr)
	}
	mcp := fileBytes(t, ".mcp.json")
	skill := filepath.Join(".claude", "skills", "evo", "SKILL.md")

	code, _, stderr := runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill", "--yes")
	if code != clerrors.ExitGeneral || !strings.Contains(stderr, "--skill-only") || !strings.Contains(stderr, "--force") {
		t.Errorf("another evo entry with --skill: exit %d, want 1 naming --skill-only and --force:\n%s", code, stderr)
	}

	code, _, stderr = runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill-only", "--yes")
	if code != 0 {
		t.Fatalf("--skill-only: exit %d: %s", code, stderr)
	}
	if !bytes.Equal(fileBytes(t, ".mcp.json"), mcp) {
		t.Errorf("--skill-only changed .mcp.json:\n%s", fileBytes(t, ".mcp.json"))
	}
	if got := string(fileBytes(t, skill)); got != agentSkill(newRootCmd(), false) {
		t.Errorf("--skill-only did not write the rules:\n%s", got)
	}

	// The two ask for opposite things: refused before anything is written.
	before := fileBytes(t, ".mcp.json")
	code, _, stderr = runCLI(t, "", "agent", "setup", "--client", "claude-code", "--skill-only", "--force", "--yes")
	if code != clerrors.ExitGeneral || !bytes.Equal(fileBytes(t, ".mcp.json"), before) {
		t.Errorf("--skill-only --force: exit %d, want 1 and nothing written:\n%s", code, stderr)
	}
}
