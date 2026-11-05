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

// The goreleaser check of ci.yml runs the version release.yml releases
// with, from an archive checked against a pinned sha256, fetched with
// retries: goreleaser-action fetched it with three tries and failed the
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
	// The binary run with check, its path quoted or not.
	runsCheck := regexp.MustCompile(`goreleaser["']?\s+check\b`)
	found := false
	for name, j := range ws["ci.yml"].Jobs {
		for _, s := range j.Steps {
			if s.Name != "goreleaser check" {
				continue
			}
			found = true
			where := "ci.yml " + name + ": goreleaser check"
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
				t.Errorf("%s: run does not run goreleaser check", where)
			}
		}
	}
	if !found {
		t.Fatal("ci.yml: no step «goreleaser check» — the check sees nothing, update it")
	}
}
