// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// intentKillChildEnv makes the test binary the child of
// TestIntent_KilledAfterConsentIsNamed: evo disk create with --yes in the
// JSON world, whose cloud never answers the create.
const intentKillChildEnv = "EVO_TEST_INTENT_KILL_CHILD"

// intentKillName is the name of the disk the child creates.
const intentKillName = "r59-disk"

// TestIntentKillChild is the child: it tells the parent on fd 3 the
// directory of its world once the cloud has the create, and waits there
// for the SIGKILL. It does nothing unless the parent started it.
func TestIntentKillChild(t *testing.T) {
	if os.Getenv(intentKillChildEnv) == "" {
		t.Skip("the child of TestIntent_KilledAfterConsentIsNamed")
	}
	parent := os.NewFile(3, "parent")
	w, _ := withJSONWorld(t)
	inner := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Host == "compute.api.cloud.ru" && strings.HasSuffix(r.URL.Path, "/disks") {
			fmt.Fprintln(parent, w.dir)
			select {} // the cloud has the call and never answers: the parent kills
		}
		return inner(r)
	}
	code, out, errOut := runCLI(t, "", "--json", "--yes", "disk", "create", intentKillName, "--size-gb", "20")
	t.Fatalf("the create answered (exit %d): the cloud of the child does not\n%s%s", code, out, errOut)
}

// R-59 (round 3 of Grok, F1; cloud-e2e 181, R3F1): evo killed after
// «Confirmed», with the create in the cloud and no answer, left a paid disk
// and nothing in evo. Decision of the owner 10.11 (plan 1.8, B-5): the
// intent stays, and the next run names it — evo doctor (pending
// operation), the stderr of the next command, the first answer over MCP —
// until the same create with the name answers and settles it.
func TestIntent_KilledAfterConsentIsNamed(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntentKillChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), intentKillChildEnv+"=1")
	cmd.ExtraFiles = []*os.File{pw}
	var stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		if sc.Scan() {
			got <- sc.Text()
		}
		close(got)
	}()
	var dir string
	select {
	case dir = <-got:
	// wall-clock: the child builds its world and reaches the create in
	// well under a second; a child that never does is a failure, not a wait.
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
	}
	if dir == "" {
		_ = cmd.Wait()
		t.Fatalf("the child did not reach the create of the disk:\n%s", stderr.String())
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) // the child's cleanup did not run

	left := intent.Pending(dir)
	if len(left) != 1 || left[0].Tool != "disk_create" || left[0].Name != intentKillName || left[0].Surface != "cli" {
		t.Fatalf("killed after the consent, the child left %+v — want one intent of disk_create %s, surface cli", left, intentKillName)
	}

	// The next run, in a world of its own with what the child left.
	w, _ := withJSONWorld(t)
	if err := os.MkdirAll(intent.Dir(w.dir), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(intent.Dir(dir), left[0].ID+".json")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intent.Dir(w.dir), left[0].ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	names := func(s string) bool { return strings.Contains(s, "disk_create") && strings.Contains(s, intentKillName) }

	newProcess()
	_, out, errOut := runCLI(t, "", "--json", "doctor")
	if !strings.Contains(out+errOut, "pending operation") || !names(out+errOut) {
		t.Errorf("evo doctor does not name the cut disk_create %s:\n%s%s", intentKillName, out, errOut)
	}
	newProcess()
	_, _, errOut = runCLI(t, "", "disk", "list")
	if !names(errOut) {
		t.Errorf("the next command does not name the cut disk_create %s on stderr:\n%s", intentKillName, errOut)
	}
	newProcess()
	flagJSON, flagDryRun, flagYes = false, false, false
	tl, _ := newRegistry().Lookup("disk_list")
	text, _ := tl.Handler(context.Background(), map[string]any{})
	if !strings.Contains(text, "pending_operations") || !names(text) {
		t.Errorf("the first answer over MCP does not carry the cut disk_create %s: %.400s", intentKillName, text)
	}

	// The same create with the name answers: the intent is settled.
	newProcess()
	if code, out, errOut := runCLI(t, "", "--json", "--yes", "disk", "create", intentKillName, "--size-gb", "20"); code != 0 {
		t.Fatalf("the create again: exit %d\n%s%s", code, out, errOut)
	}
	if p := intent.Pending(w.dir); len(p) != 0 {
		t.Errorf("the create of %s answered, and the intents stay: %+v", intentKillName, p)
	}
}
