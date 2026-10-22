// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package workflows checks the GitHub Actions workflows of the repository:
// rules that keep a pull request's code away from the secrets.
package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflow struct {
	On          map[string]any `yaml:"on"`
	Permissions any            `yaml:"permissions"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress string `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		If    string `yaml:"if"`
		Steps []struct {
			Uses string         `yaml:"uses"`
			Run  string         `yaml:"run"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflows(t *testing.T) map[string]workflow {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	out := map[string]workflow{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var w workflow
		if err := yaml.Unmarshal(b, &w); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = w
	}
	return out
}

// A workflow that runs with the secrets on someone's pull request —
// pull_request_target, issue_comment — runs the code of the default
// branch: GitHub checks that out unless a step asks for another ref. A
// checkout of the PR's head there would hand its code the secrets (the
// key of Foundation Models in qwen-review). The PR is read as data (git
// diff), never checked out. A pull_request_target job runs only for a
// branch of this repository, not a fork's, and the workflow grants no
// permission beyond what each job declares.
func TestWorkflows_PRCodeNeverRunsWithSecrets(t *testing.T) {
	for name, w := range readWorkflows(t) {
		_, prt := w.On["pull_request_target"]
		_, comment := w.On["issue_comment"]
		if !prt && !comment {
			continue
		}
		if p, ok := w.Permissions.(map[string]any); !ok || len(p) != 0 {
			t.Errorf("%s: workflow permissions %v, want {} (each job declares its own)", name, w.Permissions)
		}
		for job, j := range w.Jobs {
			for _, s := range j.Steps {
				if !strings.HasPrefix(s.Uses, "actions/checkout@") {
					continue
				}
				for _, k := range []string{"ref", "repository"} {
					if _, ok := s.With[k]; ok {
						t.Errorf("%s, job %s: checkout with %s: the PR's code would run with the secrets", name, job, k)
					}
				}
			}
			if prt && !strings.Contains(j.If, "github.event.pull_request.head.repo.full_name == github.repository") {
				t.Errorf("%s, job %s: on pull_request_target, runs for a fork's PR too", name, job)
			}
		}
	}
}

// The text of an event — a PR's title or body, a comment, an issue, the
// name of a branch — is someone else's. Put into a run script as
// ${{ … }}, it becomes shell code with the job's token and secrets; it is
// passed through env instead and read as data (the script injection of
// GitHub's hardening guide). Any workflow, any trigger.
func TestWorkflows_EventTextNeverInRunScripts(t *testing.T) {
	for name, w := range readWorkflows(t) {
		for job, j := range w.Jobs {
			for _, s := range j.Steps {
				for _, bad := range []string{"github.event.", "github.head_ref"} {
					for _, expr := range strings.Split(s.Run, "${{")[1:] {
						if strings.Contains(strings.SplitN(expr, "}}", 2)[0], bad) {
							t.Errorf("%s, job %s: ${{ %s… }} in a run script: pass it through env", name, job, bad)
						}
					}
				}
			}
		}
	}
}

// The review of Qwen is part of every PR's cycle (CLAUDE.md §4): it starts
// when the PR is opened or leaves draft, not by hand (process audit 1.2.1,
// PR 2); the command /qwen review repeats it.
func TestWorkflows_QwenReviewsEveryPR(t *testing.T) {
	w, ok := readWorkflows(t)["qwen-review.yml"]
	if !ok {
		t.Fatal("no qwen-review.yml")
	}
	trig, _ := w.On["pull_request_target"].(map[string]any)
	types, _ := trig["types"].([]any)
	for _, want := range []string{"opened", "ready_for_review"} {
		if !slices.Contains(types, any(want)) {
			t.Errorf("qwen-review: pull_request_target types %v, want %s", types, want)
		}
	}
	for _, j := range w.Jobs {
		if !strings.Contains(j.If, "!github.event.pull_request.draft") {
			t.Error("qwen-review: a draft PR is reviewed on opening; it waits for ready_for_review")
		}
	}
	if _, ok := w.On["issue_comment"]; !ok {
		t.Error("qwen-review: /qwen review no longer repeats the review")
	}
}

// ci runs once per change: on a push to main and on a pull request, not
// on every push to every branch as well (a PR's push ran it twice and
// doubled the queue of the macOS runners), and a newer push to a PR
// cancels its running check; main and the release (workflow_call from
// release.yml) are never cancelled (process audit 1.2.1, PR 3).
func TestWorkflows_CIOncePerChange(t *testing.T) {
	w, ok := readWorkflows(t)["ci.yml"]
	if !ok {
		t.Fatal("no ci.yml")
	}
	push, _ := w.On["push"].(map[string]any)
	if b, _ := push["branches"].([]any); len(b) != 1 || b[0] != "main" {
		t.Errorf("ci: push branches %v, want [main]", push["branches"])
	}
	for _, ev := range []string{"pull_request", "workflow_call"} {
		if _, ok := w.On[ev]; !ok {
			t.Errorf("ci: no %s", ev)
		}
	}
	if !strings.Contains(w.Concurrency.Group, "github.ref") ||
		strings.ReplaceAll(w.Concurrency.CancelInProgress, " ", "") != "${{github.event_name=='pull_request'}}" {
		t.Errorf("ci: concurrency %+v, want a group per ref that cancels on pull_request only", w.Concurrency)
	}
}

// main runs the tests in a random order: a test that depends on another's
// state fails there, with the seed in the log, instead of in an unrelated
// PR (process audit 1.2.1, PR 4).
func TestWorkflows_CIShufflesOnMain(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	for _, j := range w.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, "go test -race") {
				if strings.Contains(s.Run, "-shuffle=") {
					return
				}
			}
		}
	}
	t.Error("ci: go test -race without -shuffle")
}
