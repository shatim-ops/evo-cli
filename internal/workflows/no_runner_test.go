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

// A job of ci that GitHub gave no runner is run again once by itself
// (process backlog 1.9): on 12–13.11 seven test (macos-latest) jobs were
// cancelled with no step exactly 15 min after they were made, on pull
// requests and on main, and a rerun passed every time. The rerun follows
// every completed ci — a pull request's and main's — with the least right
// it needs (actions: write, to rerun), on the default branch: no code of a
// pull request runs there, and the run comes to the script through env
// (TestWorkflows_EventTextNeverInRunScripts). Which runs it reruns and
// which it leaves (a second attempt, a failure beside, a moved head) is
// no_runner_test.sh's, which ci runs.
func TestWorkflows_NoRunnerRerunsOnce(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["no-runner.yml"]
	if !ok {
		t.Fatal("no no-runner.yml: a job given no runner waits for a rerun by hand")
	}
	trig, _ := w.On["workflow_run"].(map[string]any)
	if fmt.Sprint(trig["workflows"]) != "[ci]" || !slices.Contains(trig["types"].([]any), any("completed")) {
		t.Errorf("no-runner: on workflow_run %v, want ci completed", trig)
	}
	if len(w.On) != 1 {
		t.Errorf("no-runner: triggers %v, want workflow_run alone", w.On)
	}
	if p, _ := w.Permissions.(map[string]any); len(p) != 0 {
		t.Errorf("no-runner: workflow permissions %v, want {} (rights per job)", w.Permissions)
	}
	for name, j := range w.Jobs {
		if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["actions"] != "write" {
			t.Errorf("no-runner: job %s permissions %v, want actions: write alone", name, j.Permissions)
		}
		// The first attempt alone: a second cancel with no runner goes to
		// «релиз» by hand (CLAUDE.md §0, rule 2), and a loop of reruns
		// would hold a macOS slot for nothing.
		for _, c := range []string{"github.event.workflow_run.conclusion == 'cancelled'", "github.event.workflow_run.run_attempt == 1"} {
			if !strings.Contains(j.If, c) {
				t.Errorf("no-runner: job %s runs on %q, want %s", name, j.If, c)
			}
		}
	}
	if !runsStep(w, ".github/scripts/no_runner.sh") {
		t.Error("no-runner: does not run .github/scripts/no_runner.sh")
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/no_runner_test.sh") {
		t.Error("ci: no_runner.sh is not tested (no_runner_test.sh)")
	}
}
