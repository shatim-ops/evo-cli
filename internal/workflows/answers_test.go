// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The file of EVO_RECORD_ANSWERS holds the host and the segments of a path
// that are not ids (a bucket, a key of S3, a DNS zone), so it never leaves
// the job of cloud-e2e («Риск», conclusion 206, condition У2; plan 1.9,
// #1716): every job that runs evo ends with .github/actions/answers, which
// makes the catalogue lines of answers_catalogue.py, removes the raw file
// and uploads only those lines, for 7 days at most; no step of any
// workflow uploads the raw file. What the generator lets out is
// answers_catalogue_test.sh's, which ci runs.
func TestWorkflows_RawAnswersNeverLeaveTheJob(t *testing.T) {
	ws := readWorkflows(t)
	e2e := ws["cloud-e2e.yml"]
	raw := e2e.Env["EVO_RECORD_ANSWERS"]
	if raw == "" {
		t.Fatal("cloud-e2e: EVO_RECORD_ANSWERS is not set — no answers are recorded")
	}
	for name, j := range e2e.Jobs {
		evo := false
		for _, s := range j.Steps {
			evo = evo || strings.Contains(s.Run, "evo ") || strings.Contains(s.Run, "cloud-live.sh")
		}
		if !evo {
			continue
		}
		// The last step: a step after it that reached the cloud would
		// leave its records unread (review of #1811).
		last := j.Steps[len(j.Steps)-1]
		if last.Uses != "./.github/actions/answers" || !strings.Contains(last.If, "always()") || last.With["job"] != name {
			t.Errorf("cloud-e2e/%s: runs evo but does not end with ./.github/actions/answers (if: always(), job: %s) — its raw answers stay unread", name, name)
		}
	}
	base := filepath.Base(raw)
	for file, w := range ws {
		for jn, j := range w.Jobs {
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
					if p, _ := s.With["path"].(string); strings.Contains(p, base) || strings.Contains(p, "EVO_RECORD_ANSWERS") {
						t.Errorf("%s/%s: uploads the raw file of answers (%s)", file, jn, p)
					}
				}
			}
		}
	}

	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "answers", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Runs struct {
			Steps []struct {
				Uses string         `yaml:"uses"`
				Run  string         `yaml:"run"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	made, removed, uploads := false, false, 0
	for _, s := range a.Runs.Steps {
		if strings.Contains(s.Run, ".github/scripts/answers_catalogue.py") {
			made = true
		}
		if strings.Contains(s.Run, `rm -f "$EVO_RECORD_ANSWERS"`) {
			removed = true
		}
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploads++
			if p, _ := s.With["path"].(string); !strings.Contains(p, "answers-") || !strings.HasSuffix(p, ".json") {
				t.Errorf("answers: uploads %q, want the generator's answers-<job>.json alone", p)
			}
			if d, _ := s.With["retention-days"].(int); d < 1 || d > 7 {
				t.Errorf("answers: retention-days %v, want 1–7", s.With["retention-days"])
			}
		}
	}
	if !made || !removed || uploads != 1 {
		t.Errorf("answers: generator run %v, raw file removed %v, uploads %d — want the lines made, the raw file removed and one upload", made, removed, uploads)
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/answers_catalogue_test.sh") {
		t.Error("ci: answers_catalogue.py is not tested (answers_catalogue_test.sh)")
	}
}
