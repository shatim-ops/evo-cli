// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/journal"
)

// The journal evo writes passes scripts/diag-check.py --public, which the
// dogfood triage and cloud-e2e run on it: the two hold one format of a
// record (field, session mark, surface, os), and a change of either that
// the other does not follow fails here, not on a participant's file. The
// journal is of every tool of the contract world, with its contract
// arguments — UUIDs among them, which must not get in.
func TestJournal_PassesDiagCheck(t *testing.T) {
	py := pythonFor(t)
	journalOn(t)
	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	for _, tl := range reg.Tools() {
		if contractHTTPSkip[tl.Name] {
			continue
		}
		contractCall(tl, contractArgs(tl), contractCallLimit)
		rt.take()
	}
	if len(journalRecords(t)) == 0 {
		t.Fatal("no record")
	}
	script := filepath.Join("..", "..", "scripts", "diag-check.py")
	out, err := exec.Command(py, script, "--public", journal.Path()).CombinedOutput()
	if err != nil {
		t.Fatalf("diag-check --public refuses the journal evo writes: %v\n%s", err, out)
	}
}

// pythonFor finds Python 3: python3, or python on Windows. Without one the
// test is skipped on a developer's machine and fails in CI, where every
// runner has it.
func pythonFor(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			if exec.Command(p, "-c", "import sys; sys.exit(sys.version_info[0] != 3)").Run() == nil {
				return p
			}
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatal("no Python 3 in CI")
	}
	t.Skip("no Python 3")
	return ""
}
