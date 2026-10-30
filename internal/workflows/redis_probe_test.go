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

var (
	cleanupKinds = regexp.MustCompile(`for k in ([a-z0-9 ]+);`)
	// preflightCall is a call of a tool in e2e-preflight.sh: «call VAR
	// tool», «evo tools call tool», «"${k}_list"» as k_list.
	preflightCall = regexp.MustCompile(`(?:\bcall [a-z]+ |evo tools call )"?(?:\$\{k\})?([a-z0-9_]+)`)
	// probeSecret is what names the project's resources, or opens them.
	probeSecret = regexp.MustCompile(`\$\{?(id|token|v|ZONE_ID|PROJECT|CLUSTER_ID)\b`)
)

// A cluster a job of cloud-e2e creates (its env CLUSTER) is deleted by its
// own job, whatever came before, and by cleanup_only, when that job was cut
// off (a timeout, a cancelled run): a cluster cleanup does not know of
// stays billed until someone sees it in the console. Names carry the run
// (TestWorkflows_E2ENamesCarryTheRun), so cleanup_only knows them by the
// prefix evo-e2e-: Kubernetes by its own step, DBaaS by dbaas-delete.sh for
// each kind with 'evo-e2e-*'.
func TestWorkflows_CleanupKnowsEveryCluster(t *testing.T) {
	jobs := readWorkflows(t)["cloud-e2e.yml"].Jobs
	var cleanup string
	for _, s := range jobs["cleanup"].Steps {
		cleanup += s.Run + "\n"
	}
	kinds := map[string]bool{}
	if m := cleanupKinds.FindStringSubmatch(cleanup); m != nil && strings.Contains(cleanup, `dbaas-delete.sh "$k" 'evo-e2e-*'`) {
		for _, k := range strings.Fields(m[1]) {
			kinds[k] = true
		}
	}
	kinds["k8s"] = strings.Contains(cleanup, `k8s_list`) && strings.Contains(cleanup, `startswith("evo-e2e-")`)
	found := 0
	for name, j := range jobs {
		c := j.Env["CLUSTER"]
		if c == "" {
			continue
		}
		found++
		if !strings.HasPrefix(c, "evo-e2e-") {
			t.Errorf("cloud-e2e job %s: cluster %q is not evo-e2e-*", name, c)
		}
		kind, own := "", false
		for _, s := range j.Steps {
			switch {
			case strings.Contains(s.Run, "k8s delete"):
				kind = "k8s"
			case strings.Contains(s.Run, "dbaas-delete.sh"):
				kind = j.Env["KIND"]
				if f := strings.Fields(s.Run[strings.Index(s.Run, "dbaas-delete.sh"):]); kind == "" && len(f) > 1 {
					kind = f[1]
				}
			default:
				continue
			}
			own = own || strings.Contains(s.If, "always()")
		}
		if !own {
			t.Errorf("cloud-e2e job %s: no step deletes cluster %s with if: always()", name, c)
		}
		if !kinds[kind] {
			t.Errorf("cloud-e2e job %s creates a %q cluster %s, which cleanup_only does not delete by the prefix evo-e2e-", name, kind, c)
		}
	}
	if found == 0 {
		t.Fatal("no job of cloud-e2e has env CLUSTER: the jobs moved, update the test")
	}
}

// runSuffix is the suffix of the run in the names of cloud-e2e's resources.
const runSuffix = "r${{ github.run_number }}a${{ github.run_attempt }}"

// Release 1.5 retro, run 147 and 148: clusters the platform does not delete
// (FAILED, DB-21, DB-17) and a VM in «Error» (OPS-18) kept the fixed names
// evo-e2e-pg, -kafka, -app and turned the jobs off. Every name a job of
// cloud-e2e creates carries the run (env RUN of the workflow, repeated in
// CLUSTER and VM, where the env context is not available); the zone of a
// service is its own input over zone, and the DBaaS jobs run alone
// (dbaas_services), so a probe of one service does not move the others; a
// cancelled run tries a deletion once instead of retrying up to 120 min
// (run 147 did not stop without a force cancel).
func TestWorkflows_E2ENamesCarryTheRun(t *testing.T) {
	w := readWorkflows(t)["cloud-e2e.yml"]
	if w.Env["RUN"] != runSuffix {
		t.Errorf("cloud-e2e: env RUN %q, want %q", w.Env["RUN"], runSuffix)
	}
	dispatch, _ := w.On["workflow_dispatch"].(map[string]any)
	inputs, _ := dispatch["inputs"].(map[string]any)
	if len(inputs) > 25 {
		t.Errorf("cloud-e2e: %d inputs, GitHub takes 25", len(inputs))
	}
	zones := map[string]string{
		"k8s-smoke": "k8s_zone", "dbaas-pg": "pg_zone", "dbaas-kafka": "kafka_zone",
		"dbaas-redis": "redis_zone", "redis-standalone-probe": "redis_zone",
	}
	for name, j := range w.Jobs {
		for _, v := range []string{"CLUSTER", "VM"} {
			if n := j.Env[v]; n != "" && !strings.HasSuffix(n, "-"+runSuffix) {
				t.Errorf("cloud-e2e job %s: %s %q does not end with -%s", name, v, n, runSuffix)
			}
		}
		if j.Env["RUN"] != "" {
			t.Errorf("cloud-e2e job %s: an env RUN of its own", name)
		}
		if in, ok := zones[name]; ok {
			want := "${{ inputs." + in + " || inputs.zone || 'ru.AZ-2' }}"
			if j.Env["ZONE"] != want {
				t.Errorf("cloud-e2e job %s: ZONE %q, want %q", name, j.Env["ZONE"], want)
			}
			if _, ok := inputs[in]; !ok {
				t.Errorf("cloud-e2e: no input %s", in)
			}
		}
		if k := j.Env["KIND"]; k != "" && !strings.Contains(j.If, "' "+k+" '") {
			t.Errorf("cloud-e2e job %s: runs on %q, not on its kind %s in dbaas_services", name, j.If, k)
		}
		if name == "cleanup" {
			continue
		}
		for _, s := range j.Steps {
			if !strings.Contains(s.If, "always()") {
				continue
			}
			once := s.Env["DBAAS_ONCE"]
			if strings.Contains(s.Run, "k8s delete") {
				once = s.Env["ONCE"]
			} else if !strings.Contains(s.Run, "dbaas-delete.sh") {
				continue
			}
			if once != "${{ job.status == 'cancelled' }}" {
				t.Errorf("cloud-e2e job %s, %s: a cancelled run retries the deletion (once %q)", name, s.Name, once)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "cloud-e2e.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"inputs.pg_cluster", `"evo-e2e-app"`, `"evo-e2e-k8s"`} {
		if strings.Contains(string(b), gone) {
			t.Errorf("cloud-e2e: %s — a fixed name, not one of the run", gone)
		}
	}
}

// Release 1.5 retro, run 147 and 148: jobs failed after the owner's approval
// and hours of waiting on what one read would have shown (names taken, a
// zone without a subnet). Every job of cloud-e2e that writes to the cloud
// (environment cloud-e2e) but cleanup_only waits for preflight, which only
// reads (the read-only key on main) and runs e2e-preflight.sh, tested in ci.
func TestWorkflows_E2EJobsWaitForPreflight(t *testing.T) {
	ws := readWorkflows(t)
	jobs := ws["cloud-e2e.yml"].Jobs
	pf, ok := jobs["preflight"]
	if !ok {
		t.Fatal("cloud-e2e: no job preflight")
	}
	runs := false
	for _, s := range pf.Steps {
		runs = runs || strings.Contains(s.Run, "e2e-preflight.sh")
		if strings.Contains(s.Run, "--yes") {
			t.Errorf("preflight, %s: confirms a change", s.Name)
		}
	}
	if !runs {
		t.Error("preflight does not run e2e-preflight.sh")
	}
	if !strings.Contains(pf.Environment, "'cloud-readonly'") {
		t.Errorf("preflight: environment %q, want the read-only key on main", pf.Environment)
	}
	for name, j := range jobs {
		if j.Environment != "cloud-e2e" || name == "cleanup" {
			continue
		}
		var needs []string
		switch n := j.Needs.(type) {
		case string:
			needs = []string{n}
		case []any:
			for _, x := range n {
				needs = append(needs, fmt.Sprint(x))
			}
		}
		if !slices.Contains(needs, "preflight") {
			t.Errorf("cloud-e2e job %s writes to the cloud without needs: preflight (needs %v)", name, needs)
		}
	}
	// The script reads: every tool it calls is a list or the quotas.
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "e2e-preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}
	calls := preflightCall.FindAllStringSubmatch(string(b), -1)
	if len(calls) < 5 {
		t.Errorf("e2e-preflight.sh: %d calls of tools found — the check sees nothing, update preflightCall", len(calls))
	}
	for _, m := range calls {
		tool := m[1]
		if !strings.HasPrefix(tool, "list_") && !strings.HasSuffix(tool, "_list") && tool != "org_quotas" {
			t.Errorf("e2e-preflight.sh calls %s: only lists and org_quotas", tool)
		}
	}
	if strings.Contains(string(b), "--yes") {
		t.Error("e2e-preflight.sh confirms a change (--yes)")
	}
	ci := false
	for _, j := range ws["ci.yml"].Jobs {
		for _, s := range j.Steps {
			ci = ci || strings.Contains(s.Run, "e2e_preflight_test.sh")
		}
	}
	if !ci {
		t.Error("ci: e2e-preflight.sh is not tested (e2e_preflight_test.sh)")
	}
}

// The probe of G140-6 creates what the API accepts and is a record, not a
// check: it never fails the job, its cluster is deleted after it by
// dbaas-delete.sh whatever came before, and it writes nothing but POST
// /v1/clusters and the exchange of the key for a token. The log is public:
// the IDs it reads are masked, never printed, the token too, and curl's
// own errors, which may carry the URL, do not reach it.
func TestWorkflows_RedisStandaloneProbe(t *testing.T) {
	j, ok := readWorkflows(t)["cloud-e2e.yml"].Jobs["redis-standalone-probe"]
	if !ok {
		t.Fatal("cloud-e2e: no job redis-standalone-probe")
	}
	if !strings.Contains(j.If, "inputs.redis_standalone_probe") || !strings.Contains(j.If, "!inputs.cleanup_only") {
		t.Errorf("redis-standalone-probe runs on %q, not on its input alone", j.If)
	}
	if !strings.HasPrefix(j.Env["CLUSTER"], "evo-e2e-") {
		t.Errorf("redis-standalone-probe: cluster %q is not evo-e2e-*", j.Env["CLUSTER"])
	}
	probe, del := -1, -1
	for i, s := range j.Steps {
		if strings.Contains(s.Run, "redis-standalone-probe.sh") {
			probe = i
		}
		if strings.Contains(s.Run, "dbaas-delete.sh redis") {
			del = i
		}
	}
	if probe < 0 || del < probe {
		t.Fatalf("redis-standalone-probe: the deletion (step %d) must come after the probe (step %d)", del, probe)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "redis-standalone-probe.sh"))
	if err != nil {
		t.Fatal(err)
	}
	misuse := 0
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\\\n", " "), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if strings.Contains(l, "curl ") && strings.Contains(l, "-X") &&
			!strings.Contains(l, "/auth/token") && !strings.Contains(l, `-X POST "$api/v1/clusters"`) {
			t.Errorf("redis-standalone-probe.sh writes to the API beyond POST /v1/clusters: %s", strings.TrimSpace(l))
		}
		// The only failing exit is a misuse: a name outside evo-e2e-*.
		if strings.Contains(l, "exit 2") {
			misuse++
		}
		if strings.Contains(l, "exit 1") || strings.Contains(l, "::error::") {
			t.Errorf("redis-standalone-probe.sh fails the step: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "evo ") && strings.Contains(l, "--yes") {
			t.Errorf("redis-standalone-probe.sh confirms a change through evo: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "evo ") && strings.Contains(l, "redis_create") && !strings.Contains(l, "--dry-run") {
			t.Errorf("redis-standalone-probe.sh calls redis_create without --dry-run: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "echo ") && !strings.Contains(l, "curl ") && !strings.Contains(l, "::add-mask::") && !strings.Contains(l, `>>"$GITHUB_ENV"`) &&
			probeSecret.MatchString(l) {
			t.Errorf("redis-standalone-probe.sh prints an ID or the token: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "curl ") && !strings.Contains(l, "2>/dev/null") {
			t.Errorf("redis-standalone-probe.sh lets curl's errors into the log: %s", strings.TrimSpace(l))
		}
	}
	if misuse != 1 {
		t.Errorf("redis-standalone-probe.sh exits 2 on %d lines: only the guard of the name, evo-e2e-*, fails the step", misuse)
	}
}
