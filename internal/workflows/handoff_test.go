// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"strings"
	"testing"
)

// docs/RELEASING.md, step 6: after a tag no PR merges into main before the
// handoff of the version (handoff/<version>.md). ci runs the check on every
// PR, it fails the run (no continue-on-error), the base comes through env,
// and the script is tested on a scratch repository. Retro 1.4.1: three PRs
// were merged after v1.4.1 before its handoff, the rule held only in the
// release session's memory.
func TestWorkflows_HandoffBeforeNextMerge(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	j, ok := w.Jobs["handoff"]
	if !ok {
		t.Fatal("ci: no job handoff (docs/RELEASING.md, step 6)")
	}
	if !strings.Contains(j.If, "pull_request") || j.ContinueOnError {
		t.Errorf("ci handoff: runs on %q, continue-on-error %v — want every PR, failing the run", j.If, j.ContinueOnError)
	}
	found := false
	for _, s := range j.Steps {
		if strings.Contains(s.Run, ".github/scripts/handoff_check.sh") {
			found = true
			if s.ContinueOnError || s.Env["BASE"] == "" || !strings.Contains(s.Run, `"$BASE"`) || strings.Contains(s.Run, "${{") {
				t.Errorf("ci handoff: the check must fail the job and take the base from env: %+v", s)
			}
		}
	}
	if !found {
		t.Error("ci handoff: no step runs handoff_check.sh")
	}
	if !runsStep(w, ".github/scripts/handoff_check_test.sh") {
		t.Error("ci: handoff_check.sh is not tested (handoff_check_test.sh)")
	}
}
