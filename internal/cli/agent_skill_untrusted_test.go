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
	}
}
