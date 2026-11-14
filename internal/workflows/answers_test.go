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

	"gopkg.in/yaml.v3"
)

// The file of EVO_RECORD_ANSWERS holds the host and the segments of a path
// that are not ids (a bucket, a key of S3, a DNS zone), so it never leaves
// the job of cloud-e2e («Риск», conclusion 206, condition У2; plan 1.9,
// #1716): every job that runs evo ends with .github/actions/answers, which
// makes the catalogue lines of answers_catalogue.py, removes the raw file
// and uploads only those lines, for 7 days at most; no step of any
// workflow uploads the raw file. No step of a workflow names the raw file,
// a line of the action names it only to run the generator on it or remove
// it, the action calls no evo, and the artifact of a matrix leg is named by
// a coordinate of its matrix (#1861: five mutations passed a match of
// lines). What the generator lets out is answers_catalogue_test.sh's, which
// ci runs.
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
		// The name of the artifact is answers-<job>: a leg of a matrix adds
		// its own key, or the second leg's upload conflicts with the first
		// («Риск», conclusion 226, У2).
		want := name
		if len(j.Strategy.Matrix) > 0 {
			got, _ := last.With["job"].(string)
			key := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(got, name+"-${{ matrix.")), "}}")
			key = strings.TrimSpace(key)
			if !strings.HasPrefix(got, name+"-${{ matrix.") || !strings.HasSuffix(got, "}}") || !matrixKeys(j.Strategy.Matrix)[key] {
				t.Errorf("cloud-e2e/%s: a matrix job names its answers %q, want %s-${{ matrix.<key> }} with a key of its matrix %v — one artifact name for every leg (#1861)", name, got, name, keysOf(matrixKeys(j.Strategy.Matrix)))
			}
			want = got
		}
		if last.Uses != "./.github/actions/answers" || !strings.Contains(last.If, "always()") || last.With["job"] != want {
			t.Errorf("cloud-e2e/%s: runs evo but does not end with ./.github/actions/answers (if: always(), job: %s) — its raw answers stay unread", name, name)
		}
	}
	base := filepath.Base(raw)
	for file, w := range ws {
		for jn, j := range w.Jobs {
			for _, s := range j.Steps {
				// Only evo writes the raw file and only the action reads it:
				// a step that names it could print or copy it (#1861).
				if touchesRaw(s.Run+fmt.Sprint(s.With)+fmt.Sprint(s.Env), base) {
					t.Errorf("%s/%s: step %q names the raw file of answers — only .github/actions/answers reads it", file, jn, s.Name)
				}
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
		// Each line of the action that names the raw file is the generator
		// reading it into the catalogue or its removal — no cat, cp or
		// other reader (#1861: three mutations passed the line match).
		for _, l := range logicalLines(s.Run) {
			if !touchesRaw(l, base) {
				continue
			}
			switch {
			case strings.Contains(l, ".github/scripts/answers_catalogue.py") && strings.Count(l, "EVO_RECORD_ANSWERS") == 1 &&
				strings.Contains(l, `"$EVO_RECORD_ANSWERS" >"$out"`):
				made = true
			case l == `rm -f "$EVO_RECORD_ANSWERS"`:
				removed = true
			default:
				t.Errorf("answers: line %q names the raw file — only the generator reads it and rm removes it", l)
			}
		}
		// No call of evo inside the action: a call after the removal
		// writes records that nobody reads (#1861).
		for _, l := range logicalLines(s.Run) {
			if evoCall.MatchString(l) {
				t.Errorf("answers: line %q calls evo — the action ends the job's calls of evo, it makes none", l)
			}
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

// evoCall: a command line that runs evo (not a word of a comment or a path),
// also in a substitution: $(evo …) or `evo …` (Qwen review of #1878).
var evoCall = regexp.MustCompile(`(^|[;&|(` + "`" + `]\s*|\$\(\s*)(\./)?evo(\s|$|` + "`" + `)`)

// touchesRaw: the text names the raw file of answers, by its variable or
// by the name of the file.
func touchesRaw(text, base string) bool {
	return strings.Contains(text, "EVO_RECORD_ANSWERS") || strings.Contains(text, base)
}

// logicalLines: the lines of a run script with the continuations joined,
// trimmed, without comments and empty lines.
func logicalLines(run string) []string {
	var out []string
	cur := ""
	for _, l := range strings.Split(run, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			continue
		}
		if c, ok := strings.CutSuffix(l, "\\"); ok {
			cur += strings.TrimSpace(c) + " "
			continue
		}
		if l = strings.TrimSpace(cur + l); l != "" {
			out = append(out, l)
		}
		cur = ""
	}
	return out
}

// matrixKeys: the coordinates of a matrix — its keys and the keys of its
// include entries, not include and exclude themselves.
func matrixKeys(m map[string]any) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		if k == "exclude" {
			continue
		}
		if k != "include" {
			out[k] = true
			continue
		}
		list, _ := v.([]any)
		for _, e := range list {
			if em, ok := e.(map[string]any); ok {
				for ek := range em {
					out[ek] = true
				}
			}
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
