// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"testing"
)

// An open pull request that stands with no red check and no event is told
// so by a round on a schedule (journal 1.9, «Тихое красное»): a pull
// request in conflict with main gets no ci at all (#1574), and a job the
// runner never took is cancelled, not failed (#1536 stood two hours). The
// round runs from main on a schedule and by hand — never on an event of a
// pull request, so no code of a pull request runs with its token — with
// the least rights it needs: the runs and their jobs, and a comment in the
// pull request. What the script says and when is pr_stuck_test.sh's, which
// ci runs.
func TestWorkflows_StuckPullRequestsAreTold(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["pr-stuck.yml"]
	if !ok {
		t.Fatal("no pr-stuck.yml: a pull request in conflict or with a job no runner took stands unseen")
	}
	for trig := range w.On {
		if trig != "schedule" && trig != "workflow_dispatch" {
			t.Errorf("pr-stuck: trigger %s, want schedule and workflow_dispatch alone (no event of a pull request)", trig)
		}
	}
	if _, ok := w.On["schedule"]; !ok {
		t.Error("pr-stuck: no schedule — the round is what finds a pull request nobody is told about")
	}
	if p, _ := w.Permissions.(map[string]any); len(p) != 0 {
		t.Errorf("pr-stuck: workflow permissions %v, want {} (rights per job)", w.Permissions)
	}
	for name, j := range w.Jobs {
		p, _ := j.Permissions.(map[string]any)
		if len(p) != 2 || p["actions"] != "read" || p["pull-requests"] != "write" {
			t.Errorf("pr-stuck: job %s permissions %v, want actions: read and pull-requests: write alone", name, j.Permissions)
		}
		if j.TimeoutMinutes == 0 {
			t.Errorf("pr-stuck: job %s has no timeout-minutes", name)
		}
	}
	if !runsStep(w, ".github/scripts/pr_stuck.sh") {
		t.Error("pr-stuck: does not run .github/scripts/pr_stuck.sh")
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/pr_stuck_test.sh") {
		t.Error("ci: pr_stuck.sh is not tested (pr_stuck_test.sh)")
	}
}
