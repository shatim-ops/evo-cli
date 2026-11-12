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
