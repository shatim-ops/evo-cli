// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cli

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/login"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// loginKillChildEnv makes the test binary the child of
// TestLogin_KilledAfterTheKeyIsNamed: evo login for an agent on the world
// of B-2, whose IAM made the key and whose answer the child holds.
const loginKillChildEnv = "EVO_TEST_LOGIN_KILL_CHILD"

// holdAfterKey answers the POST of the access key and then holds: the key
// is in the cloud, its secret in the process, the profile not written.
type holdAfterKey struct {
	inner  http.RoundTripper
	parent *os.File
	tell   string
}

func (h holdAfterKey) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := h.inner.RoundTrip(r)
	if err == nil && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/accessKeys") && resp.StatusCode == http.StatusOK {
		fmt.Fprintln(h.parent, h.tell)
		select {} // the parent kills
	}
	return resp, err
}

// TestLoginKillChild is the child: it tells the parent on fd 3 its config
// directory and working directory once the key is made, and waits there
// for the SIGKILL. It does nothing unless the parent started it.
func TestLoginKillChild(t *testing.T) {
	if os.Getenv(loginKillChildEnv) == "" {
		t.Skip("the child of TestLogin_KilledAfterTheKeyIsNamed")
	}
	parent := os.NewFile(3, "parent")
	setupCLI(t)
	grantingWorld(t, loginworld.Variant{}, login.ForAgent)
	wd, _ := os.Getwd()
	// The calls of IAM admin go through the transport of every evo client.
	old := httpretry.DefaultBase
	t.Cleanup(func() { httpretry.DefaultBase = old })
	httpretry.DefaultBase = holdAfterKey{inner: old, parent: parent, tell: os.Getenv(config.EnvConfigDir) + "\t" + wd}
	code, out, errOut := runCLI(t, "", "--json", "login", "--for", "agent", "--key-ttl", "7d")
	t.Fatalf("the login answered (exit %d): the child holds after the key\n%s%s", code, out, errOut)
}

// Findings ledger 1.8, № 37 (R4-10, AUTH-52; plan 1.8, «Живые шаги гейта
// rc.1», (3); «Риск», conclusion 71: the outcome, not the exit code):
// evo login killed after IAM made the key and before the profile is written
// leaves a key in the cloud that no profile holds — a working credential
// nobody knows of, which no logout deletes. The next run names it, as a
// cut paid create is named (R-59): evo doctor reports a pending operation
// of the login — whose key, which account, where to delete it.
func TestLogin_KilledAfterTheKeyIsNamed(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLoginKillChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), loginKillChildEnv+"=1")
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
	var told string
	select {
	case told = <-got:
	// wall-clock: the child's login reaches the key in well under a
	// second; a child that never does is a failure, not a wait.
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
	}
	if told == "" {
		_ = cmd.Wait()
		t.Fatalf("the child did not reach the key:\n%s", stderr.String())
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	cfgDir, wd, _ := strings.Cut(told, "\t")
	t.Cleanup(func() { _ = os.RemoveAll(cfgDir); _ = os.RemoveAll(wd) }) // the child's cleanup did not run

	// The next run, in another folder than the child's: the key belongs to
	// the profiles, and doctor names it from any folder («Риск»,
	// conclusion 75, condition 1).
	setupCLI(t)
	t.Setenv(config.EnvConfigDir, cfgDir)
	t.Chdir(t.TempDir())
	if _, _, found, _ := config.LoadProfile("agent"); found {
		t.Fatal("the profile agent was written: the kill came after the save, not between the key and the profile")
	}
	newProcess()
	_, out, errOut := runCLI(t, "", "--json", "doctor")
	// Named by its time, so that no working key of an agent is deleted
	// for it (condition 2).
	if !strings.Contains(out+errOut, "pending operation") || !strings.Contains(out+errOut, "login") || !strings.Contains(out+errOut, "created at about") {
		t.Errorf("killed after the key and before the profile, evo login left a key in the cloud, and evo doctor does not name it:\n%.600s%.300s", out, errOut)
	}
}
