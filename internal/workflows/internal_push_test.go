// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// internalPushSecret is the token that writes to the private evo-internal
// (the owner's decision of 04.11 for ai-factory-probe: fine-grained, that
// repository only, Contents: write).
const internalPushSecret = "secrets.EVO_INTERNAL_PUSH_TOKEN"

// The token that writes to evo-internal is a door from a public
// repository's CI to the private one: a job that holds it runs only on
// workflow_dispatch from main, in the environment ai-factory-probe (the
// owner approves each run), never checks out another ref or repository,
// and is the only kind of job that names it. The report it pushes goes
// through aif_secret_scan.sh against every secret first, and aif_push.sh
// pushes to probe/ai-factory-<run> only (both checked by their script
// tests). The owner's conditions of 04.11, 4 of 4.
func TestWorkflows_InternalPushTokenOnlyOnDispatchFromMain(t *testing.T) {
	users := 0
	for name, w := range readWorkflows(t) {
		raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), internalPushSecret) {
			continue
		}
		for ev := range w.On {
			if ev != "workflow_dispatch" {
				t.Errorf("%s: names %s and runs on %s: only workflow_dispatch", name, internalPushSecret, ev)
			}
		}
		if wenv, _ := yamlEnvText(w.Env); strings.Contains(wenv, internalPushSecret) {
			t.Errorf("%s: %s in the workflow's env: every job would hold it", name, internalPushSecret)
		}
		for job, j := range w.Jobs {
			holds := false
			for _, v := range j.Env {
				holds = holds || strings.Contains(v, internalPushSecret)
			}
			for _, s := range j.Steps {
				for _, v := range s.Env {
					holds = holds || strings.Contains(v, internalPushSecret)
				}
				for _, v := range s.With {
					if sv, ok := v.(string); ok && strings.Contains(sv, internalPushSecret) {
						holds = true
					}
				}
				if strings.Contains(s.Run, internalPushSecret) {
					t.Errorf("%s, job %s, step %q: %s in the run script — through env only", name, job, s.Name, internalPushSecret)
				}
			}
			if !holds {
				continue
			}
			users++
			if j.Environment != "ai-factory-probe" {
				t.Errorf("%s, job %s: holds %s in environment %q, want ai-factory-probe (the owner approves each run)", name, job, internalPushSecret, j.Environment)
			}
			if !strings.Contains(j.If, "github.ref == 'refs/heads/main'") {
				t.Errorf("%s, job %s: holds %s without if: github.ref == 'refs/heads/main'", name, job, internalPushSecret)
			}
			for _, s := range j.Steps {
				if !strings.HasPrefix(s.Uses, "actions/checkout@") {
					continue
				}
				for _, k := range []string{"ref", "repository"} {
					if _, ok := s.With[k]; ok {
						t.Errorf("%s, job %s: checkout with %s: the job would run code other than main's with %s", name, job, k, internalPushSecret)
					}
				}
				if v, _ := s.With["persist-credentials"].(bool); v || s.With["persist-credentials"] == nil {
					t.Errorf("%s, job %s: checkout without persist-credentials: false", name, job)
				}
			}
		}
	}
	if users == 0 {
		t.Error("no job holds " + internalPushSecret + ": the pattern moved, update the test")
	}
}

func yamlEnvText(env map[string]string) (string, bool) {
	var b strings.Builder
	for k, v := range env {
		b.WriteString(k + "=" + v + "\n")
	}
	return b.String(), len(env) > 0
}
