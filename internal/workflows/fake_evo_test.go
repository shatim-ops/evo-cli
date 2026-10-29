// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// ownFakeEvo is a script test that writes an evo of its own: a file evo
// under a directory it puts on PATH (cat > "$dir/bin/evo" <<…, or a path
// written out: cat > /tmp/bin/evo <<…).
var ownFakeEvo = regexp.MustCompile(`>\s*"?[^"\s<>]*/evo"?\s*<<`)

// The checks of the scripts of cloud-e2e (.github/scripts/*_test.sh) put
// one fake evo on PATH, fake_evo.sh, in the format of the real one: a
// fake of its own wrote the error to stdout, the script read it there, and
// its branches FAILED and «still being created» never ran (run 144, #525,
// #526; process backlog 1.6).
func TestWorkflows_ScriptTestsShareOneFakeEvo(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "scripts", "*_test.sh"))
	if err != nil || len(files) == 0 {
		t.Fatal("no script tests in .github/scripts")
	}
	users := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if ownFakeEvo.Match(b) {
			t.Errorf("%s writes an evo of its own: install the one of fake_evo.sh (fake_evo_install, fake_evo_answer)", filepath.Base(f))
		}
		if bytes.Contains(b, []byte("fake_evo_install")) {
			users++
		}
	}
	if users == 0 {
		t.Error("no script test installs fake_evo.sh: the pattern moved, update the test")
	}
}

// The fake evo answers as the real one with --json (QA-12,
// TestQA12_ToolsCallJSONErrorOnlyStderr in internal/cli): exit 0 — the
// answer on stdout; otherwise the {"error":…} on stderr and stdout empty;
// a call it has no answer for is an error, not an empty success.
func TestWorkflows_FakeEvoAnswersLikeEvo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash script: CI runs it on Linux")
	}
	dir := t.TempDir()
	lib := filepath.Join("..", "..", ".github", "scripts", "fake_evo.sh")
	setup := `. "$1" && fake_evo_install "$FAKE_DIR/bin" &&
fake_evo_answer pg_list 0 0 '{"clusters":[]}' &&
fake_evo_answer pg_delete 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"boom"}}' &&
fake_evo_answer pg_delete.yes 0 0 '{"status":"deleting"}'`
	cmd := exec.Command("bash", "-c", setup, "setup", lib)
	cmd.Env = append(os.Environ(), "FAKE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fake_evo.sh: %v\n%s", err, out)
	}
	call := func(args ...string) (int, string, string) {
		cmd := exec.Command(filepath.Join(dir, "bin", "evo"), args...)
		cmd.Env = append(os.Environ(), "FAKE_DIR="+dir)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, stdout.String(), stderr.String()
	}
	for i := 1; i <= 3; i++ { // one answer: the calls after it repeat it
		if code, out, errOut := call("tools", "call", "pg_list", "--json"); code != 0 || !strings.Contains(out, "clusters") || errOut != "" {
			t.Errorf("success, call %d: exit %d, stdout %q, stderr %q; want 0, the answer on stdout", i, code, out, errOut)
		}
	}
	if code, out, errOut := call("tools", "call", "pg_delete", "--arg", "cluster_id=a", "--json"); code != 5 || out != "" || !strings.Contains(errOut, `"error"`) {
		t.Errorf("error: exit %d, stdout %q, stderr %q; want 5, stdout empty, the error on stderr", code, out, errOut)
	}
	if code, out, _ := call("--yes", "tools", "call", "pg_delete", "--json"); code != 0 || !strings.Contains(out, "deleting") {
		t.Errorf("--yes: exit %d, stdout %q; want the answer of pg_delete.yes", code, out)
	}
	if code, out, errOut := call("tools", "call", "vm_list", "--json"); code == 0 || out != "" || !strings.Contains(errOut, "no answer for vm_list call 0") {
		t.Errorf("no answer: exit %d, stdout %q, stderr %q; want an error naming the call", code, out, errOut)
	}
	// N with a leading zero would name a file no call reads.
	bad := exec.Command("bash", "-c", `. "$1" && fake_evo_answer pg_get 00 0 '{}'`, "bad", lib)
	bad.Env = append(os.Environ(), "FAKE_DIR="+dir)
	if out, err := bad.CombinedOutput(); err == nil || !strings.Contains(string(out), "N is 0, 1, 2") {
		t.Errorf("fake_evo_answer with N 00: %v %q; want a refusal", err, out)
	}
}
