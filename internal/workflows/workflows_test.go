// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package workflows checks the GitHub Actions workflows of the repository:
// rules that keep a pull request's code away from the secrets.
package workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		Steps          []struct {
			ID   string            `yaml:"id"`
			Name string            `yaml:"name"`
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

// On Windows the race-enabled go test runs with async preemption off until
// CI moves to a Go with the fix: the runtime crashed there with 0xc0000005
// in adjustdefers during shrinkstack, in internal/tools, on PRs whose diff
// could not cause it (#226, #219; hypothesis golang/go#79249, fixed for Go
// 1.28). Other systems keep the default. On Go 1.28 and later the setting
// must go, so the test fails until it is removed.
func TestWorkflows_WindowsRaceWithoutAsyncPreemptBeforeGo128(t *testing.T) {
	const want = "${{ runner.os == 'Windows' && 'asyncpreemptoff=1' || '' }}"
	ws := readWorkflows(t)
	for _, file := range []string{"ci.yml", "weekly-race.yml"} {
		found := false
		for name, j := range ws[file].Jobs {
			goVersion := ""
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/setup-go@") {
					goVersion, _ = s.With["go-version"].(string)
				}
			}
			for _, s := range j.Steps {
				if !strings.Contains(s.Run, "go test -race") {
					continue
				}
				found = true
				got, set := s.Env["GODEBUG"]
				switch fixed := goAtLeast(goVersion, 1, 28); {
				case fixed && set:
					t.Errorf("%s: job %s runs Go %s with the fix of golang/go#79249: remove GODEBUG %q", file, name, goVersion, got)
				case !fixed && got != want:
					t.Errorf("%s: job %s (Go %q) go test -race GODEBUG %q, want %q", file, name, goVersion, got, want)
				}
			}
		}
		if !found {
			t.Errorf("%s: no go test -race step", file)
		}
	}
}

// goAtLeast reports whether version ("1.26.10") is major.minor or later.
func goAtLeast(version string, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(version, "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma > major || ma == major && mi >= minor
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
