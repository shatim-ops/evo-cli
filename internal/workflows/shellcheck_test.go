// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"regexp"
	"strings"
	"testing"
)

// shellcheck in CI is the version sessions run (plan 1.9, «Локальная
// проверка скриптов — той же версией shellcheck, что в CI», #1523): the
// runner took its image's shellcheck, and a remark the runner failed on did
// not show locally — a red round of CI cost 15–20 minutes. Every step runs
// it through .github/scripts/shellcheck.sh, which holds the one version.
func TestWorkflows_ShellcheckIsOneVersion(t *testing.T) {
	ws := readWorkflows(t)
	bare := regexp.MustCompile(`(^|[\s;&|(])shellcheck\s`)
	// "# shellcheck disable=…" in a step's script is a directive, not a run.
	directive := regexp.MustCompile(`#\s*shellcheck\s`)
	for file, w := range ws {
		for name, j := range w.Jobs {
			for _, s := range j.Steps {
				if bare.MatchString(directive.ReplaceAllString(s.Run, "#")) {
					t.Errorf("%s: job %s runs shellcheck itself, not .github/scripts/shellcheck.sh: %q", file, name, s.Run)
				}
			}
		}
	}
	if !runsStep(ws["ci.yml"], ".github/scripts/shellcheck.sh install.sh internal/bootstrap/install.sh .github/scripts/*.sh") {
		t.Error("ci: the scripts are not checked by .github/scripts/shellcheck.sh")
	}
	// actionlint checks the scripts of the steps with shellcheck too: the
	// runner's 0.10 flagged what 0.11 does not (SC2015, Qwen on #1981).
	if !runsStep(ws["ci.yml"], "actionlint\" -shellcheck=.github/scripts/shellcheck.sh") {
		t.Error("ci: actionlint checks the steps' scripts with the runner's shellcheck, not .github/scripts/shellcheck.sh")
	}
	for file, w := range ws {
		for name, j := range w.Jobs {
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "SHELLCHECK_OPTS") || j.Env["SHELLCHECK_OPTS"] != "" || s.Env["SHELLCHECK_OPTS"] != "" {
					t.Errorf("%s: job %s narrows shellcheck with SHELLCHECK_OPTS: one version needs no exclusions of another", file, name)
				}
			}
		}
	}
}
