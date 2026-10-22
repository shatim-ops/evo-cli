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
	On   map[string]any `yaml:"on"`
	Jobs map[string]struct {
		If    string `yaml:"if"`
		Steps []struct {
			Uses string         `yaml:"uses"`
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
// branch of this repository, not a fork's.
func TestWorkflows_PRCodeNeverRunsWithSecrets(t *testing.T) {
	for name, w := range readWorkflows(t) {
		_, prt := w.On["pull_request_target"]
		_, comment := w.On["issue_comment"]
		if !prt && !comment {
			continue
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
	if _, ok := w.On["issue_comment"]; !ok {
		t.Error("qwen-review: /qwen review no longer repeats the review")
	}
}
