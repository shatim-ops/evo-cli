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

// The probe of a repeated creation of a PostgreSQL user and database
// (pg-repeat-probe.sh, plan 1.9) writes to a live cluster from a workflow
// with a secret. It runs between the step that created the two and the step
// that deletes them, only by its word of the input probes, and never fails
// the job; it sends two POSTs — the user and the database that already
// exist — and no other write; the cluster, the token and the password are
// masked and never printed, and curl's own errors do not reach the log.
func TestWorkflows_PgRepeatProbeIsBounded(t *testing.T) {
	steps := readWorkflows(t)["cloud-e2e.yml"].Jobs["dbaas-pg"].Steps
	made, probe, gone := -1, -1, -1
	for i, s := range steps {
		switch {
		case strings.Contains(s.Run, "pg_user_create") && strings.Contains(s.Run, "pg_db_create"):
			made = i
		case strings.Contains(s.Run, "pg-repeat-probe.sh"):
			probe = i
		case strings.Contains(s.Run, "pg_db_delete") && strings.Contains(s.Run, "pg_user_delete"):
			gone = i
		}
	}
	if made < 0 || probe < 0 || gone < 0 || !(made < probe && probe < gone) {
		t.Fatalf("dbaas-pg: the probe (step %d) must run after the user and the database are created (step %d) and before they are deleted (step %d)", probe, made, gone)
	}
	p := steps[probe]
	if !strings.Contains(p.If, "' pgrepeat '") || !strings.Contains(p.If, "inputs.probes") {
		t.Errorf("dbaas-pg: the probe runs without its word of the input probes: if %q", p.If)
	}
	if p.TimeoutMinutes == 0 || p.TimeoutMinutes > 15 {
		t.Errorf("dbaas-pg: the probe needs timeout-minutes of its own, at most 15, has %d — a hung step would keep the paid cluster", p.TimeoutMinutes)
	}
	if !p.ContinueOnError {
		t.Error("dbaas-pg: the probe needs continue-on-error: true — its answer is the platform's, not a failure of the job")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "pg-repeat-probe.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	for _, want := range []string{`echo "::add-mask::$CLUSTER_ID"`, `echo "::add-mask::$token"`, `echo "::add-mask::$pw"`} {
		if !strings.Contains(script, want) {
			t.Errorf("pg-repeat-probe.sh lacks %s", want)
		}
	}
	if !strings.Contains(script, "\napi=https://postgresql.api.cloud.ru\n") {
		t.Error("pg-repeat-probe.sh: the host is not the constant of the published spec")
	}
	writes := regexp.MustCompile(`\breq (POST|PUT|PATCH|DELETE) (\S+)`)
	var sent []string
	for _, l := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		for _, m := range writes.FindAllStringSubmatch(l, -1) {
			sent = append(sent, m[1]+" "+m[2])
		}
		if strings.Contains(l, "exit 1") || strings.Contains(l, "::error::") {
			t.Errorf("pg-repeat-probe.sh fails the step: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "evo ") && strings.Contains(l, "--yes") {
			t.Errorf("pg-repeat-probe.sh confirms a change through evo: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "echo ") && !strings.Contains(l, "::add-mask::") &&
			(strings.Contains(l, "$CLUSTER_ID") || strings.Contains(l, "$token") || strings.Contains(l, "$pw")) {
			t.Errorf("pg-repeat-probe.sh prints the cluster, the token or the password: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "curl ") && !strings.HasSuffix(strings.TrimSpace(l), "\\") && !strings.Contains(l, "2>/dev/null") {
			t.Errorf("pg-repeat-probe.sh lets curl's errors into the log: %s", strings.TrimSpace(l))
		}
		// A request that hangs would keep the paid cluster to the limit of
		// the job (risk note 185).
		if strings.Contains(l, "curl ") && (!strings.Contains(l, "--connect-timeout ") || !strings.Contains(l, "--max-time ")) {
			t.Errorf("pg-repeat-probe.sh: curl without --connect-timeout and --max-time: %s", strings.TrimSpace(l))
		}
		if regexp.MustCompile(`curl [^|]*(\s-[A-Za-z]*L[A-Za-z]*\s|--location)`).MatchString(l) {
			t.Errorf("pg-repeat-probe.sh follows redirects: %s", strings.TrimSpace(l))
		}
	}
	if got := strings.Join(sent, "; "); got != "POST users; POST databases" {
		t.Errorf("pg-repeat-probe.sh writes %q, want the two repeats only: POST users; POST databases", got)
	}
}
