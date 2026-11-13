// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"strings"
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
	// The triggers: a schedule, by hand, and the end of a run of ci. No
	// event of a pull request itself — a workflow_run runs the workflow of
	// the default branch, so no code of a pull request runs with this token.
	// The schedule alone does not do: GitHub ran the schedules of this
	// repository about seven hours late (the nightly ci and cloud-e2e of
	// 11 and 12.11, four runs of four), and a round that comes hours later
	// tells nobody in time. The end of a ci run is when both cases appear:
	// a job no runner took — at the end of the run of the pull request; a
	// conflict — when a merge moved main, and ci of main ends.
	for trig := range w.On {
		if trig != "schedule" && trig != "workflow_dispatch" && trig != "workflow_run" {
			t.Errorf("pr-stuck: trigger %s, want schedule, workflow_dispatch and workflow_run alone (no event of a pull request)", trig)
		}
	}
	if _, ok := w.On["schedule"]; !ok {
		t.Error("pr-stuck: no schedule — the round is what finds a pull request nobody is told about")
	}
	run, _ := w.On["workflow_run"].(map[string]any)
	if fmt.Sprint(run["workflows"]) != "[ci]" || fmt.Sprint(run["types"]) != "[completed]" {
		t.Errorf("pr-stuck: on workflow_run %v, want ci completed — the schedule of GitHub comes hours late", run)
	}
	if _, filtered := run["branches"]; filtered {
		t.Error("pr-stuck: workflow_run is narrowed to branches — the run of a pull request must start the round too")
	}
	// A round by the end of a ci run is for the two cases the round exists
	// for: a push to main (a merge may have left open pull requests in
	// conflict) and a run that did not end green (a job no runner took
	// leaves the run failed). A green run of a pull request starts none:
	// every round spends the hourly request limit of the token, which
	// main-red and the review share (risk note 178).
	for name, j := range w.Jobs {
		for _, want := range []string{
			"github.event_name != 'workflow_run'",
			"github.event.workflow_run.event == 'push'",
			"github.event.workflow_run.conclusion != 'success'",
		} {
			if !strings.Contains(j.If, want) {
				t.Errorf("pr-stuck: job %s runs on %q, want %s in it — a green run of a pull request must not start a round", name, j.If, want)
			}
		}
	}
	// The checkout is the default branch, never the head of the run that
	// started the round.
	for name, j := range w.Jobs {
		for _, st := range j.Steps {
			if strings.Contains(fmt.Sprint(st.With), "workflow_run") || strings.Contains(st.Run, "github.event.workflow_run") || strings.Contains(fmt.Sprint(st.Env), "github.event.workflow_run") {
				t.Errorf("pr-stuck: job %s reads the run that started it (with %v, env %v, run %q): the round must not check out, run or be handed anything of a pull request", name, st.With, st.Env, st.Run)
			}
		}
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
