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
		If          string `yaml:"if"`
		Permissions any    `yaml:"permissions"`
		Uses        string `yaml:"uses"`
		Steps       []struct {
			ID   string            `yaml:"id"`
			If   string            `yaml:"if"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			With map[string]any    `yaml:"with"`
			Env  map[string]string `yaml:"env"`
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
				// Expanded by Actions: a shell variable here is PowerShell's
				// on Windows, not the environment's (-shuffle="" broke CI).
				if strings.Contains(s.Run, "-shuffle=${{") {
					return
				}
			}
		}
	}
	t.Error("ci: go test -race without -shuffle")
}

// Every PR gets its change class and the checklist of checks beyond CI in
// the run's summary, and the classifier's tables are tested on sample
// paths (process audit 1.2.1, PR 8); the classes of the registry diff come
// from tools/list of the base and the head (stage 6.5). The job holds the PR's code, so it
// writes nothing to the repository: no labels, no permissions of its own.
func TestWorkflows_CIClassifiesPRs(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	var classify, tested bool
	registry := ""
	for name, j := range w.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, ".github/scripts/classify_registry.sh") && strings.Contains(s.Run, ".github/scripts/tools_list.sh") {
				registry = "${{ steps." + s.ID + ".outputs.classes }}"
			}
			if strings.Contains(s.Run, ".github/scripts/classify_test.sh") {
				tested = true
			}
			if strings.Contains(s.Run, ".github/scripts/classify.sh") &&
				strings.Contains(s.Run, ".github/scripts/classify_summary.sh") &&
				strings.Contains(s.Run, "$GITHUB_STEP_SUMMARY") {
				classify = true
				if registry == "" || s.Env["REGISTRY"] != registry {
					t.Errorf("ci: job %s classifies without the registry classes (REGISTRY %q)", name, s.Env["REGISTRY"])
				}
				if !strings.Contains(j.If, "pull_request") {
					t.Errorf("ci: job %s classifies outside pull_request (if: %q)", name, j.If)
				}
				if j.Permissions != nil {
					t.Errorf("ci: job %s declares permissions %v; it runs the PR's code", name, j.Permissions)
				}
			}
		}
	}
	if !classify {
		t.Error("ci: no job writes the PR's class and checklist to the summary")
	}
	if !tested {
		t.Error("ci: classify_test.sh is not run")
	}
}

// ci lints the workflows (actionlint, with shellcheck of each run script)
// and fails on code unreachable from main (deadcode): checks of the cycle
// of CLAUDE.md §4 that lived only in sessions (process audit 1.2.1, PR 7).
func TestWorkflows_CILintsWorkflowsAndDeadCode(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	runs := ""
	for _, j := range w.Jobs {
		for _, s := range j.Steps {
			runs += s.Run + "\n"
		}
	}
	for _, tool := range []string{"rhysd/actionlint/cmd/actionlint@", "golang.org/x/tools/cmd/deadcode@"} {
		if !strings.Contains(runs, tool) {
			t.Errorf("ci: no step installs %s", tool)
		}
	}
}

// cloud-e2e runs from main every week (process audit 1.2.1, PR 9; owner's
// decision В4): on Monday the cheap set — the main e2e run with dns, s3
// and secrets — and on Saturday cleanup_only. The weekly workflow only
// dispatches cloud-e2e.yml with explicit inputs, so the run logic and the
// owner's approval in the cloud-e2e environment stay in one place; it holds
// no key and may do nothing but start a workflow.
func TestWorkflows_CloudE2EWeekly(t *testing.T) {
	w, ok := readWorkflows(t)["cloud-e2e-weekly.yml"]
	if !ok {
		t.Fatal("no cloud-e2e-weekly.yml")
	}
	if s, _ := w.On["schedule"].([]any); len(s) != 2 {
		t.Errorf("cloud-e2e-weekly: schedule %v, want the Monday set and the Saturday cleanup", w.On["schedule"])
	}
	runs := ""
	for name, j := range w.Jobs {
		if !strings.Contains(j.If, "refs/heads/main") {
			t.Errorf("cloud-e2e-weekly: job %s runs off main (if: %q)", name, j.If)
		}
		if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["actions"] != "write" {
			t.Errorf("cloud-e2e-weekly: job %s permissions %v, want actions: write only", name, j.Permissions)
		}
		for _, s := range j.Steps {
			runs += s.Run + "\n"
		}
	}
	for _, want := range []string{
		"gh workflow run cloud-e2e.yml --repo \"$GITHUB_REPOSITORY\" --ref main",
		"-f cleanup_only=true",
		"-f dns_smoke=true -f s3_smoke=true -f secrets_smoke=true",
	} {
		if !strings.Contains(runs, want) {
			t.Errorf("cloud-e2e-weekly: no %q", want)
		}
	}
}

// cloud-e2e started from a PR's branch with no area chosen checks the cheap
// areas the branch touches (classify.sh, owner's decision В5) instead of
// none: the owner's one click covers what the PR's checklist asks for.
func TestWorkflows_CloudE2EAreasFromBranch(t *testing.T) {
	w := readWorkflows(t)["cloud-e2e.yml"]
	for _, s := range w.Jobs["live-plan"].Steps {
		if strings.Contains(s.Run, "git diff --name-only origin/main...HEAD | .github/scripts/classify.sh") {
			return
		}
	}
	t.Error("cloud-e2e: live-plan does not take areas from the branch's paths")
}

// Every week main's tests run three times in a random order under -race on
// the three OSes, and a red run opens an issue (process audit 1.2.1, PR 10):
// a race or an order dependency that shows once in a few runs is caught
// there.
func TestWorkflows_WeeklyRace(t *testing.T) {
	w := weeklyWorkflow(t, "weekly-race.yml")
	if !runsStep(w, "go test -race -count=3 -shuffle=on ./...") {
		t.Error("weekly-race: no go test -race -count=3 -shuffle=on")
	}
}

// Every week each Fuzz* target of the repository fuzzes main for five
// minutes (process audit 1.2.1, second wave): the target list comes from
// go test -list, so a new target joins without an edit here.
func TestWorkflows_WeeklyFuzz(t *testing.T) {
	w := weeklyWorkflow(t, "weekly-fuzz.yml")
	for _, want := range []string{"go test -list '^Fuzz' ./...", `go test -run '^$' -fuzz "^${NAME}\$" -fuzztime=5m`} {
		if !runsStep(w, want) {
			t.Errorf("weekly-fuzz: no step runs %s", want)
		}
	}
}

// A red weekly run reaches people as an issue, one per kind, written by one
// workflow that checks out no code and may do nothing but write issues.
func TestWorkflows_WeeklyIssue(t *testing.T) {
	w, ok := readWorkflows(t)["weekly-issue.yml"]
	if !ok {
		t.Fatal("no weekly-issue.yml")
	}
	if _, ok := w.On["workflow_call"]; !ok || len(w.On) != 1 {
		t.Errorf("weekly-issue: triggers %v, want workflow_call only", w.On)
	}
	for name, j := range w.Jobs {
		if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["issues"] != "write" {
			t.Errorf("weekly-issue: job %s permissions %v, want issues: write only", name, j.Permissions)
		}
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Uses, "actions/checkout@") {
				t.Errorf("weekly-issue: job %s checks out code with a write token", name)
			}
		}
	}
	if !runsStep(w, "gh issue create") {
		t.Error("weekly-issue: no gh issue create")
	}
}

// weeklyWorkflow is a scheduled workflow whose red run calls weekly-issue.yml.
func weeklyWorkflow(t *testing.T, file string) workflow {
	t.Helper()
	w, ok := readWorkflows(t)[file]
	if !ok {
		t.Fatalf("no %s", file)
	}
	if _, ok := w.On["schedule"]; !ok {
		t.Errorf("%s: no schedule", file)
	}
	reports := false
	for name, j := range w.Jobs {
		if j.Uses != "./.github/workflows/weekly-issue.yml" {
			continue
		}
		reports = true
		if j.If != "failure()" {
			t.Errorf("%s: job %s reports on %q, want failure()", file, name, j.If)
		}
		if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["issues"] != "write" {
			t.Errorf("%s: job %s permissions %v, want issues: write only", file, name, j.Permissions)
		}
	}
	if !reports {
		t.Errorf("%s: a red run opens no issue (weekly-issue.yml)", file)
	}
	return w
}

func runsStep(w workflow, cmd string) bool {
	for _, j := range w.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, cmd) {
				return true
			}
		}
	}
	return false
}

// The contract check caches the specs of cloud.ru/docs for a day, and only
// a complete download is cached: a PR whose download failed skipped the
// check and still saved the partial directory, so every later PR of the
// day failed on spec-missing (#230, 22.10). Restore and save are separate
// steps, the save conditioned on a download that succeeded.
func TestWorkflows_ContractCachesOnlyCompleteSpecs(t *testing.T) {
	w, ok := readWorkflows(t)["api-contract.yml"]
	if !ok {
		t.Fatal("no api-contract.yml")
	}
	var restore, save bool
	for name, j := range w.Jobs {
		for _, s := range j.Steps {
			switch {
			case strings.HasPrefix(s.Uses, "actions/cache@"):
				t.Errorf("api-contract job %s: actions/cache saves whatever the job left; use actions/cache/restore and actions/cache/save", name)
			case strings.HasPrefix(s.Uses, "actions/cache/restore@"):
				restore = true
			case strings.HasPrefix(s.Uses, "actions/cache/save@"):
				save = true
				if !strings.Contains(s.If, "steps.specs.outputs.complete == 'true'") {
					t.Errorf("api-contract job %s: the save runs on %q, not only after a complete download (steps.specs.outputs.complete)", name, s.If)
				}
			}
		}
	}
	if !restore || !save {
		t.Errorf("api-contract: restore %v, save %v; want both", restore, save)
	}
}
