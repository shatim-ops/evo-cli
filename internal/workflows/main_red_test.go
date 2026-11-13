// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A red run of ci on main reaches an issue by itself (process backlog 1.7,
// #796): main was red six runs in a row after #769 and no one saw it,
// because a pull request runs Windows on a few packages only (#578). The
// signal follows every completed ci of main — push and the nightly full
// matrix — with the least rights it needs (the jobs' logs and the issue),
// on the default branch: no code of a pull request runs there, and the run
// comes to the script through env (TestWorkflows_EventTextNeverInRunScripts).
// What the script does with a red, a green and a cancelled run is
// main_red_test.sh's, which ci runs.
func TestWorkflows_RedMainOpensAnIssue(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["main-red.yml"]
	if !ok {
		t.Fatal("no main-red.yml: a red ci on main reaches no one")
	}
	trig, _ := w.On["workflow_run"].(map[string]any)
	// stand — the daily reading of the stand (plan 1.9, Г-50): a stand past
	// its date is red in a run by schedule, and a red nobody is told about
	// is no signal.
	// pr-stuck — the round over the open pull requests (journal 1.9, «Тихое
	// красное»): a round that fails by schedule tells nobody about a stuck
	// pull request, so its own red must reach an issue too.
	if fmt.Sprint(trig["workflows"]) != "[ci cloud-e2e stand pr-stuck]" || !slices.Contains(trig["types"].([]any), any("completed")) || fmt.Sprint(trig["branches"]) != "[main]" {
		t.Errorf("main-red: on workflow_run %v, want ci, cloud-e2e, stand and pr-stuck completed on main", trig)
	}
	if len(w.On) != 1 {
		t.Errorf("main-red: triggers %v, want workflow_run alone", w.On)
	}
	if p, _ := w.Permissions.(map[string]any); len(p) != 0 {
		t.Errorf("main-red: workflow permissions %v, want {} (rights per job)", w.Permissions)
	}
	for name, j := range w.Jobs {
		p, _ := j.Permissions.(map[string]any)
		// pull-requests: write — one line into every open pull request
		// when main turns red and when it is green again (plan 1.9, «CI»,
		// М5): the issue alone stood six hours unread on 12.11, it has no
		// addressee, and a comment in a pull request reaches its author.
		if len(p) != 3 || p["actions"] != "read" || p["issues"] != "write" || p["pull-requests"] != "write" {
			t.Errorf("main-red: job %s permissions %v, want actions: read, issues: write and pull-requests: write alone", name, j.Permissions)
		}
		for _, ev := range []string{"'push'", "'schedule'"} {
			if !strings.Contains(j.If, "github.event.workflow_run.event == "+ev) {
				t.Errorf("main-red: job %s does not follow a run of %s", name, ev)
			}
		}
		for _, c := range []string{"'failure'", "'success'"} {
			if !strings.Contains(j.If, "github.event.workflow_run.conclusion == "+c) {
				t.Errorf("main-red: job %s does not follow a run that ends in %s", name, c)
			}
		}
	}
	if !runsStep(w, ".github/scripts/main_red.sh") {
		t.Error("main-red: does not run .github/scripts/main_red.sh")
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/main_red_test.sh") {
		t.Error("ci: main_red.sh is not tested (main_red_test.sh)")
	}
}
