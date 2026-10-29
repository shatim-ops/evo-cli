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

var (
	cleanupKinds = regexp.MustCompile(`for k in ([a-z0-9 ]+);`)
	// probeSecret is what names the project's resources, or opens them.
	probeSecret = regexp.MustCompile(`\$\{?(id|token|v|ZONE_ID|PROJECT|CLUSTER_ID)\b`)
)

// A cluster a job of cloud-e2e creates (its env CLUSTER) is deleted by its
// own job, whatever came before, and by cleanup_only, when that job was cut
// off (a timeout, a cancelled run): a cluster cleanup does not know of
// stays billed until someone sees it in the console.
func TestWorkflows_CleanupKnowsEveryCluster(t *testing.T) {
	jobs := readWorkflows(t)["cloud-e2e.yml"].Jobs
	var cleanup string
	for _, s := range jobs["cleanup"].Steps {
		cleanup += s.Run + "\n"
	}
	kinds := map[string]bool{}
	if m := cleanupKinds.FindStringSubmatch(cleanup); m != nil && strings.Contains(cleanup, `"evo-e2e-$k"`) {
		for _, k := range strings.Fields(m[1]) {
			kinds["evo-e2e-"+k] = true
		}
	}
	found := 0
	for name, j := range jobs {
		c := j.Env["CLUSTER"]
		if c == "" {
			continue
		}
		found++
		for _, n := range clusterNames(t, c) {
			if !kinds[n] && !strings.Contains(cleanup, `"`+n+`"`) && !strings.Contains(cleanup, " "+n+" ") {
				t.Errorf("cloud-e2e job %s creates cluster %s, which cleanup_only does not delete", name, n)
			}
		}
		own := false
		for _, s := range j.Steps {
			own = own || (strings.Contains(s.Run, "dbaas-delete.sh") || strings.Contains(s.Run, "k8s delete")) && strings.Contains(s.If, "always()")
		}
		if !own {
			t.Errorf("cloud-e2e job %s: no step deletes cluster %s with if: always()", name, c)
		}
	}
	if found == 0 {
		t.Fatal("no job of cloud-e2e has env CLUSTER: the jobs moved, update the test")
	}
}

// clusterInput is a cluster name a job takes from an input of the
// workflow: ${{ inputs.<name> || '<default>' }}.
var clusterInput = regexp.MustCompile(`^\$\{\{ inputs\.([a-z_]+) \|\| '([a-z0-9-]+)' \}\}$`)

// clusterNames is every name the env CLUSTER of a job can take: the name
// itself, or the default and every option of the choice input it reads.
func clusterNames(t *testing.T, c string) []string {
	t.Helper()
	m := clusterInput.FindStringSubmatch(c)
	if m == nil {
		if strings.Contains(c, "${{") {
			t.Errorf("cluster %q: an expression other than ${{ inputs.<choice> || '<default>' }}", c)
		}
		return []string{c}
	}
	on := readWorkflows(t)["cloud-e2e.yml"].On
	dispatch, _ := on["workflow_dispatch"].(map[string]any)
	inputs, _ := dispatch["inputs"].(map[string]any)
	in, _ := inputs[m[1]].(map[string]any)
	opts, _ := in["options"].([]any)
	if in["type"] != "choice" || len(opts) == 0 {
		t.Errorf("cluster %q: input %s is not a choice — cleanup_only cannot know the name", c, m[1])
		return []string{m[2]}
	}
	names := []string{m[2]}
	for _, o := range opts {
		names = append(names, o.(string))
	}
	return names
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
