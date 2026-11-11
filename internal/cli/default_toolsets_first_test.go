// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"slices"
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
