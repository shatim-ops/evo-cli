// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deletes is a call of a cleanup step that removes a cloud resource: a
// deleting tool or command of evo, a raw DELETE, a script of the cleanup.
var deletes = regexp.MustCompile(`tools call (destroy|release_floating_ip|\w*delete\w*)|evo [^|\n]*\b(destroy|delete)\b [^|\n]*"\$|-X DELETE|dbaas-delete\.sh|cloud-live\.sh sweep|e2e_rules_sweep\.sh`)

// stepScript is a script of .github/scripts a step runs.
var stepScript = regexp.MustCompile(`\.github/scripts/([a-z0-9_-]+\.sh)`)

// cleanup_only runs in a concurrency group of its own (owner decision
// 12.11, plan 1.9, Вп-14): the schedule of live probes is 13 runs in 8 days
// and a cleanup that waits for a live run — or holds one back — costs a day
// of the window. Next to a live run the cleanup must not delete what that
// run has just created: every step of the job that deletes takes only the
// resources of finished runs — it reads the runs alive with
// e2e_live_runs.sh, itself or through the script it calls — and the job may
// read the runs (actions: read, GH_TOKEN). A new kind of resource added to
// the cleanup without the filter fails here.
func TestWorkflows_CleanupTouchesOnlyFinishedRuns(t *testing.T) {
	w := readWorkflows(t)["cloud-e2e.yml"]
	if !strings.Contains(strings.ReplaceAll(w.Concurrency.Group, " ", ""), "inputs.cleanup_only&&'cloud-e2e-cleanup'") {
		t.Errorf("cloud-e2e: concurrency group %q — cleanup_only has no group of its own", w.Concurrency.Group)
	}
	// A new run does not cancel a cleanup in the middle of its deletes.
	if w.Concurrency.CancelInProgress != "false" {
		t.Errorf("cloud-e2e: cancel-in-progress %q — a cleanup cut in the middle leaves the resources", w.Concurrency.CancelInProgress)
	}
	j, ok := w.Jobs["cleanup"]
	if !ok {
		t.Fatal("cloud-e2e: no job cleanup — the job moved, update the test")
	}
	if p, _ := j.Permissions.(map[string]any); len(p) != 2 || p["actions"] != "read" || p["contents"] != "read" {
		t.Errorf("cloud-e2e: job cleanup permissions %v, want contents: read and actions: read alone", j.Permissions)
	}
	if j.Env["GH_TOKEN"] != "${{ github.token }}" {
		t.Errorf("cloud-e2e: job cleanup has no GH_TOKEN — e2e_live_runs.sh cannot read the runs")
	}
	// The token reads the runs and nothing else, and no code of a pull
	// request meets it: the workflow starts by hand or on schedule only, and
	// the job waits for the owner's approval of environment cloud-e2e.
	for trigger := range w.On {
		if trigger != "schedule" && trigger != "workflow_dispatch" {
			t.Errorf("cloud-e2e: trigger %q — the cleanup's GH_TOKEN and the cloud keys would meet code nobody approved", trigger)
		}
	}
	if j.Environment != "cloud-e2e" || !strings.Contains(j.If, "inputs.cleanup_only") {
		t.Errorf("cloud-e2e: job cleanup (environment %q, if %q) — want the owner's approval and the input cleanup_only", j.Environment, j.If)
	}
	const filter = "e2e_live_runs.sh"
	found := 0
	for _, s := range j.Steps {
		if !deletes.MatchString(s.Run) {
			continue
		}
		found++
		filtered := strings.Contains(s.Run, filter)
		for _, m := range stepScript.FindAllStringSubmatch(s.Run, -1) {
			b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", m[1]))
			if err != nil {
				t.Fatal(err)
			}
			filtered = filtered || strings.Contains(string(b), filter)
		}
		if !filtered {
			t.Errorf("cloud-e2e: step «%s» of job cleanup deletes without %s — it would delete the resources of a run that is still alive", s.Name, filter)
		}
	}
	if found < 5 {
		t.Fatalf("%d deleting steps in job cleanup — the check sees nothing, update deletes", found)
	}
	// The filter refuses when the runs are not read: a caller that puts it
	// into an argument («--arg live "$(…)"») loses its exit code.
	arg := regexp.MustCompile(`--arg\s+\w+\s+"\$\([^)]*` + regexp.QuoteMeta(filter))
	files, _ := filepath.Glob(filepath.Join("..", "..", ".github", "scripts", "*.sh"))
	texts := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts[filepath.Base(f)] = string(b)
	}
	for _, s := range j.Steps {
		texts["cloud-e2e.yml: "+s.Name] = s.Run
	}
	for name, text := range texts {
		if arg.MatchString(text) {
			t.Errorf("%s: %s inside an argument — its failure is lost and the cleanup deletes everything", name, filter)
		}
	}
}
