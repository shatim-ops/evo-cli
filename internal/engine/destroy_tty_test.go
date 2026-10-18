// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// destroyEngineWithVM returns an Engine whose state holds one running VM.
// Auth and Cloud are nil on purpose: any attempt to reach the cloud panics,
// which proves the VM was not touched.
func destroyEngineWithVM(t *testing.T) (*Engine, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{
		ID: "vm-1", VMID: "vm-1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	return &Engine{Printer: output.NewWithWriter(&buf, output.ModePlain, false), StateDir: dir}, &buf
}

func withPromptStdin(t *testing.T, r io.Reader, tty bool) {
	t.Helper()
	oldIn, oldTTY := confirm.Stdin, confirm.StdinIsTTY
	confirm.Stdin = r
	confirm.StdinIsTTY = func() bool { return tty }
	t.Cleanup(func() { confirm.Stdin, confirm.StdinIsTTY = oldIn, oldTTY })
}

func assertVMStillInState(t *testing.T, e *Engine) {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a == nil || a.Status != "running" {
		t.Fatalf("deployment changed: %+v", a)
	}
}

func TestDestroy_NoTTYWithoutForce_RequiresConfirmation(t *testing.T) {
	e, _ := destroyEngineWithVM(t)
	withPromptStdin(t, strings.NewReader("\n"), false)

	err := e.Destroy(context.Background(), DestroyOptions{})
	if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitConfirmationRequired)
	}
	assertVMStillInState(t, e)
}

func TestDestroy_AnswerNotYes_IsNotSuccess(t *testing.T) {
	e, buf := destroyEngineWithVM(t)
	withPromptStdin(t, strings.NewReader("no\n"), true)

	err := e.Destroy(context.Background(), DestroyOptions{})
	if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitConfirmationRequired)
	}
	if !strings.Contains(buf.String(), "[y/N]") {
		t.Errorf("prompt not shown: %q", buf.String())
	}
	assertVMStillInState(t, e)
}

func TestDestroy_PromptGoesToStderr(t *testing.T) {
	e, _ := destroyEngineWithVM(t)
	var stdout, stderr bytes.Buffer
	e.Printer = output.NewWithWriters(&stdout, &stderr, output.ModePlain, false)
	withPromptStdin(t, strings.NewReader("no\n"), true)

	_ = e.Destroy(context.Background(), DestroyOptions{})
	if strings.Contains(stdout.String(), "[y/N]") {
		t.Errorf("prompt written to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[y/N]") {
		t.Errorf("prompt missing from stderr: %q", stderr.String())
	}
}

func TestDestroy_InterruptAtPrompt(t *testing.T) {
	e, _ := destroyEngineWithVM(t)
	pr, pw := io.Pipe() // never written: the user does not answer
	defer pw.Close()
	withPromptStdin(t, pr, true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Ctrl-C while the prompt waits

	err := e.Destroy(ctx, DestroyOptions{})
	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	assertVMStillInState(t, e)
}
