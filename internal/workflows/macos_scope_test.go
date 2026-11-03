// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A pull request that changes only docs/internal/*.md runs ci without
// macOS: GitHub runs at most five macOS jobs at once and drops the rest
// unstarted, and the PRs of the plan and the decisions are half the flow
// (CLAUDE.md §4, the owner's decision of 03.11, #800). The decision is
// classify.sh's (its macos= line), as the PR's class is; everything that is
// not a pull request runs the full matrix. The required check
// test (macos-latest) is a success with its reason, not a pending check
// that blocks the merge; a failed scope runs the full matrix, not none
// (a skipped required check passes).
func TestWorkflows_MacOSOnlyOffForInternalDocs(t *testing.T) {
	w := readWorkflows(t)["ci.yml"]
	scope, ok := w.Jobs["scope"]
	if !ok {
		t.Fatal("ci: no job scope")
	}
	decides := false
	for _, s := range scope.Steps {
		if strings.Contains(s.Run, ".github/scripts/classify.sh") {
			decides = strings.Contains(s.Run, "--no-renames") && strings.Contains(s.Run, "grep '^macos='") &&
				strings.Contains(s.Run, `if [ "$EVENT" != pull_request ]; then`) && strings.Contains(s.Run, "echo macos=yes")
		}
	}
	if !decides {
		t.Error("ci: scope does not take macos= from classify.sh (renames as two paths), with macos=yes for every event but pull_request")
	}

	test := w.Jobs["test"]
	if test.Needs != "scope" || !strings.Contains(strings.ReplaceAll(test.If, " ", ""), "!cancelled()") {
		t.Errorf("ci: test needs %v, if %q: want scope, and the full matrix when scope failed (!cancelled())", test.Needs, test.If)
	}
	m := regexp.MustCompile(`^\$\{\{ fromJSON\(needs\.scope\.outputs\.macos == 'no' && '(\[[^']*\])' \|\| '(\[[^']*\])'\) \}\}$`).
		FindStringSubmatch(test.Strategy.Matrix["os"].(string))
	if m == nil {
		t.Fatalf("ci: test's matrix os %v is not chosen by scope's macos", test.Strategy.Matrix["os"])
	}
	var off, full []string
	if json.Unmarshal([]byte(m[1]), &off) != nil || json.Unmarshal([]byte(m[2]), &full) != nil {
		t.Fatalf("ci: test's matrix lists are not JSON: %q %q", m[1], m[2])
	}
	all := []string{"ubuntu-latest", "macos-latest", "windows-latest"}
	if !slices.Equal(full, all) {
		t.Errorf("ci: the full matrix is %v, want %v", full, all)
	}
	if !slices.Equal(off, []string{"ubuntu-latest", "windows-latest"}) {
		t.Errorf("ci: without macOS the matrix is %v, want Linux and Windows", off)
	}

	stub, ok := w.Jobs["test-macos-skipped"]
	if !ok {
		t.Fatal("ci: no job that answers test (macos-latest) when macOS is off")
	}
	if stub.Needs != "scope" || stub.If != "needs.scope.outputs.macos == 'no'" ||
		stub.Name != "${{ needs.scope.outputs.macos == 'no' && 'test (macos-latest)' || 'macOS в матрице test' }}" ||
		strings.Contains(stub.RunsOn, "macos") {
		t.Errorf("ci: test-macos-skipped (needs %v, if %q, name %q, runs-on %q): want the name test (macos-latest) only when scope says macos=no, off a macOS runner",
			stub.Needs, stub.If, stub.Name, stub.RunsOn)
	}
}
