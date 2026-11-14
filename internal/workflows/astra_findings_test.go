// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"strings"
	"testing"
)

// The findings of the outside check are reconciled as a whole list, not
// one event (plan 1.9, «Результаты Астры не теряются», item 3; owner's
// decision 14.11): of 36 findings of 14.11, 12 had no plan line, and a
// feed line posted as a review comment woke nobody. The workflow runs on
// the events of a finding and of the feed — reviews and review comments
// too — and every hour; it runs the script of main with no secret, hands
// the script nothing of the event, and has the least rights: read the
// issues, the pull requests and the plan, write a check run. What the
// script reports is astra_findings_test.sh's, which ci runs.
func TestWorkflows_AstraFindingsAreReconciled(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["astra-findings.yml"]
	if !ok {
		t.Fatal("no astra-findings.yml: a finding with no owner or a feed line nobody read stays unseen")
	}
	for _, trig := range []string{"issues", "issue_comment", "pull_request_review", "pull_request_review_comment", "schedule"} {
		if _, ok := w.On[trig]; !ok {
			t.Errorf("astra-findings: no trigger %s — a finding or a feed line of that kind is not reconciled when it comes", trig)
		}
	}
	for trig := range w.On {
		if trig == "pull_request_target" || trig == "pull_request" || trig == "workflow_run" {
			t.Errorf("astra-findings: trigger %s — the reconciliation needs none", trig)
		}
	}
	if p, _ := w.Permissions.(map[string]any); len(p) != 0 {
		t.Errorf("astra-findings: workflow permissions %v, want {} (rights per job)", w.Permissions)
	}
	if len(w.Jobs) == 0 {
		t.Fatal("astra-findings: no jobs")
	}
	for name, j := range w.Jobs {
		p, _ := j.Permissions.(map[string]any)
		want := map[string]string{"contents": "read", "issues": "read", "pull-requests": "read", "checks": "write"}
		if len(p) != len(want) {
			t.Errorf("astra-findings: job %s permissions %v, want %v alone", name, p, want)
		}
		for k, v := range want {
			if p[k] != v {
				t.Errorf("astra-findings: job %s permission %s is %v, want %s", name, k, p[k], v)
			}
		}
		if j.TimeoutMinutes == 0 {
			t.Errorf("astra-findings: job %s has no timeout-minutes", name)
		}
		var feed string
		for _, st := range j.Steps {
			if strings.HasPrefix(st.Uses, "actions/checkout@") && fmt.Sprint(st.With["ref"]) != "main" {
				t.Errorf("astra-findings: job %s checks out %v, want ref: main — a review runs on the merge ref of the pull request", name, st.With["ref"])
			}
			for k, v := range st.Env {
				if strings.Contains(v, "secrets.") {
					t.Errorf("astra-findings: job %s env %s reads a secret: the reconciliation needs none", name, k)
				}
				if strings.Contains(v, "github.event") {
					t.Errorf("astra-findings: job %s env %s takes %q from the event: the script reads the lists itself", name, k, v)
				}
			}
			if f, ok := st.Env["FEED"]; ok {
				feed = f
			}
		}
		// The feed the job reads is the feed whose events start it.
		if feed == "" || strings.Count(j.If, "number == "+feed) != 2 {
			t.Errorf("astra-findings: job %s reads the feed %q, and its if %q does not start it on comments and reviews of that feed", name, feed, j.If)
		}
	}
	if !runsStep(w, ".github/scripts/astra_findings.py") {
		t.Error("astra-findings: does not run .github/scripts/astra_findings.py")
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/astra_findings_test.sh") {
		t.Error("ci: astra_findings.py is not tested (astra_findings_test.sh)")
	}
}
