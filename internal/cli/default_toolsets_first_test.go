// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Grok, gate 1.8, D2-W (2 of 2): evo agent setup without --toolsets gave
// the agent deploy,vm,vpc,disk,image, while whoami (group auth) says «Call
// it first when calls fail with an authentication error or land in an
// unexpected project» — and the agent got «Tool whoami not found». A tool
// that its description or the built-in skill tells the agent to call first
// is in the groups the agent sees by default.
func TestDefaultToolsets_HoldTheToolsToCallFirst(t *testing.T) {
	first := regexp.MustCompile(`(?i)\bcall it first\b`)
	named := regexp.MustCompile("(?i)(?:call|run)\\s+`?(?:evo\\s+)?([a-z_]+)`?\\s+first")
	skill := agentSkillBody(newRootCmd(), false)
	want := map[string]bool{}
	for _, m := range named.FindAllStringSubmatch(skill, -1) {
		want[m[1]] = true
	}
	all := tools.New(nil).Tools()
	for _, tl := range all {
		if first.MatchString(tl.Description) {
			want[tl.Name] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("no tool is told to be called first: the check sees nothing")
	}
	for _, tl := range all {
		if want[tl.Name] && !slices.Contains(defaultToolsets, tl.Group) {
			t.Errorf("%s (group %s) is to be called first, but evo agent setup does not give its group by default (%v)", tl.Name, tl.Group, defaultToolsets)
		}
	}
}

// «Риск», conclusion 87 to D2-W, way (б): the entry a released evo wrote
// with its default groups is evo's own, as the rules are (generated): a
// repeated evo agent setup without --toolsets replaces it after the diff and
// the consent, with exit 0, as before the default changed. An entry a
// person edited is still theirs (a conflict without --force), and the old
// groups asked for by name are kept («nothing changed»).
func TestAgentSetup_ReleasedDefaultEntryIsEvos(t *testing.T) {
	released := `{"mcpServers": {"evo": {"command": "evo", "args": ["mcp", "--toolsets", "deploy,vm,vpc,disk,image"]}}}`
	for _, c := range []struct {
		name, file string
		args       []string
		code       int
		want       string // in the file after the call, or in the output
	}{
		{"the entry of 1.7.2: replaced", released, nil, 0, "deploy,vm,vpc,disk,image,auth"},
		{"the entry of 1.7.2 a person edited: a conflict",
			`{"mcpServers": {"evo": {"command": "evo", "args": ["mcp", "--toolsets", "deploy,vm"]}}}`, nil, 1, `"deploy,vm"`},
		{"the old groups by name: nothing changed", released, []string{"--toolsets", "deploy,vm,vpc,disk,image"}, 0, "nothing changed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			setupAgent(t)
			cl := mustClient(t, "claude-code")
			path := clientPath(t, cl, cl.defaultScope())
			putFile(t, path, []byte(c.file), 0o600)
			code, stdout, stderr := runCLI(t, "", append([]string{"agent", "setup", "--client", "claude-code", "--yes"}, c.args...)...)
			data, _ := os.ReadFile(path)
			if code != c.code || !strings.Contains(string(data)+stdout+stderr, c.want) {
				t.Errorf("exit %d (want %d), no %q:\n%s\n%s%s", code, c.code, c.want, data, stdout, stderr)
			}
		})
	}
}
