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

// standPrefix is the declaration of the prefix of the stand in e2e_stand.sh.
var standPrefix = regexp.MustCompile(`(?m)^STAND_PREFIX=([a-z0-9-]+)$`)

// The stand (plan 1.9, Г-50) is a set of cloud resources that outlives the
// runs of cloud-e2e: operations of the second day are tried on it until the
// tag. The cleanup of the runs must not take it, and it must stay findable
// as a whole. Its prefix is declared once, in e2e_stand.sh, and is not a
// prefix of the runs: every deleting step of the cleanup and every sweeping
// script selects by evo-e2e- (or by the scripts that guard that prefix), so
// a resource of the stand matches none of them. The exception is the name,
// not a kind of resource left out of the cleanup.
func TestWorkflows_CleanupLeavesTheStand(t *testing.T) {
	root := filepath.Join("..", "..", ".github")
	decl, err := os.ReadFile(filepath.Join(root, "scripts", "e2e_stand.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := standPrefix.FindAllSubmatch(decl, -1)
	if len(m) != 1 {
		t.Fatalf("e2e_stand.sh: %d declarations STAND_PREFIX=…, want exactly one", len(m))
	}
	prefix := string(m[0][1])
	for _, runs := range []string{"evo-e2e-", "fip-evo-e2e-"} {
		if strings.HasPrefix(prefix, runs) || strings.HasPrefix("fip-"+prefix, runs) {
			t.Errorf("the prefix of the stand %s is a prefix of the runs (%s): the cleanup would take the stand", prefix, runs)
		}
	}
	if !strings.HasPrefix(prefix, "evo-") {
		t.Errorf("the prefix of the stand %s does not start with evo-: the report of disks left without a VM (evo-*) would not see a disk of the stand", prefix)
	}

	// Declared once: nobody else spells the prefix.
	for _, dir := range []string{"scripts", "workflows"} {
		files, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.IsDir() || f.Name() == "e2e_stand.sh" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, dir, f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), prefix) {
				t.Errorf(".github/%s/%s spells the prefix of the stand %s: it is declared in e2e_stand.sh only — source it", dir, f.Name(), prefix)
			}
		}
	}

	// The guards of the sweeping scripts: a prefix or a name that is not of
	// the runs is refused before anything is listed.
	for file, guard := range map[string]string{
		"cloud-live.sh":   `case "$p" in evo-e2e-*) ;; *)`,
		"dbaas-delete.sh": `case "$name" in evo-e2e-*) ;; *)`,
	} {
		b, err := os.ReadFile(filepath.Join(root, "scripts", file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), guard) {
			t.Errorf("%s: no guard %s — a sweep by another prefix would not be refused", file, guard)
		}
	}

	// Every deleting step of the cleanup names the prefix of the runs or
	// calls the sweep of rules, which selects by rules, not by names (its
	// own test holds what it leaves); none of them knows the stand.
	j, ok := readWorkflows(t)["cloud-e2e.yml"].Jobs["cleanup"]
	if !ok {
		t.Fatal("cloud-e2e: no job cleanup")
	}
	seen := 0
	for _, s := range j.Steps {
		if strings.Contains(s.Run, "e2e_stand.sh") {
			t.Errorf("cloud-e2e cleanup, step %q: calls e2e_stand.sh — the stand has a mode of its own, not a step of the cleanup of the runs", s.Name)
		}
		if !deletes.MatchString(s.Run) {
			continue
		}
		seen++
		if !strings.Contains(s.Run, "evo-e2e-") && !strings.Contains(s.Run, "e2e_rules_sweep.sh") {
			t.Errorf("cloud-e2e cleanup, step %q: deletes without the prefix evo-e2e- — what it selects could be of the stand", s.Name)
		}
	}
	if seen < 5 {
		t.Errorf("cloud-e2e cleanup: %d deleting steps found, want at least 5 — the pattern no longer sees them", seen)
	}
}

// standKinds is the declaration of the kinds of the stand in e2e_stand.sh.
var standKinds = regexp.MustCompile(`(?m)^STAND_KINDS="([a-z0-9 ]+)"$`)

// sweepKinds is the declaration of the kinds the cleanup of the runs finds.
var sweepKinds = regexp.MustCompile(`(?m)^KINDS="([a-z0-9 ]+)"$`)

// The sweep of the stand keeps a list of kinds of its own: the cleanup of
// the runs must not be able to take the prefix of the stand, so the two do
// not share the code that selects. What keeps them from drifting apart:
// every kind of the stand is a kind the cleanup of the runs knows how to
// find — a kind of cloud.live.sh, or a cluster the job cleanup deletes
// (Kubernetes, PostgreSQL). A kind added to the stand alone has no cleanup
// that was ever run live.
func TestWorkflows_StandKindsAreKindsOfTheCleanup(t *testing.T) {
	root := filepath.Join("..", "..", ".github", "scripts")
	stand, err := os.ReadFile(filepath.Join(root, "e2e_stand.sh"))
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(filepath.Join(root, "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	sk, lk := standKinds.FindSubmatch(stand), sweepKinds.FindSubmatch(live)
	if sk == nil || lk == nil {
		t.Fatalf("declarations not found: STAND_KINDS in e2e_stand.sh: %v, KINDS in cloud-live.sh: %v", sk != nil, lk != nil)
	}
	known := map[string]bool{"k8s": true, "pg": true}
	for _, k := range strings.Fields(string(lk[1])) {
		known[k] = true
	}
	kinds := strings.Fields(string(sk[1]))
	if len(kinds) == 0 {
		t.Fatal("e2e_stand.sh: STAND_KINDS is empty")
	}
	for _, k := range kinds {
		if !known[k] {
			t.Errorf("e2e_stand.sh: the kind %s of the stand is not a kind of the cleanup of the runs (%s, k8s, pg)", k, lk[1])
		}
	}
	// What itself spends and spawns goes first (conclusion 158 of the risk
	// role): protection plans and backups before the clusters and the VM.
	if len(kinds) < 2 || kinds[0] != "pplan" || kinds[1] != "backup" {
		t.Errorf("e2e_stand.sh: STAND_KINDS starts with %v, want pplan and backup first — a plan left for last goes on making paid backups", kinds)
	}
}

// The stand has a workflow of its own (plan 1.9, Г-50; conclusion 158 of the
// risk role): the cleanup of the runs passes it by, so nothing else would
// say that it is still there. stand.yml reads it every day with the key
// that only reads — no approval, red when it is past its date or its tag —
// and sweeps it only by hand: the job of the sweep runs in the environment
// that asks the owner, takes the confirmation from the input of the run
// through the environment of the step, and is not the job that reads.
func TestWorkflows_StandHasAWorkflowOfItsOwn(t *testing.T) {
	ws := readWorkflows(t)
	w, ok := ws["stand.yml"]
	if !ok {
		t.Fatal("no workflow stand.yml")
	}
	if _, daily := w.On["schedule"]; !daily {
		t.Error("stand.yml: no schedule — the date of the stand is checked only when somebody remembers")
	}
	if _, byHand := w.On["workflow_dispatch"]; !byHand || len(w.On) != 2 {
		t.Errorf("stand.yml: triggers %v, want schedule and workflow_dispatch only — code of a pull request must not reach the keys", w.On)
	}
	if w.Concurrency.Group != "stand" || w.Concurrency.CancelInProgress != "false" {
		t.Errorf("stand.yml: concurrency %+v, want the group stand without cancel — a sweep must not be cut in the middle", w.Concurrency)
	}
	list, sweep := w.Jobs["list"], w.Jobs["sweep"]
	var listRun, sweepRun strings.Builder
	for _, s := range list.Steps {
		listRun.WriteString(s.Run + "\n")
		if _, has := s.Env["STAND_SWEEP"]; has {
			t.Errorf("stand.yml, job list, step %q: has STAND_SWEEP — the job that reads must not be able to delete", s.Name)
		}
	}
	confirmed := false
	for _, s := range sweep.Steps {
		sweepRun.WriteString(s.Run + "\n")
		if strings.Contains(s.Run, "inputs.") {
			t.Errorf("stand.yml, job sweep, step %q: the input of the run is in the script — pass it through env", s.Name)
		}
		if s.Env["STAND_SWEEP"] == "${{ inputs.sweep }}" {
			confirmed = true
		}
	}
	if list.Environment != "cloud-readonly" || !strings.Contains(listRun.String(), "e2e_stand.sh list") || strings.Contains(listRun.String(), "e2e_stand.sh sweep") {
		t.Errorf("stand.yml, job list: environment %q, want cloud-readonly, and only e2e_stand.sh list in its steps", list.Environment)
	}
	if !strings.Contains(list.If, "inputs.sweep == ''") || !strings.Contains(list.If, "refs/heads/main") {
		t.Errorf("stand.yml, job list: if %q, want main and no sweep asked", list.If)
	}
	if sweep.Environment != "cloud-e2e" || !strings.Contains(sweepRun.String(), "e2e_stand.sh sweep") || !confirmed {
		t.Errorf("stand.yml, job sweep: environment %q (want cloud-e2e, the approval of the owner), e2e_stand.sh sweep in the steps, STAND_SWEEP from inputs.sweep through env: %v", sweep.Environment, confirmed)
	}
	if !strings.Contains(sweep.If, "inputs.sweep != ''") || !strings.Contains(sweep.If, "refs/heads/main") {
		t.Errorf("stand.yml, job sweep: if %q, want main and the confirmation typed", sweep.If)
	}
	// Nobody else sweeps the stand.
	for name, other := range ws {
		if name == "stand.yml" {
			continue
		}
		for jn, j := range other.Jobs {
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "e2e_stand.sh sweep") || s.Env["STAND_SWEEP"] != "" {
					t.Errorf("%s, job %s, step %q: sweeps the stand — only stand.yml does", name, jn, s.Name)
				}
			}
		}
	}
}
