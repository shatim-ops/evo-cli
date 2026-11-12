// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// probeOn is a probe cloud-live.sh runs when its word is in probes.
	probeOn = regexp.MustCompile(`probe_on ([a-z0-9]+)`)
	// probeIf is a step of cloud-e2e.yml that runs when its word is in probes.
	probeIf = regexp.MustCompile(`contains\(format\(' \{0\} ', inputs\.probes\), ' ([a-z0-9]+) '\)`)
	// casePattern is a pattern line of a shell case: «a | b)».
	casePattern = regexp.MustCompile(`(?m)^\s*([a-z0-9]+(?:\s*\|\s*[a-z0-9]+)*)\)`)
)

// caseWords are the words the case statement after the first marker in
// text accepts: its pattern lines up to the closing esac.
func caseWords(t *testing.T, file, text, marker string) map[string]bool {
	t.Helper()
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("%s: no %q — the check sees nothing, update it", file, marker)
	}
	block := text[i:]
	if j := strings.Index(block, "esac"); j >= 0 {
		block = block[:j]
	}
	words := map[string]bool{}
	for _, m := range casePattern.FindAllStringSubmatch(block, -1) {
		for _, w := range strings.Split(m[1], "|") {
			words[strings.TrimSpace(w)] = true
		}
	}
	if len(words) < 4 {
		t.Fatalf("%s: %d words in the case after %q — the check sees nothing", file, len(words), marker)
	}
	return words
}

// Every word of the input probes that a probe reads (probe_on in
// cloud-live.sh, a step's if in cloud-e2e.yml) is one the preflight
// accepts and one the dbaas-pg job, which reads the same input, does not
// refuse as unknown. cloud-e2e run 170: the probe shared was in
// cloud-live.sh, the preflight refused the word and nothing ran (#911).
func TestWorkflows_ProbeWordsAcceptedEverywhere(t *testing.T) {
	read := func(parts ...string) string {
		b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	live := read(".github", "scripts", "cloud-live.sh")
	wf := read(".github", "workflows", "cloud-e2e.yml")
	pre := read(".github", "scripts", "e2e-preflight.sh")

	used := map[string]string{}
	for _, m := range probeOn.FindAllStringSubmatch(live, -1) {
		used[m[1]] = "cloud-live.sh"
	}
	for _, m := range probeIf.FindAllStringSubmatch(wf, -1) {
		used[m[1]] = "cloud-e2e.yml"
	}
	if len(used) < 5 {
		t.Fatalf("%d probe words found — the check sees nothing, update probeOn and probeIf", len(used))
	}
	preflight := caseWords(t, "e2e-preflight.sh", pre, `for w in ${probe_words`)
	pg := caseWords(t, "cloud-e2e.yml", wf, `read -ra words <<<"${PROBES`)

	var bad []string
	for w, where := range used {
		if !preflight[w] {
			bad = append(bad, w+" ("+where+"): e2e-preflight.sh refuses it — add it to its case")
		}
	}
	for w := range preflight {
		if !pg[w] {
			bad = append(bad, w+": the preflight accepts it, the dbaas-pg job refuses it as unknown — add it to that case")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// A probe of the schedule 1.9 (probe_on in area_ops of cloud-live.sh) runs
// only when live-plan puts the area ops into the matrix for its word, and
// the area's sweep knows every kind of resource the area's probes create
// (area_kinds ops is a part of KINDS, which cleanup_only sweeps): a probe
// whose word starts nothing, or whose resource no cleanup finds, is not run
// (docs/internal/phase-0/1.9.md, 1.1 rule 3).
func TestWorkflows_OpsProbesHaveTheirArea(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	live := string(b)
	i := strings.Index(live, "\narea_ops() {")
	if i < 0 {
		t.Fatal("cloud-live.sh: no area_ops — the area moved, update the test")
	}
	body := live[i:]
	body = body[:strings.Index(body, "\n}\n")]
	words := probeOn.FindAllStringSubmatch(body, -1)
	if len(words) == 0 {
		t.Fatal("cloud-live.sh: area_ops runs no probe — the check sees nothing")
	}
	var plan string
	for _, s := range readWorkflows(t)["cloud-e2e.yml"].Jobs["live-plan"].Steps {
		if s.ID == "areas" {
			plan = s.Run
		}
	}
	j := strings.Index(plan, "for w in ${pw")
	if j < 0 {
		t.Fatal("live-plan: no loop over the probe words — update the test")
	}
	loop := plan[j:]
	loop = loop[:strings.Index(loop, "done")]
	accepted := map[string]bool{}
	for _, m := range casePattern.FindAllStringSubmatch(loop, -1) {
		for _, w := range strings.Split(m[1], "|") {
			accepted[strings.TrimSpace(w)] = true
		}
	}
	for _, m := range words {
		if !accepted[m[1]] {
			t.Errorf("probe %s of area ops: live-plan does not start the area for the word — add it to its case", m[1])
		}
	}
	if !strings.Contains(plan, "a+=(ops)") {
		t.Error("live-plan: the words of area ops do not add the area ops")
	}
	kinds := regexp.MustCompile(`(?m)^KINDS="([^"]+)"`).FindStringSubmatch(live)
	ops := regexp.MustCompile(`(?m)^\s*ops\) echo "([^"]+)"`).FindStringSubmatch(live)
	if kinds == nil || ops == nil {
		t.Fatal("cloud-live.sh: no KINDS or no kinds of area ops — update the test")
	}
	for _, k := range strings.Fields(ops[1]) {
		if !strings.Contains(" "+kinds[1]+" ", " "+k+" ") {
			t.Errorf("area ops creates %q, which the sweep of cleanup_only (KINDS) does not know", k)
		}
	}
}

// sweepFixedArea is a sweep of one named area: «sweep PREFIX area», the
// area a bare word that ends the line (the prefix may hold spaces).
var sweepFixedArea = regexp.MustCompile(`cloud-live\.sh sweep .*\s([a-z0-9]+)$`)

// The sweep reads the backups and the protection plans of the probes
// (kinds backup and pplan of cloud-live.sh) by raw requests: they need the
// project and the IAM key. A sweep step that reads those kinds — every
// kind, the area of the matrix, or the area ops — without PROJECT, KEY_ID
// and SECRET cannot read the lists and fails; a step nobody gave the
// project to would turn every cleanup red (or, read as «nothing left»,
// leave billable backups behind).
func TestWorkflows_SweepOfBackupsHasTheProject(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	live := string(b)
	// The areas whose kinds hold backup or pplan.
	withBackups := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*([a-z0-9]+)\) echo "([^"]+)" ;;`).FindAllStringSubmatch(live, -1) {
		if strings.Contains(" "+m[2]+" ", " backup ") || strings.Contains(" "+m[2]+" ", " pplan ") {
			withBackups[m[1]] = true
		}
	}
	if !withBackups["ops"] {
		t.Fatal("cloud-live.sh: area ops does not sweep backup or pplan — the check sees nothing, update it")
	}
	seen := 0
	for file, wf := range readWorkflows(t) {
		for name, job := range wf.Jobs {
			for _, s := range job.Steps {
				for _, line := range strings.Split(s.Run, "\n") {
					if !strings.Contains(line, "cloud-live.sh sweep ") {
						continue
					}
					seen++
					if m := sweepFixedArea.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !withBackups[m[1]] {
						continue
					}
					for _, v := range []string{"PROJECT", "KEY_ID", "SECRET"} {
						if s.Env[v] == "" {
							t.Errorf("%s, job %s, step %q: the sweep reads backups and protection plans, the step has no %s", file, name, s.Name, v)
						}
					}
				}
			}
		}
	}
	if seen < 3 {
		t.Fatalf("%d sweep steps found — the check sees nothing, update it", seen)
	}
}

// The resources of the price probe Г-20 (the word g20 in the name) outlive
// their run: e2e_live_runs.sh keeps them from every sweep until cleanup_only
// runs with the word g20 in the input probes. The word reaches the script
// through PROBES of the job cleanup; without it the kept VM, its disk and
// its address would never be deleted (plan 1.9, Г-20; R-77).
func TestWorkflows_CleanupGetsTheProbeWords(t *testing.T) {
	job, ok := readWorkflows(t)["cloud-e2e.yml"].Jobs["cleanup"]
	if !ok {
		t.Fatal("cloud-e2e.yml: no job cleanup — update the test")
	}
	wf := readWorkflows(t)["cloud-e2e.yml"]
	dispatch, _ := wf.On["workflow_dispatch"].(map[string]any)
	inputs, _ := dispatch["inputs"].(map[string]any)
	if _, ok := inputs["probes"]; !ok {
		t.Error("cloud-e2e.yml: no input probes — the word g20 could never be given to cleanup_only")
	}
	if got := job.Env["PROBES"]; got != "${{ inputs.probes }}" {
		t.Errorf("job cleanup: env PROBES = %q, want the input probes: the word g20 would not reach e2e_live_runs.sh", got)
	}
	// The exception has a term: the last word of every cleanup is the age
	// of the kept VM, and a VM past the term fails the job (risk
	// conclusion 136: a resource no sweep deletes must not live unseen).
	term := false
	for _, s := range job.Steps {
		if strings.Contains(s.Run, "e2e_live_runs.sh overdue") && strings.Contains(s.If, "always()") {
			term = true
		}
	}
	if !term {
		t.Error("job cleanup: no always() step runs e2e_live_runs.sh overdue — the VM of the price probe would live without a term")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "e2e_live_runs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `case " ${PROBES:-} " in *" g20 "*)`) {
		t.Error("e2e_live_runs.sh: the word g20 of PROBES is not read — update the test or the script")
	}
}
