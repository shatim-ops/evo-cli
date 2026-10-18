// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deploy

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// fakeSession stands in for *ssh.Session. Start writes the scripted output in
// chunks; Wait returns exitErr, or blocks until Close when follow is set (as
// `docker compose logs -f` never exits on its own).
type fakeSession struct {
	stdout, stderr io.Writer
	chunks         []string
	errChunk       string
	follow         bool
	exitErr        error

	mu      sync.Mutex
	cmd     string
	signals []ssh.Signal
	closed  chan struct{}
	once    sync.Once
}

func newFakeSession() *fakeSession { return &fakeSession{closed: make(chan struct{})} }

func (f *fakeSession) Start(cmd string) error {
	f.mu.Lock()
	f.cmd = cmd
	f.mu.Unlock()
	for _, c := range f.chunks {
		_, _ = io.WriteString(f.stdout, c)
	}
	if f.errChunk != "" {
		_, _ = io.WriteString(f.stderr, f.errChunk)
	}
	return nil
}

func (f *fakeSession) Wait() error {
	if f.follow {
		<-f.closed
		return io.EOF
	}
	return f.exitErr
}

func (f *fakeSession) Signal(sig ssh.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, sig)
	return nil
}

func (f *fakeSession) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

func deployerWith(f *fakeSession) *SSHDeployer {
	return &SSHDeployer{newStreamSession: func(stdout, stderr io.Writer) (streamSession, error) {
		f.stdout, f.stderr = stdout, stderr
		return f, nil
	}}
}

func TestExecStream_CopiesStdoutAndStderr(t *testing.T) {
	f := newFakeSession()
	f.chunks = []string{"web-1 | line 1\n", "web-1 | line 2\n"}
	f.errChunk = "warning\n"
	var out, errOut bytes.Buffer

	if err := deployerWith(f).ExecStream(context.Background(), "docker compose logs", &out, &errOut); err != nil {
		t.Fatalf("ExecStream: %v", err)
	}
	if out.String() != "web-1 | line 1\nweb-1 | line 2\n" || errOut.String() != "warning\n" {
		t.Errorf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if f.cmd != "docker compose logs" {
		t.Errorf("cmd = %q", f.cmd)
	}
}

func TestExecStream_RemoteFailureIsError(t *testing.T) {
	f := newFakeSession()
	exitErr := stderrors.New("Process exited with status 1")
	f.exitErr = exitErr

	err := deployerWith(f).ExecStream(context.Background(), "false", io.Discard, io.Discard)
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || !stderrors.Is(err, exitErr) {
		t.Fatalf("err = %v, want CLIError wrapping the exit status", err)
	}
}

// A follow stream outlives the old 120 s Exec limit and ends only with ctx:
// the remote command gets SIGTERM, the session is closed, and the error
// wraps context.Canceled so the CLI exits 130.
func TestExecStream_FollowEndsOnCancel(t *testing.T) {
	f := newFakeSession()
	f.follow = true
	f.chunks = []string{"web-1 | started\n"}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- deployerWith(f).ExecStream(ctx, "docker compose logs -f", &out, io.Discard) }()

	select {
	case err := <-done:
		t.Fatalf("returned before cancel: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()

	select {
	case err := <-done:
		if !stderrors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ExecStream did not stop after cancel")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.signals) != 1 || f.signals[0] != ssh.SIGTERM {
		t.Errorf("signals = %v, want [TERM]", f.signals)
	}
	if out.String() != "web-1 | started\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestExecStream_WithoutConnect(t *testing.T) {
	err := NewSSHDeployer().ExecStream(context.Background(), "true", io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("err = %v, want 'not connected'", err)
	}
}
