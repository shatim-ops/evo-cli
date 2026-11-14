// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"slices"
	"testing"
)

// Every live run gets a report (plan 1.9, «Каждый живой прогон — отчёт»,
// the owner's decision of 14.11): the review of api-probe 54 and cloud-e2e
// 218 and 219 waited 3–4.5 hours, no one owned it. A completed run of each
// workflow that touches the cloud opens an issue; an hourly check is red on
// a run without one, an issue without a link to its report and one open
// for over a day, and main-red carries that red to «релиз». The rights are
// the least the script needs: no contents, no pull requests, no secrets.
// What the script does is live_run_report_test.sh's, which ci runs.
func TestWorkflows_LiveRunsGetAReport(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["live-run-report.yml"]
	if !ok {
		t.Fatal("no live-run-report.yml: a live run reaches no one who reviews it")
	}
	trig, _ := w.On["workflow_run"].(map[string]any)
	if fmt.Sprint(trig["workflows"]) != "[cloud-e2e api-probe cloud-readonly-os stand]" || !slices.Contains(trig["types"].([]any), any("completed")) {
		t.Errorf("live-run-report: on workflow_run %v, want cloud-e2e, api-probe, cloud-readonly-os and stand completed", trig)
	}
	// Every branch: a live run from a branch touches the cloud as well.
	if _, has := trig["branches"]; has {
		t.Errorf("live-run-report: workflow_run limited to branches %v", trig["branches"])
	}
	if _, has := w.On["schedule"]; !has {
		t.Error("live-run-report: no schedule — nothing checks that a report came")
	}
	if p, _ := w.Permissions.(map[string]any); len(p) != 0 {
		t.Errorf("live-run-report: workflow permissions %v, want {} (rights per job)", w.Permissions)
	}
	want := map[string]map[string]string{
		"open":  {"actions": "read", "issues": "write"},
		"check": {"actions": "read", "issues": "read"},
	}
	for name, j := range w.Jobs {
		p, _ := j.Permissions.(map[string]any)
		if fmt.Sprint(p) != fmt.Sprint(want[name]) {
			t.Errorf("live-run-report: job %s permissions %v, want %v", name, j.Permissions, want[name])
		}
	}
	if len(w.Jobs) != len(want) {
		t.Errorf("live-run-report: jobs %d, want open and check", len(w.Jobs))
	}
	for _, s := range []string{".github/scripts/live_run_report.sh open", ".github/scripts/live_run_report.sh check"} {
		if !runsStep(w, s) {
			t.Errorf("live-run-report: does not run %s", s)
		}
	}
	// The red of the hourly check reaches «релиз» the way a red main does.
	trig, _ = ws["main-red.yml"].On["workflow_run"].(map[string]any)
	if !slices.Contains(trig["workflows"].([]any), any("live-run-report")) {
		t.Error("main-red does not follow live-run-report: its red reaches no one")
	}
}
