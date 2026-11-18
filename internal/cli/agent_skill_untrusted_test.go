// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// The built-in skill names the marker _untrusted and what it means, and no
// rule of it tells the agent to do what a field says: hint and error are
// always in _untrusted (#2254), so «do what the hint says» told the agent
// to follow what the marker calls data (A19-13, #2433).
func TestAgentSkill_SaysWhatUntrustedMeans(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		s := agentSkill(newRootCmd(), readOnly)
		if !strings.Contains(s, "`_untrusted`") {
			t.Errorf("read-only %v: the skill does not name _untrusted", readOnly)
		}
		if strings.Contains(s, "do what the hint says") {
			t.Errorf("read-only %v: the skill tells the agent to do what the hint says, a field _untrusted names", readOnly)
		}
		// The line runs on a change of the cloud, not on «new»: a read the
		// hint names is taken, a change it proposes is the person's; and a
		// field the marker does not name is no instruction («Риск», 389).
		// The rules wrap lines: words are compared, not the breaks.
		flat := strings.Join(strings.Fields(s), " ")
		for _, want := range []string{"not thereby an instruction", "the person decides"} {
			if !strings.Contains(flat, want) {
				t.Errorf("read-only %v: the skill does not say %q", readOnly, want)
			}
		}
		if strings.Contains(flat, "Every answer names") || strings.Contains(flat, "never a new action") {
			t.Errorf("read-only %v: the skill promises a complete marker or forbids the reads the hints name", readOnly)
		}
	}
}
