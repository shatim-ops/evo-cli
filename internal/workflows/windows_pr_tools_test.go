// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pull request that touches the code of paths in internal/tools runs
// internal/tools on Windows (release plan 1.9, «CI: main красный на
// Windows…», М4): a pull request tests on Windows only the packages with
// code of Windows (owner's decision 29.10), internal/tools is not one, and
// on 12.11 a test that took a path apart as a string with "/" passed its
// pull request and kept main red for six hours. The chain has four links,
// and a rule that loses one runs nothing with no word of it: classify.sh
// prints wintools=, the job scope hands it on, the step of the tests takes
// it as WINDOWS_PR_TOOLS, go_test_ci.sh adds the package.
func TestWorkflows_PathCodeOfToolsRunsOnWindowsInAPullRequest(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	classify := read(".github/scripts/classify.sh")
	for _, want := range []string{
		`internal/tools/intent*.go | internal/intent/*.go) wintools=yes`,
		`grep -q '"path/filepath"' -- "$p"`,
		`echo "wintools=${wintools:-no}"`,
	} {
		if !strings.Contains(classify, want) {
			t.Errorf("classify.sh: no %q — the rule of wintools= is not the one the plan names", want)
		}
	}
	// Before the tests are set aside: the defect of 12.11 was in a test.
	if rule, tests := strings.Index(classify, "wintools=yes"), strings.Index(classify, "*_test.go) add tests; continue"); rule < 0 || tests < 0 || rule > tests {
		t.Error("classify.sh: wintools= is decided after the tests are set aside (continue): a _test.go file never sets it")
	}

	ci := read(".github/workflows/ci.yml")
	if !strings.Contains(ci, "wintools: ${{ steps.paths.outputs.wintools }}") {
		t.Error("ci: the job scope does not hand wintools on (outputs)")
	}
	w := readWorkflows(t)["ci.yml"]
	takes := false
	for _, s := range w.Jobs["scope"].Steps {
		if s.ID == "paths" {
			takes = strings.Contains(s.Run, ".github/scripts/classify.sh | grep '^wintools=' >> \"$GITHUB_OUTPUT\"")
		}
	}
	if !takes {
		t.Error("ci: the step paths of scope does not take wintools= from classify.sh")
	}
	passed := false
	for _, s := range w.Jobs["test"].Steps {
		if s.ID == "gotest" {
			passed = strings.ReplaceAll(s.Env["WINDOWS_PR_TOOLS"], " ", "") == "${{needs.scope.outputs.wintools}}"
		}
	}
	if !passed {
		t.Error("ci: the step gotest does not take WINDOWS_PR_TOOLS from needs.scope.outputs.wintools")
	}
	if script := read(".github/scripts/go_test_ci.sh"); !strings.Contains(script, `[ "${WINDOWS_PR_TOOLS:-}" != yes ] || passes+=("$tools")`) {
		t.Error("go_test_ci.sh: WINDOWS_PR_TOOLS=yes does not add internal/tools to the passes of Windows in a pull request")
	}
}
