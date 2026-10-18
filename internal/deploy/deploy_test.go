// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deploy

import (
	"context"
	"errors"
	"io"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// ---------------------------------------------------------------------------
// MockDeployer
// ---------------------------------------------------------------------------

// MockDeployer records calls for testing.
type MockDeployer struct {
	ConnectCalls []connectCall
	UploadCalls  [][]FileEntry
	ExecCalls    []string
	ExecResults  []string
	ExecErrors   []error
	CloseCalled  bool
	ConnectError error
	UploadError  error
	HostKey      string
	execIndex    int
}

type connectCall struct {
	Host, User, KnownHostKey string
	Port                     int
}

// Compile-time check that MockDeployer implements Deployer.
var _ Deployer = (*MockDeployer)(nil)

func (m *MockDeployer) Connect(_ context.Context, host string, port int, user string, _ []byte, knownHostKey string) error {
	m.ConnectCalls = append(m.ConnectCalls, connectCall{
		Host:         host,
		User:         user,
		KnownHostKey: knownHostKey,
		Port:         port,
	})
	return m.ConnectError
}

func (m *MockDeployer) HostKeyFingerprint() string {
	return m.HostKey
}

func (m *MockDeployer) Upload(_ context.Context, files []FileEntry) error {
	m.UploadCalls = append(m.UploadCalls, files)
	return m.UploadError
}

func (m *MockDeployer) Exec(_ context.Context, cmd string) (string, error) {
	m.ExecCalls = append(m.ExecCalls, cmd)
	idx := m.execIndex
	m.execIndex++

	var result string
	if idx < len(m.ExecResults) {
		result = m.ExecResults[idx]
	}

	var err error
	if idx < len(m.ExecErrors) {
		err = m.ExecErrors[idx]
	}

	return result, err
}

func (m *MockDeployer) ExecStream(ctx context.Context, cmd string, stdout, _ io.Writer) error {
	out, err := m.Exec(ctx, cmd)
	_, _ = io.WriteString(stdout, out)
	return err
}

func (m *MockDeployer) Close() error {
	m.CloseCalled = true
	return nil
}

// ---------------------------------------------------------------------------
// MockDeployer tests
// ---------------------------------------------------------------------------

func TestMockDeployer_Connect(t *testing.T) {
	m := &MockDeployer{}
	err := m.Connect(context.Background(), "192.168.1.1", 22, "root", nil, "SHA256:abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.ConnectCalls) != 1 {
		t.Fatalf("expected 1 connect call, got %d", len(m.ConnectCalls))
	}
	c := m.ConnectCalls[0]
	if c.Host != "192.168.1.1" || c.Port != 22 || c.User != "root" || c.KnownHostKey != "SHA256:abc" {
		t.Errorf("connect call mismatch: %+v", c)
	}
}

func TestMockDeployer_ConnectError(t *testing.T) {
	m := &MockDeployer{ConnectError: errors.New("connection refused")}
	err := m.Connect(context.Background(), "host", 22, "user", nil, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "connection refused" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestMockDeployer_Upload(t *testing.T) {
	m := &MockDeployer{}
	files := []FileEntry{
		{RemotePath: "/tmp/a.txt", Content: []byte("hello")},
		{RemotePath: "/tmp/b.txt", Content: []byte("world")},
	}
	err := m.Upload(context.Background(), files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.UploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(m.UploadCalls))
	}
	if len(m.UploadCalls[0]) != 2 {
		t.Errorf("expected 2 files, got %d", len(m.UploadCalls[0]))
	}
}

func TestMockDeployer_UploadError(t *testing.T) {
	m := &MockDeployer{UploadError: errors.New("disk full")}
	err := m.Upload(context.Background(), []FileEntry{{RemotePath: "/tmp/f", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMockDeployer_Exec(t *testing.T) {
	m := &MockDeployer{
		ExecResults: []string{"output1", "output2"},
		ExecErrors:  []error{nil, errors.New("fail")},
	}

	out, err := m.Exec(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "output1" {
		t.Errorf("expected output1, got %s", out)
	}
	if len(m.ExecCalls) != 1 || m.ExecCalls[0] != "echo hello" {
		t.Errorf("exec call not recorded correctly")
	}

	out, err = m.Exec(context.Background(), "bad command")
	if err == nil {
		t.Fatal("expected error on second call")
	}
	if out != "output2" {
		t.Errorf("expected output2, got %s", out)
	}
}

func TestMockDeployer_Close(t *testing.T) {
	m := &MockDeployer{}
	if m.CloseCalled {
		t.Fatal("CloseCalled should be false initially")
	}
	_ = m.Close()
	if !m.CloseCalled {
		t.Fatal("CloseCalled should be true after Close()")
	}
}

func TestMockDeployer_HostKeyFingerprint(t *testing.T) {
	m := &MockDeployer{HostKey: "SHA256:testkey"}
	if fp := m.HostKeyFingerprint(); fp != "SHA256:testkey" {
		t.Errorf("expected SHA256:testkey, got %s", fp)
	}
}

// ---------------------------------------------------------------------------
// SSHDeployer unit tests (no real SSH server needed)
// ---------------------------------------------------------------------------

func TestSSHDeployer_NewSSHDeployer(t *testing.T) {
	d := NewSSHDeployer()
	if d == nil {
		t.Fatal("NewSSHDeployer returned nil")
	}
	if d.connectTimeout != 10*1e9 { // 10 seconds in nanoseconds
		t.Errorf("unexpected default timeout: %v", d.connectTimeout)
	}
	if d.client != nil {
		t.Error("client should be nil before Connect")
	}
}

func TestSSHDeployer_ExecWithoutConnect(t *testing.T) {
	d := NewSSHDeployer()
	_, err := d.Exec(context.Background(), "echo hi")
	if err == nil {
		t.Fatal("expected error when calling Exec without Connect")
	}
	var cliErr *clerrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
}

func TestSSHDeployer_UploadWithoutConnect(t *testing.T) {
	d := NewSSHDeployer()
	err := d.Upload(context.Background(), []FileEntry{{RemotePath: "/tmp/x", Content: []byte("y")}})
	if err == nil {
		t.Fatal("expected error when calling Upload without Connect")
	}
}

func TestSSHDeployer_CloseWithoutConnect(t *testing.T) {
	d := NewSSHDeployer()
	err := d.Close()
	if err != nil {
		t.Fatalf("Close on unconnected deployer should not error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper function unit tests
// ---------------------------------------------------------------------------

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"simple", "'simple'"},
		{"with spaces", "'with spaces'"},
		{"it's", "'it'\"'\"'s'"},
		{"", "''"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParentDir(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/tmp/a/b.txt", "/tmp/a"},
		{"/tmp/file", "/tmp"},
		{"/file", "/"},
		{"file", "/"},
	}
	for _, tt := range tests {
		got := parentDir(tt.input)
		if got != tt.want {
			t.Errorf("parentDir(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
