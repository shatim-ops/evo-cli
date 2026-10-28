// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package workflows checks the GitHub Actions workflows of the repository:
// rules that keep a pull request's code away from the secrets.
package workflows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
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
		If             string `yaml:"if"`
		TimeoutMinutes int    `yaml:"timeout-minutes"`
		Permissions    any    `yaml:"permissions"`
		Uses           string `yaml:"uses"`
		RunsOn         string `yaml:"runs-on"`
		Strategy       struct {
			Matrix      map[string]any `yaml:"matrix"`
			MaxParallel int            `yaml:"max-parallel"`
		} `yaml:"strategy"`
		Env map[string]string `yaml:"env"`
		// ContinueOnError: a job whose failure does not fail the run.
		ContinueOnError bool `yaml:"continue-on-error"`
		Steps           []struct {
			ID   string            `yaml:"id"`
			Name string            `yaml:"name"`
			If   string            `yaml:"if"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			With map[string]any    `yaml:"with"`
			Env  map[string]string `yaml:"env"`
			// ContinueOnError: a step whose failure does not fail the job.
			ContinueOnError bool `yaml:"continue-on-error"`
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

// The review of Qwen is part of a PR's cycle (CLAUDE.md §4): it starts
// when the PR is opened or leaves draft, not by hand (process audit 1.2.1,
// PR 2); the command /qwen review repeats it. Which PRs it reviews by
// itself is the script's choice (TestWorkflows_QwenReviewScope, #485).
func TestWorkflows_QwenReviewsOnOpening(t *testing.T) {
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

// The race-enabled go test runs with the runtime's default preemption on
// every system. GODEBUG=asyncpreemptoff=1 on Windows (#245, against a rare
// 0xc0000005 in adjustdefers, hypothesis golang/go#79249) stretched the tail
// of the waits ten- to twentyfold there (#262: state.Update p90 142 ms
// against 10 ms) and made TestFault_Matrix wait seconds (#255); the owner
// dropped it on 23.10. A crash of the runtime is a case to look into, not a
// setting of CI.
func TestWorkflows_RaceKeepsAsyncPreemption(t *testing.T) {
	ws := readWorkflows(t)
	for _, file := range []string{"ci.yml", "weekly-race.yml"} {
		found := false
		for name, j := range ws[file].Jobs {
			for _, s := range j.Steps {
				if !strings.Contains(s.Run, "go test -race") {
					continue
				}
				found = true
				if strings.Contains(s.Env["GODEBUG"], "asyncpreemptoff") {
					t.Errorf("%s: job %s runs go test -race with GODEBUG %q: async preemption off slows the tests on Windows tenfold (#262)", file, name, s.Env["GODEBUG"])
				}
			}
		}
		if !found {
			t.Errorf("%s: no go test -race step", file)
		}
	}
}

// Each PR writes its CHANGELOG entry as a fragment in changelog.d (one file,
// no shared lines, so PRs of a version do not conflict); CI checks the
// fragments and the script that folds them before a tag, and a fragment
// with «### Миграция» marks the PR breaking, as the section in CHANGELOG.md
// does.
func TestWorkflows_CIChecksChangelogFragments(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	for _, want := range []string{
		"python3 scripts/changelog-test.py",
		"python3 scripts/changelog.py check",
		`git diff "$BASE...HEAD" -- CHANGELOG.md changelog.d/ | grep -q '^+### Миграция'`,
	} {
		if !runsStep(w, want) {
			t.Errorf("ci: no step runs %s", want)
		}
	}
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

// The readonly job of cloud-e2e checks that every tariff_url of the previews
// answers (TestLive_TariffPagesAnswer, network only there): a page Cloud.ru
// moved would send the person to a 404 (review Qwen #170).
func TestWorkflows_ReadonlyChecksTariffPages(t *testing.T) {
	j, ok := readWorkflows(t)["cloud-e2e.yml"].Jobs["readonly"]
	if !ok {
		t.Fatal("cloud-e2e: no job readonly")
	}
	for _, s := range j.Steps {
		if strings.Contains(s.Run, "-run '^TestLive_TariffPagesAnswer$'") && s.Env["EVO_LIVE_TARIFFS"] == "1" {
			return
		}
	}
	t.Error("cloud-e2e readonly: no step runs TestLive_TariffPagesAnswer with EVO_LIVE_TARIFFS=1")
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

// cloud-e2e disk marks image_create «не покрыто», not FAIL, only when the
// organisation's quota of custom images refuses it (CMP-16, a limit of the
// platform and the test organisation); which refusal that is is tested in
// ci (e2e_quota_test.sh).
func TestWorkflows_CIChecksImageQuotaSkip(t *testing.T) {
	if !runsStep(readWorkflows(t)["ci.yml"], ".github/scripts/e2e_quota_test.sh") {
		t.Error("ci: the image quota skip of cloud-e2e disk is not tested (e2e_quota_test.sh)")
	}
}

var (
	smokeToken = regexp.MustCompile(`MCP_BEARER_TOKEN=([^\s"'$]+)`)
	// minBearerTokenLen of internal/cli/mcp.go: a shorter MCP_BEARER_TOKEN
	// on an address open to the network (the container's -p 8080:8080) is
	// CONFIG_INVALID, and the server does not start (D45). Read from the
	// source, so the test follows the server's bound.
	minBearerConst = regexp.MustCompile(`(?m)^const minBearerTokenLen = (\d+)$`)
)

// The HTTP smoke of the image starts the server with a literal token: in
// ci on every PR, in release only on a tag. A token shorter than the
// server takes would surface first on the tag (#384, D45), and a smoke
// whose script call names another token fails the same way. Every literal
// MCP_BEARER_TOKEN of the workflows is long enough, the step's
// mcp-smoke.sh call passes that same token, and release runs the script ci
// runs.
func TestWorkflows_SmokeBearerTokenLong(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "cli", "mcp.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := minBearerConst.FindSubmatch(src)
	if m == nil {
		t.Fatal("internal/cli/mcp.go: no const minBearerTokenLen — the bound moved, update the test")
	}
	smokeTokenLen, _ := strconv.Atoi(string(m[1]))
	ws := readWorkflows(t)
	found := 0
	for name, w := range ws {
		for jname, j := range w.Jobs {
			for _, s := range j.Steps {
				for _, m := range smokeToken.FindAllStringSubmatch(s.Run, -1) {
					found++
					tok := m[1]
					if len(tok) < smokeTokenLen {
						t.Errorf("%s/%s: MCP_BEARER_TOKEN of %d characters, the server takes %d or more off loopback (D45)", name, jname, len(tok), smokeTokenLen)
					}
					call := regexp.MustCompile(`mcp-smoke\.sh http://localhost:8080 ` + regexp.QuoteMeta(tok) + `(\s|$)`)
					if strings.Contains(s.Run, "mcp-smoke.sh") && !call.MatchString(s.Run) {
						t.Errorf("%s/%s: mcp-smoke.sh is not called with the token the container starts with", name, jname)
					}
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no literal MCP_BEARER_TOKEN in the workflows: the smoke moved, update the test")
	}
	if !runsStep(ws["release.yml"], ".github/scripts/mcp-smoke.sh") || !runsStep(ws["ci.yml"], ".github/scripts/mcp-smoke.sh") {
		t.Error("ci and release must run the same smoke script, .github/scripts/mcp-smoke.sh")
	}
	// H11 is checked on a server of the image with --read-only beside the
	// smoke's own (S2, 1.4.0), in ci and in release alike.
	for _, name := range []string{"ci.yml", "release.yml"} {
		if !runsStep(ws[name], "--read-only") || !runsStep(ws[name], "MCP_SMOKE_READONLY=http://localhost:8081 .github/scripts/mcp-smoke.sh") {
			t.Errorf("%s: the smoke runs without the --read-only server (MCP_SMOKE_READONLY)", name)
		}
	}
}

// The files of the profile hold the key: on Windows their privacy is an
// ACL, which the mode bits of Linux and macOS do not show. cloud-readonly-os
// reads it with icacls on Windows after configure and fails on a grant to
// Everyone, Users or Authenticated Users (S2, 1.4.0).
func TestWorkflows_WindowsProfileACLChecked(t *testing.T) {
	steps := readWorkflows(t)["cloud-readonly-os.yml"].Jobs["readonly"].Steps
	configure, acl := -1, -1
	for i, s := range steps {
		if strings.Contains(s.Run, "evo configure") {
			configure = i
		}
		if strings.Contains(s.Run, "icacls") {
			acl = i
			if !strings.Contains(s.If, "Windows") || !strings.Contains(s.Run, "credentials.yaml") || !strings.Contains(s.Run, "Everyone") {
				t.Errorf("cloud-readonly-os: the ACL step runs on Windows over credentials.yaml and fails on Everyone; has if %q", s.If)
			}
		}
	}
	if acl < 0 || acl < configure {
		t.Errorf("cloud-readonly-os: no icacls step after configure (configure %d, icacls %d)", configure, acl)
	}
}

// 1.4.0 ships without an rc (owner's decision 27.10): the session journal
// of the live runs is checked instead. In the cloud-e2e jobs that call evo
// against the cloud, the journal is on from the first call after configure
// to the end, and its last step runs diag-check.py --public on it even when
// a step before failed.
func TestWorkflows_CloudE2EJournalChecked(t *testing.T) {
	jobs := readWorkflows(t)["cloud-e2e.yml"].Jobs
	for _, name := range []string{"readonly", "live"} {
		steps := jobs[name].Steps
		on, configure, check := -1, -1, -1
		for i, s := range steps {
			switch {
			case strings.Contains(s.Run, "evo configure"):
				configure = i
			case strings.Contains(s.Run, "evo diag on"):
				on = i
			case strings.Contains(s.Run, "scripts/diag-check.py --public"):
				check = i
				if !strings.Contains(s.If, "!cancelled()") {
					t.Errorf("cloud-e2e/%s: the journal check needs if: !cancelled(), has %q", name, s.If)
				}
			}
		}
		if on != configure+1 || configure < 0 {
			t.Errorf("cloud-e2e/%s: the journal goes on right after configure (configure step %d, diag on step %d)", name, configure, on)
		}
		if check != len(steps)-1 {
			t.Errorf("cloud-e2e/%s: diag-check --public is the last step (step %d of %d)", name, check, len(steps))
		}
	}
}

// The review's comment is public: the text of a refusal of Foundation
// Models comes from fm_error.sh alone, which gives 401, 403 and 404 as one
// line without the answer's body (E7, 1.4.0; fm_error_test.sh); the review
// script reads no message of the API's answer itself.
func TestWorkflows_QwenRefusalWithoutBody(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "qwen-review.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `fm_error "$code"`) {
		t.Error("qwen-review.sh: a refusal of Foundation Models is not worded by fm_error")
	}
	if strings.Contains(s, ".error.message") {
		t.Error("qwen-review.sh reads the message of the API's answer itself: the comment is public, fm_error words it")
	}
}

// Every offline test of a CI script (.github/scripts/*_test.sh) runs in
// ci: a test that no job runs guards nothing (dbaas_delete_test.sh, 1.3.1:
// the repeated delete of a DBaaS cluster in ERASED, D44).
func TestWorkflows_ScriptTestsRun(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "scripts", "*_test.sh"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no script tests found: %v", err)
	}
	ws := readWorkflows(t)
	for _, f := range files {
		if name := ".github/scripts/" + filepath.Base(f); !runsStep(ws["ci.yml"], name) {
			t.Errorf("ci: %s is not run", name)
		}
	}
	// The offline tests of the repository's scripts (scripts/*-test.py:
	// changelog, specs, diag-check) run on a pull request — in ci.yml or in
	// a workflow of their own with paths, as api-contract — so that a change
	// of the script is checked before the merge, not only on a schedule.
	py, _ := filepath.Glob(filepath.Join("..", "..", "scripts", "*-test.py"))
	for _, f := range py {
		name, run := "scripts/"+filepath.Base(f), false
		for _, w := range ws {
			_, pr := w.On["pull_request"]
			run = run || (pr && runsStep(w, name))
		}
		if !run {
			t.Errorf("%s is run by no workflow of a pull request", name)
		}
	}
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
	// A wait the test gives in milliseconds while it expects success after
	// a number of polls decides by the timer: Windows ticks at 15.6 ms
	// (#277, found on #283 by chance). The worlds of engine and tools run
	// many times on Windows, without -race: the Go runtime under -race
	// crashes there on its own (process-handoff.md) and would hide them.
	if !runsStep(w, "go test -count=20 -shuffle=on ./internal/engine/ ./internal/tools/") {
		t.Error("weekly-race: the worlds of engine and tools do not run many times on Windows")
	}
}

// The Go runtime crashes on Windows now and then under -race (#226, #219,
// #267, #278, #293, #331: "found pointer to free object" in internal/tools).
// A run by hand with windows_gc_probe tells the runtime from the only unsafe
// of evo, the ACLs of internal/config/private_windows.go: config many times
// under -race, tools with and without it, every step under GODEBUG checks
// of the collector, none hiding the next. The weekly race does not run then.
// tools ×20 under GODEBUG outlives the default -timeout of 10m (run
// 40135541445): every step sets its own.
// The third run of the probe (owner's decision 25.10): which axis takes
// the runtime crash away on Windows — the Go release, -race, the GC checks
// of GODEBUG, the Green Tea collector, async preemption. Two axes are the
// matrix of jobs, three are the runs of .github/scripts/gc-probe-matrix.sh
// in each; no run stops the next, the table is the result.
func TestWorkflows_WindowsGCProbe(t *testing.T) {
	w := weeklyWorkflow(t, "weekly-race.yml")
	d, _ := w.On["workflow_dispatch"].(map[string]any)
	in, _ := d["inputs"].(map[string]any)
	if p, _ := in["windows_gc_probe"].(map[string]any); p["type"] != "boolean" {
		t.Fatal("weekly-race: no boolean input windows_gc_probe")
	}
	if !strings.Contains(w.Jobs["race"].If, "!inputs.windows_gc_probe") {
		t.Errorf("weekly-race: the race job runs with the probe: if %q", w.Jobs["race"].If)
	}
	j, ok := w.Jobs["gc-probe"]
	if !ok || j.RunsOn != "windows-latest" || !strings.Contains(j.If, "inputs.windows_gc_probe") {
		t.Fatalf("weekly-race: no gc-probe job on windows-latest behind the input (runs-on %q, if %q)", j.RunsOn, j.If)
	}
	if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["contents"] != "read" {
		t.Errorf("weekly-race: gc-probe permissions %v, want contents: read only", j.Permissions)
	}
	axes := map[string][]string{
		"go": {"1.26.10", "1.27.x", "tip"},
		"gc": {"greenteagc", "nogreenteagc"},
	}
	for axis, want := range axes {
		got, _ := j.Strategy.Matrix[axis].([]any)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("weekly-race: gc-probe matrix %s = %v, want %v", axis, got, want)
		}
	}
	if !strings.Contains(j.Env["GOEXPERIMENT"], "matrix.gc") {
		t.Errorf("weekly-race: gc-probe GOEXPERIMENT %q does not follow matrix.gc", j.Env["GOEXPERIMENT"])
	}
	found := false
	for _, s := range j.Steps {
		if strings.TrimSpace(s.Run) == "bash .github/scripts/gc-probe-matrix.sh ./internal/tools/" {
			found = true
		}
	}
	if !found {
		t.Error("weekly-race: gc-probe does not run .github/scripts/gc-probe-matrix.sh ./internal/tools/")
	}
}

// The cleanup of cloud-e2e removes an «evo:» rule of an e2e port only when
// its security group has no interface (e2e_rules_sweep.sh, tested in ci): any evo
// deploy writes «evo: …», and a rule of a person's VM looks the same. No
// step deletes rules on its own.
func TestWorkflows_CleanupKeepsRulesOfGroupsInUse(t *testing.T) {
	ws := readWorkflows(t)
	j, ok := ws["cloud-e2e.yml"].Jobs["cleanup"]
	if !ok {
		t.Fatal("cloud-e2e: no job cleanup")
	}
	sweeps := false
	for name, job := range ws["cloud-e2e.yml"].Jobs {
		for _, s := range job.Steps {
			if strings.Contains(s.Run, "/rules/") && strings.Contains(s.Run, "DELETE") {
				t.Errorf("cloud-e2e job %s: a step deletes security group rules itself, not with e2e_rules_sweep.sh", name)
			}
		}
	}
	for _, s := range j.Steps {
		sweeps = sweeps || strings.Contains(s.Run, ".github/scripts/e2e_rules_sweep.sh")
	}
	if !sweeps {
		t.Error("cloud-e2e cleanup: the «evo:» rules are not swept with e2e_rules_sweep.sh")
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/e2e_rules_sweep_test.sh") {
		t.Error("ci: e2e_rules_sweep.sh is not tested (e2e_rules_sweep_test.sh)")
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

// A release proves where it came from (process audit 1.2.1, 6.4; owner's
// decision В8): every archive has an SBOM listed in the signed
// checksums.txt, every file in checksums.txt has a provenance attestation
// of release.yml, and the MCP image pushed to the registry is signed by
// digest with cosign keyless. syft comes from go install at a fixed
// version, checked against the Go checksum database, not from one more
// action in the jobs that may mint the workflow's OIDC token.
func TestWorkflows_ReleaseSupplyChain(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var gr struct {
		SBOMs []struct {
			Artifacts string `yaml:"artifacts"`
		} `yaml:"sboms"`
	}
	if err := yaml.Unmarshal(b, &gr); err != nil {
		t.Fatal(err)
	}
	if len(gr.SBOMs) != 1 || gr.SBOMs[0].Artifacts != "archive" {
		t.Errorf(".goreleaser.yaml: sboms %+v, want one entry for archive", gr.SBOMs)
	}

	w, ok := readWorkflows(t)["release.yml"]
	if !ok {
		t.Fatal("no release.yml")
	}
	perms := func(job string) map[string]any {
		p, _ := w.Jobs[job].Permissions.(map[string]any)
		return p
	}
	steps := func(job, uses string) []map[string]any {
		var out []map[string]any
		for _, s := range w.Jobs[job].Steps {
			if strings.HasPrefix(s.Uses, uses+"@") {
				out = append(out, s.With)
			}
		}
		return out
	}
	runs := func(job, cmd string) bool {
		for _, s := range w.Jobs[job].Steps {
			if strings.Contains(s.Run, cmd) {
				return true
			}
		}
		return false
	}

	if p := perms("goreleaser"); p["attestations"] != "write" || p["id-token"] != "write" {
		t.Errorf("release.yml goreleaser: permissions %v, want attestations and id-token: write", p)
	}
	syft := regexp.MustCompile(`go install github\.com/anchore/syft/cmd/syft@v\d+\.\d+\.\d+\s`)
	pinned := false
	for _, s := range w.Jobs["goreleaser"].Steps {
		pinned = pinned || syft.MatchString(s.Run)
	}
	if !pinned {
		t.Error("release.yml goreleaser: syft is not installed with go install at a full version (vX.Y.Z, not a query)")
	}
	if a := steps("goreleaser", "actions/attest-build-provenance"); len(a) != 1 || a[0]["subject-checksums"] != "dist/checksums.txt" {
		t.Errorf("release.yml goreleaser: attest-build-provenance %v, want one step with subject-checksums: dist/checksums.txt", a)
	}

	if p := perms("image"); p["id-token"] != "write" || p["contents"] != "read" || len(p) != 2 {
		t.Errorf("release.yml image: permissions %v, want contents: read and id-token: write only", p)
	}
	if len(steps("image", "sigstore/cosign-installer")) != 1 {
		t.Error("release.yml image: cosign is not installed")
	}
	if !runs("image", `cosign sign --yes "$DIGEST_REF"`) {
		t.Error("release.yml image: the pushed image is not signed by digest")
	}
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

// The contract is checked after every merge that can change it, not only on
// the pull request, where a slow docs site skips it, and by the weekly run:
// main runs it with the paths of the pull request and the same soft rule (a
// download that fails skips the check with a warning and a line in the run's
// summary), and the cache it saves is the one every pull request can read
// (a pull request's cache is visible to that pull request only). Only the
// schedule and a manual run stay strict and always fetch fresh specs.
func TestWorkflows_ContractOnMainSoftAndWarmsTheCache(t *testing.T) {
	w, ok := readWorkflows(t)["api-contract.yml"]
	if !ok {
		t.Fatal("no api-contract.yml")
	}
	push, _ := w.On["push"].(map[string]any)
	pr, _ := w.On["pull_request"].(map[string]any)
	if b, _ := push["branches"].([]any); len(b) != 1 || b[0] != "main" {
		t.Errorf("api-contract: push branches %v, want [main]", push["branches"])
	}
	pushPaths, _ := push["paths"].([]any)
	prPaths, _ := pr["paths"].([]any)
	if len(prPaths) == 0 || !slices.Equal(pushPaths, prPaths) {
		t.Errorf("api-contract: push paths %v, pull_request paths %v; want the same", push["paths"], pr["paths"])
	}
	const soft = "github.event_name == 'pull_request' || github.event_name == 'push'"
	for name, j := range w.Jobs {
		for _, s := range j.Steps {
			cacheStep := strings.HasPrefix(s.Uses, "actions/cache/restore@") || strings.HasPrefix(s.Uses, "actions/cache/save@")
			if (cacheStep || s.ID == "day") && !strings.Contains(s.If, soft) {
				t.Errorf("api-contract job %s: step %q runs on %q, want pull_request and push (%s)", name, s.Name, s.If, soft)
			}
			if s.ID == "specs" {
				if s.Env["SOFT"] != "${{ "+soft+" }}" || !strings.Contains(s.Run, `if [ "$SOFT" = true ]`) {
					t.Errorf("api-contract job %s: the download is not soft on pull_request and push (env SOFT %q)", name, s.Env["SOFT"])
				}
				if !strings.Contains(s.Run, "GITHUB_STEP_SUMMARY") {
					t.Errorf("api-contract job %s: a skipped check leaves no line in the run's summary", name)
				}
			}
		}
	}
}

// The download of the specs ends before the job's own limit: on a pull
// request a docs site that does not answer in time skips the check with a
// warning instead of cancelling the job (#245, #217: three full passes over
// the docs ran out the 15 minutes). The script retries only what is missing
// and is tested without the network on every change to it.
func TestWorkflows_ContractDownloadEndsBeforeTheJob(t *testing.T) {
	w, ok := readWorkflows(t)["api-contract.yml"]
	if !ok {
		t.Fatal("no api-contract.yml")
	}
	limit := regexp.MustCompile(`\btimeout (\d+) python3 scripts/cloudru-specs\.py\b`)
	var download, selfTest bool
	for name, j := range w.Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, "python3 scripts/cloudru-specs-test.py") {
				selfTest = true
			}
			if s.ID != "specs" {
				continue
			}
			download = true
			m := limit.FindStringSubmatch(s.Run)
			if m == nil {
				t.Errorf("api-contract job %s: the download has no time limit of its own (timeout N python3 scripts/cloudru-specs.py)", name)
				continue
			}
			secs, _ := strconv.Atoi(m[1])
			// Two minutes stay for checkout, the coverage step and the check.
			if j.TimeoutMinutes == 0 || secs > j.TimeoutMinutes*60-120 {
				t.Errorf("api-contract job %s: download limit %d s, job limit %d min: the skip on a pull request is never reached", name, secs, j.TimeoutMinutes)
			}
		}
	}
	if !download {
		t.Error("api-contract: no step with id specs")
	}
	if !selfTest {
		t.Error("api-contract: scripts/cloudru-specs-test.py does not run")
	}
	pr, _ := w.On["pull_request"].(map[string]any)
	paths, _ := pr["paths"].([]any)
	if !slices.Contains(paths, any("scripts/cloudru-specs-test.py")) {
		t.Errorf("api-contract: pull_request paths %v miss scripts/cloudru-specs-test.py", paths)
	}
}

// The logs of cloud-e2e and api-probe are public: every job there builds evo
// with build_evo.sh, which puts on PATH a wrapper whose stderr to the log or
// a pipe goes through redact.sed, instead of a mask at each call (k8s-smoke,
// 22.10: a cloud error from k8s create reached the log unmasked); stderr to a
// file stays as it is, written by the time the call returns. The wrapper is
// tested in ci.
func TestWorkflows_CloudE2EStderrRedacted(t *testing.T) {
	ws := readWorkflows(t)
	for _, file := range []string{"cloud-e2e.yml", "api-probe.yml"} {
		builds := 0
		for name, j := range ws[file].Jobs {
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "go build") && strings.Contains(s.Run, "./cmd/evo") {
					t.Errorf("%s job %s: evo built without build_evo.sh: its stderr reaches the public log unmasked", file, name)
				}
				if strings.Contains(s.Run, ".github/scripts/build_evo.sh") {
					builds++
				}
			}
		}
		if builds == 0 {
			t.Errorf("%s: no job builds evo with build_evo.sh", file)
		}
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/evo_wrapper_test.sh") {
		t.Error("ci: the evo wrapper of the public logs is not tested (evo_wrapper_test.sh)")
	}
}

// evoOutput is a command whose output carries IDs of the cloud: evo itself
// (also by its path, "$RUNNER_TEMP/evo" of cloud-readonly-os),
// or lines of its config and state (.evo.yaml, .evo/) printed as they are.
// A jq filter prints what it selects (a count, a port), so it is not one.
var evoOutput = regexp.MustCompile(`^(timeout [0-9]+ )?"?(\S*/)?evo(\.exe)?"?\s|^(grep|cat|head|tail)\s.*\.evo(\.yaml|/)`)

// rawToLog lists the shell commands of a step whose stdout reaches the log
// as is: the last command of a pipeline, with no redirection, matching
// evoOutput.
func rawToLog(run string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(unquoteOps(run), "\\\n", " "), "\n") {
		line = strings.ReplaceAll(line, "||", ";")
		line = strings.ReplaceAll(line, "&&", ";")
		for _, list := range strings.Split(line, ";") {
			stages := strings.Split(list, "|")
			last := strings.TrimSpace(stages[len(stages)-1])
			for _, kw := range []string{"then ", "else ", "do ", "! ", "{ "} {
				last = strings.TrimPrefix(last, kw)
			}
			if evoOutput.MatchString(last) && !strings.Contains(last, ">") {
				out = append(out, last)
			}
		}
	}
	return out
}

// unquoteOps blanks the shell operators inside quotes ('^(a|b):' is a
// pattern, not a pipe), keeping the rest of the quoted text.
func unquoteOps(run string) string {
	b := []byte(run)
	var quote byte
	for i, c := range b {
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0 && strings.IndexByte("|;&>", c) >= 0:
			b[i] = ' '
		}
	}
	return string(b)
}

// The repository is public and so are the logs of its workflows: what evo
// prints (IDs of zones, images and resources of the test project) goes to
// the log through redact.sed, a file or a pipe. The evo wrapper masks only
// stderr (stdout is read by jq), so `evo init` and `grep .evo.yaml` printed
// real IDs of the catalogue in every e2e run (cloud-e2e, 23.10).
func TestWorkflows_CloudLogsGetNoRawEvoOutput(t *testing.T) {
	ws := readWorkflows(t)
	for _, file := range []string{"cloud-e2e.yml", "cloud-e2e-weekly.yml", "api-probe.yml", "cloud-readonly-os.yml"} {
		for name, j := range ws[file].Jobs {
			for _, s := range j.Steps {
				for _, cmd := range rawToLog(s.Run) {
					t.Errorf("%s job %s step %q: %q prints to the public log unmasked: pipe it through redact.sed or into a file", file, name, s.Name, cmd)
				}
			}
		}
	}
}

func TestWorkflows_RawToLog(t *testing.T) {
	for run, want := range map[string]int{
		"evo whoami":                                   1,
		"\"$RUNNER_TEMP/evo\" whoami":                  1,
		"./bin/evo.exe init":                           1,
		"evo whoami; evo vm list":                      2,
		"evolve --help":                                0,
		"evo init --non-interactive":                   1,
		"grep -E '^(region):' .evo.yaml":               1,
		"printf '%s' \"$S\" | evo configure --key x":   1,
		"[ -s x ] || evo whoami":                       1,
		"evo whoami | sed -E -f redact.sed":            0,
		"evo --json vm list > \"$RUNNER_TEMP/v.json\"": 0,
		"evo tools call x \\\n  --json > out.json":     0,
		"id=$(evo list-zones --json | jq -r .id)":      0,
		"evo deploy 2>&1 | sed -E -f r.sed":            0,
		"cat .evo/state.json":                          1,
		"cat .evo/state.json > copy":                   0,
	} {
		if got := len(rawToLog(run)); got != want {
			t.Errorf("rawToLog(%q): %d commands, want %d", run, got, want)
		}
	}
}

// A fragment left in changelog.d is missing from the CHANGELOG.md of the
// release: #277 was merged after the fold of #273, and only a manual look
// before tag.yml found it (1.3.0-rc.1). tag.yml refuses the tag while
// changelog.d of the commit holds anything but README.md — before the tag,
// so the number is not spent.
func TestWorkflows_TagRefusesUnfoldedFragments(t *testing.T) {
	steps := readWorkflows(t)["tag.yml"].Jobs["tag"].Steps
	check, tag := -1, -1
	for i, s := range steps {
		if strings.Contains(s.Run, "changelog.d") && strings.Contains(s.Run, "README.md") && strings.Contains(s.Run, `"$SHA"`) {
			check = i
		}
		if strings.Contains(s.Run, "git tag ") {
			tag = i
		}
	}
	if check < 0 {
		t.Fatal("tag.yml does not check changelog.d of the commit it tags")
	}
	if !strings.Contains(steps[check].Run, "exit 1") {
		t.Error("tag.yml: the changelog.d check does not fail the job on fragments")
	}
	if tag < 0 || check > tag {
		t.Errorf("tag.yml checks changelog.d after git tag (step %d, tag at %d): the number would be spent", check, tag)
	}
}

// The probe of DB-22 (G13-27) asks the API what is left of a deleted
// cluster's backups: it runs after the cluster's deletion, whatever came
// before, and is a record, not a check — it never fails the job (the run of
// the rc tag must not go red on a question) and it changes nothing in the
// cloud: no curl writes but the exchange of the key for a token. Nor
// does it print what names the project's resources.
func TestWorkflows_PGBackupProbeAfterDeleteChangesNothing(t *testing.T) {
	steps := readWorkflows(t)["cloud-e2e.yml"].Jobs["dbaas-pg"].Steps
	del, probe := -1, -1
	for i, s := range steps {
		if strings.Contains(s.Run, "dbaas-delete.sh") {
			del = i
		}
		if strings.Contains(s.Run, "pg-backup-probe.sh") {
			probe = i
		}
	}
	if del < 0 || probe < 0 || probe < del {
		t.Fatalf("dbaas-pg: the probe (step %d) must run after the deletion of the cluster (step %d)", probe, del)
	}
	b0, _ := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "pg-backup-probe.sh"))
	if !strings.Contains(string(b0), `echo "::add-mask::$CLUSTER_ID"`) {
		t.Error("pg-backup-probe.sh does not mask CLUSTER_ID itself")
	}
	if p := steps[probe]; !strings.Contains(p.If, "always()") || !p.ContinueOnError {
		t.Errorf("dbaas-pg: the probe needs if: always() and continue-on-error: true, has if %q, continue-on-error %v", p.If, p.ContinueOnError)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "pg-backup-probe.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	for _, l := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if strings.Contains(l, "curl ") && strings.Contains(l, "-X") && !strings.Contains(l, "/auth/token") {
			t.Errorf("pg-backup-probe.sh writes to the API: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "exit 1") || strings.Contains(l, "::error::") {
			t.Errorf("pg-backup-probe.sh fails the step: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "evo ") && strings.Contains(l, "--yes") {
			t.Errorf("pg-backup-probe.sh confirms a change: %s", strings.TrimSpace(l))
		}
		// The log is public (#278): the ID of the cluster and the name of
		// the backup are masked, never printed, and curl's own errors,
		// which may carry the URL, do not reach it.
		if strings.Contains(l, "echo ") && !strings.Contains(l, "::add-mask::") &&
			(strings.Contains(l, "$CLUSTER_ID") || strings.Contains(l, "$BACKUP_NAME")) {
			t.Errorf("pg-backup-probe.sh prints the cluster or the backup: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "curl ") && !strings.Contains(l, "2>/dev/null") {
			t.Errorf("pg-backup-probe.sh lets curl's errors into the log: %s", strings.TrimSpace(l))
		}
		// The error of a tool is on stderr, stdout empty (QA-12): thrown
		// away, the probe of rc.2 logged «reason ,» (run 40093421741).
		if strings.Contains(l, "evo tools call pg_backups") && strings.Contains(l, "2>/dev/null") {
			t.Errorf("pg-backup-probe.sh throws away the error of pg_backups: %s", strings.TrimSpace(l))
		}
	}
}

// A rule that is not there afterwards proves nothing unless it was seen
// first, in the format the check reads (Qwen on #315): close_ports and the
// check after it match port_range "P:P", and an API that read it in another
// format would make close_ports answer already_deleted with the port open
// and the check pass on nothing. The live area net reads the rule of
// open_ports before close_ports, then repeats close_ports for
// already_deleted (D26, D37); os-smoke reads the rule of vm create --port
// before vm delete and refuses its check after delete without it.
func TestWorkflows_RuleSeenBeforeItIsCheckedGone(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	start := strings.Index(script, "\narea_net() {")
	end := strings.Index(script[start+1:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("cloud-live.sh: no area_net")
	}
	net := script[start : start+1+end]
	at := func(s string) int { return strings.Index(net, s) }
	open, seen, closeP, again := at("must open_ports open_ports"), at(`"правило $RULE_PORT видно до close_ports"`),
		at("must close_ports close_ports"), at("= already_deleted ]")
	if open < 0 || seen < 0 || closeP < 0 || again < 0 || !(open < seen && seen < closeP && closeP < again) {
		t.Errorf("cloud-live.sh area_net: open_ports (%d), the rule seen (%d), close_ports (%d), repeat already_deleted (%d) — out of order or missing",
			open, seen, closeP, again)
	}
	if !strings.Contains(net, "cov close_ports") {
		t.Error("cloud-live.sh area_net: close_ports is not counted as covered")
	}
	if !strings.Contains(net[seen:], "$(port_ranges)") {
		t.Error("cloud-live.sh area_net: a rule not seen does not print the port_range the API gave (port_ranges)")
	}

	steps := readWorkflows(t)["cloud-e2e.yml"].Jobs["os-smoke"].Steps
	create, seenStep, del, gone := -1, -1, -1, -1
	for i, s := range steps {
		switch {
		case strings.Contains(s.Run, "vm create \"$VM\""):
			create = i
		case strings.Contains(s.Run, "RULE_SEEN=1"):
			seenStep = i
		case strings.Contains(s.Run, "vm delete \"$VM_ID\""):
			del = i
		case s.Name == "Правило порта удалено после delete":
			gone = i
			if !strings.Contains(s.Run, `[ "${RULE_SEEN:-}" = 1 ] || {`) {
				t.Error("cloud-e2e os-smoke: the check after delete does not refuse without the rule seen after create")
			}
		}
	}
	if create < 0 || seenStep < 0 || del < 0 || gone < 0 || !(create < seenStep && seenStep < del && del < gone) {
		t.Errorf("cloud-e2e os-smoke: vm create (%d), the rule seen (%d), vm delete (%d), the rule gone (%d) — out of order or missing",
			create, seenStep, del, gone)
	}
}

// The API answered the probe of rc.2 400 with a body that is no JSON, and
// the probe printed nothing of it (run 40093421741): such a body goes to
// the log cut and through redact.sed.
func TestWorkflows_PGBackupProbeShowsANonJSONBody(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "pg-backup-probe.sh"))
	if err != nil {
		t.Fatal(err)
	}
	body := regexp.MustCompile(`(?m)^show\(\) \{[\s\S]*?^\}`).FindString(string(b))
	// Redacted before the cut: a cut id is no id to redact.sed, and half of
	// it would reach the log (Qwen on #323).
	if body == "" || !regexp.MustCompile(`redact \| cut -c`).MatchString(body) {
		t.Errorf("pg-backup-probe.sh show(): the text of a body that is no JSON is not redacted before its cut:\n%s", body)
	}
	for _, l := range strings.Split(string(b), "\n") {
		// head -c only bounds what is read; the cut for the log is after redact.
		if strings.Contains(l, "redact") && strings.Contains(l, "head -c") && !strings.Contains(l, "redact | cut -c") {
			t.Errorf("pg-backup-probe.sh cuts before redact.sed: %s", strings.TrimSpace(l))
		}
	}
}

// The zone of a DBaaS job goes to GITHUB_ENV, and GitHub prints the env of
// every later step: unmasked, its id was in the public log of rc.2 (run
// 40093421741). A step that writes ZONE_ID masks it.
func TestWorkflows_ZoneIDMaskedBeforeEnv(t *testing.T) {
	for name, j := range readWorkflows(t)["cloud-e2e.yml"].Jobs {
		for _, s := range j.Steps {
			if strings.Contains(s.Run, "ZONE_ID=") && strings.Contains(s.Run, "GITHUB_ENV") && !strings.Contains(s.Run, "::add-mask::") {
				t.Errorf("cloud-e2e %s, %s: ZONE_ID goes to GITHUB_ENV unmasked", name, s.Name)
			}
		}
	}
}

// The Linux job of ci runs on a named image, not ubuntu-latest: on
// ubuntu-26.04 (ubuntu-latest from 24.10.2026) evo_wrapper_test.sh failed
// 2 of 2 on the tag v1.3.0-rc.2 (release run 40100673928) and passed on
// ubuntu-24 the same day, so the image changed under an unchanged commit.
// The release and cloud-e2e are pinned the same way. A job on ubuntu-latest
// runs the wrapper test without failing the run, so the move to the new
// image is a decision made on its result.
func TestWorkflows_CILinuxImageIsPinned(t *testing.T) {
	ci := readWorkflows(t)["ci.yml"]
	if ro := ci.Jobs["test"].RunsOn; !strings.Contains(ro, "'ubuntu-24.04'") {
		t.Errorf("ci test: runs-on %q — the Linux job takes ubuntu-latest, not ubuntu-24.04", ro)
	}
	// The release and the live runs (logs -f, Ctrl-C through timeout -s
	// INT) run on the same image as the tests that cleared them.
	ws := readWorkflows(t)
	for _, f := range []string{"release.yml", "cloud-e2e.yml"} {
		for name, j := range ws[f].Jobs {
			// A job with uses: runs on the image of the workflow it calls.
			if j.Uses == "" && j.RunsOn != "ubuntu-24.04" {
				t.Errorf("%s %s: runs-on %q, not ubuntu-24.04", f, name, j.RunsOn)
			}
		}
	}
	probe := false
	for name, j := range ci.Jobs {
		if j.RunsOn != "ubuntu-latest" {
			continue
		}
		for _, s := range j.Steps {
			if strings.Contains(s.Run, ".github/scripts/evo_wrapper_test.sh") {
				probe = true
				// The result is a notice either way: the probe fails
				// neither the run nor its check.
				if !strings.Contains(s.Run, "if .github/scripts/evo_wrapper_test.sh; then") || strings.Count(s.Run, "::notice") != 2 {
					t.Errorf("ci %s: the probe of ubuntu-latest does not turn its result into a ::notice", name)
				}
			}
		}
	}
	if !probe {
		t.Error("ci: no job runs evo_wrapper_test.sh on ubuntu-latest")
	}
}

// A red check is a failure someone looks into, never a state a job is in
// by design: the probe of ubuntu-latest was a job with continue-on-error,
// red on every PR and on main, and each red PR got a comment saying so
// (1.4.1: #430, #436, #438, #452). A job does not fail and pass at once; a
// probe gives its result as a ::notice. A step may still go on after its
// own failure: its job stays green.
func TestWorkflows_NoRedByDesign(t *testing.T) {
	for f, w := range readWorkflows(t) {
		for name, j := range w.Jobs {
			if j.ContinueOnError {
				t.Errorf("%s %s: continue-on-error on the job — red by design; a probe reports with ::notice and passes", f, name)
			}
		}
	}
}

// With --json the error of a tool is {"error":…} on stderr and stdout is
// empty (QA-12). Steps of cloud-e2e threw stderr away and read .error from
// stdout: pg_create on bf6b9ea failed with exit 4 and no reason (run
// 40118457142). A step reads the reason with evo_err.sh from the stderr it
// kept, never .error from stdout.
func TestWorkflows_CloudE2EReasonFromStderr(t *testing.T) {
	stdoutError := regexp.MustCompile(`jq[^|]*'\.error //`)
	for name, j := range readWorkflows(t)["cloud-e2e.yml"].Jobs {
		for _, s := range j.Steps {
			for _, l := range strings.Split(s.Run, "\n") {
				if stdoutError.MatchString(l) {
					t.Errorf("cloud-e2e %s, %s: reads .error from stdout, where --json never puts it: %s", name, s.Name, strings.TrimSpace(l))
				}
			}
		}
	}
}

// evo_err.sh prints the reason and the message of the error evo wrote on
// stderr, redacted and cut, and the start of a stderr that is no error JSON.
func TestWorkflows_EvoErrPrintsTheRedactedReason(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash script: CI runs it on Linux")
	}
	dir := t.TempDir()
	run := func(stderr string) string {
		f := filepath.Join(dir, "err")
		if err := os.WriteFile(f, []byte(stderr), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("bash", filepath.Join("..", "..", ".github", "scripts", "evo_err.sh"), f).CombinedOutput()
		if err != nil {
			t.Fatalf("evo_err.sh: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	id := "00000000-0000-4000-8000-0000000000aa"
	if got := run("==> [1/2] Reading...\n{\"error\":{\"code\":4,\"reason\":\"NOT_FOUND\",\"message\":\"subnet " + id + " not found\"}}\n"); got != "NOT_FOUND: subnet <id> not found" {
		t.Errorf("error JSON on stderr: %q", got)
	}
	if got := run("panic: boom near " + id + "\n"); got != "panic: boom near <id>" {
		t.Errorf("stderr that is no error JSON: %q", got)
	}
	if got := run(strings.Repeat("x", 500)); len(got) > 300 {
		t.Errorf("not cut to 300: %d", len(got))
	}
}

// The probe of the Go runtime crash on Windows (release plan 1.5, #493)
// runs a variant of ci.yml's go test on windows-latest many times, the CI
// command whole: the crash comes only under its load (14 of 828 attempts
// of test (windows-latest) on 22–28.10, none in 192 runs of internal/tools
// alone). At most six jobs at once — the runners of the other sessions
// stay free; no secrets, read only; the weekly race does not run with it.
func TestWorkflows_WindowsCIProbe(t *testing.T) {
	w := weeklyWorkflow(t, "weekly-race.yml")
	d, _ := w.On["workflow_dispatch"].(map[string]any)
	in, _ := d["inputs"].(map[string]any)
	p, _ := in["windows_ci_probe"].(map[string]any)
	if p["type"] != "choice" || fmt.Sprint(p["options"]) != "[none together split norace]" || p["default"] != "none" {
		t.Fatalf("weekly-race: windows_ci_probe is not the choice of the arms of ci-probe-loop.sh: %v", p)
	}
	if !strings.Contains(w.Jobs["race"].If, "(inputs.windows_ci_probe || 'none') == 'none'") {
		t.Errorf("weekly-race: the race job runs with the CI probe: if %q", w.Jobs["race"].If)
	}
	j, ok := w.Jobs["ci-probe"]
	if !ok || j.RunsOn != "windows-latest" || !strings.Contains(j.If, "(inputs.windows_ci_probe || 'none') != 'none'") {
		t.Fatalf("weekly-race: no ci-probe job on windows-latest behind the input (runs-on %q, if %q)", j.RunsOn, j.If)
	}
	if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["contents"] != "read" {
		t.Errorf("weekly-race: ci-probe permissions %v, want contents: read only", j.Permissions)
	}
	if n := j.Strategy.MaxParallel; n < 1 || n > 6 {
		t.Errorf("weekly-race: ci-probe max-parallel %d, want 1–6", n)
	}
	found := false
	for _, s := range j.Steps {
		if strings.Contains(s.Run, "ci-probe-loop.sh \"$ARM\"") && strings.Contains(s.Env["ARM"], "inputs.windows_ci_probe") {
			found = true
		}
	}
	if !found {
		t.Error("weekly-race: ci-probe does not run ci-probe-loop.sh with the arm through env")
	}
}

// The live area disk changes the data disk only from a settled state: after
// attach and after resize the disk passes through attaching or extending,
// and a change in it is 422 disk_can_not_be_changed_from_current_state
// (run 39152048559) or disk_can_not_be_detached_from_current_state (cloud-e2e
// of the 1.5 candidate, run 142, #521). Every disk_resize and disk_detach of
// the data disk follows a poll of settled (or of state available) after
// the change before it.
func TestWorkflows_LiveDiskChangesFromASettledState(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "cloud-live.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	change := regexp.MustCompile(`\b(disk_attach|disk_resize|disk_detach)\b.*--arg disk_id="\$data"`)
	settledSince, changes := true, 0
	for i, l := range lines {
		switch {
		// A poll of settled, or one that waits for state available.
		case strings.Contains(l, "poll") && (strings.Contains(l, "settled") || strings.Contains(l, `.state == "available"`)):
			settledSince = true
		case change.MatchString(l) && !strings.HasPrefix(strings.TrimSpace(l), "#"):
			changes++
			op := change.FindStringSubmatch(l)[1]
			if op != "disk_attach" && !settledSince {
				t.Errorf("cloud-live.sh:%d: %s of the data disk right after another change, with no poll of settled between", i+1, op)
			}
			settledSince = false
		}
	}
	if changes < 3 {
		t.Fatalf("cloud-live.sh: %d changes of the data disk found: the area moved, update the test", changes)
	}
}
