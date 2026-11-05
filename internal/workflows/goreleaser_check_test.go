// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The goreleaser check and the dev build of ci.yml run the version
// release.yml releases with, from an archive checked against a pinned
// sha256, fetched with retries: goreleaser-action fetched it with three tries and failed the
// job on 504 of GitHub releases while the tests were green (#952, #964,
// 05.11). A version raised in one workflow and not the other fails here.
func TestWorkflows_GoreleaserCheckSurvivesReleasesOutage(t *testing.T) {
	ws := readWorkflows(t)
	// Every goreleaser step of release.yml runs one version: the one ci.yml checks with.
	release := map[string]bool{}
	for _, j := range ws["release.yml"].Jobs {
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Uses, "goreleaser/goreleaser-action@") {
				release[fmt.Sprint(s.With["version"])] = true
			}
		}
	}
	if len(release) != 1 {
		t.Fatalf("release.yml: goreleaser versions %v, want one", release)
	}
	sha := regexp.MustCompile(`^[0-9a-f]{64}$`)
	// The binary run with its command, its path quoted or not.
	runs := map[string]*regexp.Regexp{
		"goreleaser check":    regexp.MustCompile(`goreleaser["']?\s+check\b`),
		"goreleaser snapshot": regexp.MustCompile(`goreleaser["']?\s+release\s+--snapshot\b`),
	}
	found := map[string]bool{}
	for name, j := range ws["ci.yml"].Jobs {
		for _, s := range j.Steps {
			runsCheck := runs[s.Name]
			if runsCheck == nil {
				continue
			}
			found[s.Name] = true
			where := "ci.yml " + name + ": " + s.Name
			if s.Uses != "" {
				t.Errorf("%s uses %s: its download has no retries that outlive a 504 of GitHub releases — fetch with curl --retry", where, s.Uses)
			}
			if v := s.Env["GORELEASER_VERSION"]; !release[v] {
				t.Errorf("%s: GORELEASER_VERSION %q, release.yml releases with %v — raise both", where, v, release)
			}
			if !sha.MatchString(s.Env["GORELEASER_SHA256"]) {
				t.Errorf("%s: GORELEASER_SHA256 %q is not a sha256 — pin the archive's from checksums.txt of the release", where, s.Env["GORELEASER_SHA256"])
			}
			for _, w := range []string{"--retry-all-errors", "sha256sum -c"} {
				if !strings.Contains(s.Run, w) {
					t.Errorf("%s: run lacks %q", where, w)
				}
			}
			if !runsCheck.MatchString(s.Run) {
				t.Errorf("%s: run does not run %s", where, s.Name)
			}
		}
	}
	for step := range runs {
		if !found[step] {
			t.Errorf("ci.yml: no step «%s» — the check sees nothing, update it", step)
		}
	}
}

// The dev build of main (release-plan B-5, #1000) is a build, not a
// release: it runs on a push to main after the tests, with a read-only
// token and no secret, publishes nothing (a snapshot, no tag, no release,
// no signature) and keeps its archives as an artifact for 7 days, named
// -dev with the commit.
func TestWorkflows_DevBuildIsNoRelease(t *testing.T) {
	ci := readWorkflows(t)["ci.yml"]
	j, ok := ci.Jobs["dev-build"]
	if !ok {
		t.Fatal("ci.yml: no job dev-build — the check sees nothing, update it")
	}
	if j.If != "github.event_name == 'push' && github.ref == 'refs/heads/main'" {
		t.Errorf("dev-build: if %q — only a push to main", j.If)
	}
	if fmt.Sprint(j.Needs) != "test" {
		t.Errorf("dev-build: needs %v — after the tests", j.Needs)
	}
	if fmt.Sprint(j.Permissions) != "map[contents:read]" {
		t.Errorf("dev-build: permissions %v — contents: read alone", j.Permissions)
	}
	if j.Environment != "" {
		t.Errorf("dev-build: environment %q — no secrets of an environment", j.Environment)
	}
	uploads := 0
	for _, s := range j.Steps {
		text := s.Run + fmt.Sprint(s.With) + fmt.Sprint(s.Env)
		if strings.Contains(text, "secrets.") {
			t.Errorf("dev-build: step %q reads a secret", s.Name+s.Uses)
		}
		for _, w := range []string{"goreleaser-action", "cosign", "gh release", "git tag", "git push"} {
			if strings.Contains(s.Uses+s.Run, w) {
				t.Errorf("dev-build: step %q holds %q — a dev build publishes nothing", s.Name+s.Uses, w)
			}
		}
		if s.Run != "" && strings.Contains(s.Run, "goreleaser\" release") && !strings.Contains(s.Run, "--snapshot") {
			t.Errorf("dev-build: goreleaser release without --snapshot")
		}
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploads++
			if fmt.Sprint(s.With["retention-days"]) != "7" {
				t.Errorf("dev-build: retention-days %v, want 7", s.With["retention-days"])
			}
			if name := fmt.Sprint(s.With["name"]); !strings.Contains(name, "-dev-") || !strings.Contains(name, "steps.build.outputs.short") {
				t.Errorf("dev-build: artifact %q — -dev- and the short commit", name)
			}
		}
	}
	if uploads != 1 {
		t.Errorf("dev-build: %d uploads of an artifact, want 1", uploads)
	}
}
