// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deploy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// testSSHServer is a real SSH server on 127.0.0.1: the client side of
// SSHDeployer (dial, host key check, auth, exec, stdin) runs unmodified.
// "exec" requests are answered by handler, not by a shell.
type testSSHServer struct {
	addr    string
	hostKey ssh.Signer
	ln      net.Listener

	mu       sync.Mutex
	commands []string
	stdins   [][]byte
}

// execHandler gets the command and its stdin and returns output and exit code.
type execHandler func(cmd string, stdin []byte) (out string, code uint32)

func newTestSSHServer(t *testing.T, clientKey ssh.PublicKey, handler execHandler) *testSSHServer {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(k.Marshal(), clientKey.Marshal()) {
				return nil, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &testSSHServer{addr: ln.Addr().String(), hostKey: hostSigner, ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c, cfg, handler)
		}
	}()
	return s
}

func (s *testSSHServer) serve(c net.Conn, cfg *ssh.ServerConfig, handler execHandler) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		_ = c.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range chReqs {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				var p struct{ Cmd string }
				_ = ssh.Unmarshal(req.Payload, &p)
				_ = req.Reply(true, nil)
				stdin, _ := io.ReadAll(ch) // until the client sends EOF
				s.mu.Lock()
				s.commands = append(s.commands, p.Cmd)
				s.stdins = append(s.stdins, stdin)
				s.mu.Unlock()
				out, code := handler(p.Cmd, stdin)
				_, _ = io.WriteString(ch, out)
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, code)
				_, _ = ch.SendRequest("exit-status", false, status)
				return
			}
		}()
	}
}

func (s *testSSHServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatal(err)
	}
	var p int
	for _, r := range port {
		p = p*10 + int(r-'0')
	}
	return host, p
}

// clientKeyPair returns a PEM private key and its public key.
func clientKeyPair(t *testing.T) ([]byte, ssh.PublicKey) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), signer.PublicKey()
}

func okHandler(string, []byte) (string, uint32) { return "", 0 }

func codeOf(err error) string {
	var ce *clerrors.CLIError
	if errorAs(err, &ce) {
		return ce.Code
	}
	return ""
}

// A changed host key must be reported as such: it is the MITM signal, and
// "check your SSH key and username" sends the user the wrong way.
func TestConnect_HostKeyChanged_IsHostKeyError(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	srv := newTestSSHServer(t, pub, okHandler)
	host, port := srv.hostPort(t)

	d := NewSSHDeployer()
	err := d.Connect(context.Background(), host, port, "user1", keyPEM, "SHA256:not-the-real-host-key")
	if err == nil {
		t.Fatal("connect with a wrong known host key succeeded")
	}
	if got := codeOf(err); got != clerrors.CodeSSHHostKeyChanged {
		t.Fatalf("code = %q, want %q (err: %v)", got, clerrors.CodeSSHHostKeyChanged, err)
	}
}

// TOFU: the first connect records the host key, the next one with it passes.
func TestConnect_TOFUThenStrict(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	srv := newTestSSHServer(t, pub, okHandler)
	host, port := srv.hostPort(t)

	d := NewSSHDeployer()
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, ""); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	fp := d.HostKeyFingerprint()
	_ = d.Close()
	if fp != ssh.FingerprintSHA256(srv.hostKey.PublicKey()) {
		t.Fatalf("recorded fingerprint %q is not the server's", fp)
	}
	if want := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(srv.hostKey.PublicKey()))); d.HostKeyLine() != want {
		t.Fatalf("HostKeyLine = %q, want %q", d.HostKeyLine(), want)
	}
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, fp); err != nil {
		t.Fatalf("second connect with the recorded key: %v", err)
	}
	_ = d.Close()
}

func TestConnect_WrongClientKey_IsAuthFailed(t *testing.T) {
	keyPEM, _ := clientKeyPair(t)
	_, otherPub := clientKeyPair(t)
	srv := newTestSSHServer(t, otherPub, okHandler)
	host, port := srv.hostPort(t)

	err := NewSSHDeployer().Connect(context.Background(), host, port, "user1", keyPEM, "")
	if got := codeOf(err); got != clerrors.CodeSSHAuthFailed {
		t.Fatalf("code = %q, want %q (err: %v)", got, clerrors.CodeSSHAuthFailed, err)
	}
}

// A booting VM's sshd accepts TCP and drops it mid-handshake. That is "not
// ready yet" (retried by deploy), not an authentication failure (fatal).
func TestConnect_DroppedDuringHandshake_IsRetryable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))
			time.Sleep(10 * time.Millisecond)
			_ = c.Close()
		}
	}()
	keyPEM, _ := clientKeyPair(t)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	err = NewSSHDeployer().Connect(context.Background(), host, port, "user1", keyPEM, "")
	if err == nil {
		t.Fatal("connect succeeded")
	}
	if got := codeOf(err); got != clerrors.CodeSSHTimeout {
		t.Fatalf("code = %q, want %q (retryable); err: %v", got, clerrors.CodeSSHTimeout, err)
	}
}

// The file travels on stdin: as a command argument it is limited by the
// remote execve (128 KiB per argument on Linux) and visible in ps.
func TestUpload_ContentOnStdinNotInCommand(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	srv := newTestSSHServer(t, pub, okHandler)
	host, port := srv.hostPort(t)

	d := NewSSHDeployer()
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, ""); err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	content := []byte(strings.Repeat("POSTGRES_PASSWORD: s3cret\n", 10000)) // ~260 KB
	if err := d.Upload(context.Background(), []FileEntry{{RemotePath: "/opt/app/docker-compose.yml", Content: content}}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(srv.commands))
	}
	cmd := srv.commands[0]
	if strings.Contains(cmd, "s3cret") || len(cmd) > 1024 {
		t.Fatalf("file content is in the command line (%d bytes)", len(cmd))
	}
	if !bytes.Equal(srv.stdins[0], content) {
		t.Fatalf("stdin = %d bytes, want the %d-byte file", len(srv.stdins[0]), len(content))
	}
	if !strings.Contains(cmd, "'/opt/app/docker-compose.yml'") {
		t.Fatalf("remote path not quoted: %s", cmd)
	}
}

// A path with shell metacharacters stays one word on the remote side.
func TestUpload_PathIsQuoted(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	srv := newTestSSHServer(t, pub, okHandler)
	host, port := srv.hostPort(t)
	d := NewSSHDeployer()
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, ""); err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	evil := "/opt/app/x'; touch /tmp/pwned; echo '.yml"
	if err := d.Upload(context.Background(), []FileEntry{{RemotePath: evil, Content: []byte("a")}}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.Contains(srv.commands[0], shellQuote(evil)) {
		t.Fatalf("path not shell-quoted: %s", srv.commands[0])
	}
}

// freezingProxy forwards TCP until frozen, then silently drops everything
// in both directions: a NAT entry or Wi-Fi gone without a RST.
type freezingProxy struct {
	addr   string
	frozen atomic.Bool
}

func newFreezingProxy(t *testing.T, target string) *freezingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := &freezingProxy{addr: ln.Addr().String()}
	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			if !p.frozen.Load() {
				if _, err := dst.Write(buf[:n]); err != nil {
					return
				}
			}
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("tcp", target)
			if err != nil {
				_ = c.Close()
				continue
			}
			t.Cleanup(func() { _ = c.Close(); _ = s.Close() })
			go pipe(s, c)
			go pipe(c, s)
		}
	}()
	return p
}

// A connection that dies without a RST left ExecStream (docker compose up,
// logs -f) blocked in Wait for hours: the client sent nothing to notice.
// Keepalives close it after a few unanswered intervals.
func TestExecStream_DeadConnectionEndsByKeepalive(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	release := make(chan struct{})
	defer close(release)
	srv := newTestSSHServer(t, pub, func(string, []byte) (string, uint32) {
		<-release // a long docker compose up
		return "", 0
	})
	proxy := newFreezingProxy(t, srv.addr)
	host, portStr, _ := net.SplitHostPort(proxy.addr)
	var port int
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	d := NewSSHDeployer()
	d.keepAliveInterval = 50 * time.Millisecond
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, ""); err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	done := make(chan error, 1)
	go func() { done <- d.ExecStream(context.Background(), "docker compose up -d", io.Discard, io.Discard) }()
	time.Sleep(250 * time.Millisecond) // keepalives answered: still running
	select {
	case err := <-done:
		t.Fatalf("ExecStream ended on a live connection: %v", err)
	default:
	}
	proxy.frozen.Store(true)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ExecStream on a dead connection succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ExecStream still blocked on a dead connection")
	}
}

func TestUpload_RemoteFailureIsError(t *testing.T) {
	keyPEM, pub := clientKeyPair(t)
	srv := newTestSSHServer(t, pub, func(string, []byte) (string, uint32) { return "No space left on device", 1 })
	host, port := srv.hostPort(t)
	d := NewSSHDeployer()
	if err := d.Connect(context.Background(), host, port, "user1", keyPEM, ""); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	err := d.Upload(context.Background(), []FileEntry{{RemotePath: "/opt/app/f", Content: []byte("a")}})
	if err == nil {
		t.Fatal("upload with a failing remote command succeeded")
	}
}
