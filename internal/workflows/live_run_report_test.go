// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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

// notLiveWithAWriteKey are the jobs of NOT_LIVE that may run with the key
// of cloud-e2e, with the reason they still change nothing: a standing
// exception, not a debt — a new line is a decision with its reason.
var notLiveWithAWriteKey = map[string]string{
	"preflight": "on a branch it runs in cloud-e2e after the owner's click and only reads (e2e-preflight.sh); on main — cloud-readonly",
}

// A job of NOT_LIVE gets no report, so it must not be able to change the
// cloud: it is a job of cloud-e2e whose environment holds a key only for
// reading (cloud-readonly) or none; a key of cloud-e2e — only in
// notLiveWithAWriteKey with its reason. And a job of cloud-readonly is in
// NOT_LIVE: it reads, a report of it is noise («релиз» 17.11: the nightly
// readonly job, cloud-e2e 220 and 221; Qwen on #2099).
func TestWorkflows_NotLiveJobsOnlyRead(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "live_run_report.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^NOT_LIVE="([^"]*)"$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("live_run_report.sh: no NOT_LIVE=\"…\"")
	}
	notLive := strings.Fields(string(m[1]))
	jobs := readWorkflows(t)["cloud-e2e.yml"].Jobs
	for _, name := range notLive {
		j, ok := jobs[name]
		if !ok {
			t.Errorf("NOT_LIVE names %s, cloud-e2e has no such job", name)
			continue
		}
		_, declared := notLiveWithAWriteKey[name]
		if env := j.Environment; env != "" && env != "cloud-readonly" && !declared {
			t.Errorf("NOT_LIVE job %s runs in environment %q, want cloud-readonly or none", name, env)
		}
	}
	for name := range notLiveWithAWriteKey {
		if !slices.Contains(notLive, name) {
			t.Errorf("notLiveWithAWriteKey names %s, NOT_LIVE has no such job: drop the line", name)
		}
	}
	for name, j := range jobs {
		if j.Environment == "cloud-readonly" && !slices.Contains(notLive, name) {
			t.Errorf("cloud-e2e job %s reads with cloud-readonly but is not in NOT_LIVE", name)
		}
	}
}
