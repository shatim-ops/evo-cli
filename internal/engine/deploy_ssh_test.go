// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/bootstrap"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// timeoutErr is a dial that got no answer, as net.Dialer reports it.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "dial tcp 192.0.2.10:22: i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// dialErr wraps cause the way deploy.SSHDeployer.Connect does.
func dialErr(cause error) error {
	return clerrors.Wrap(clerrors.CodeSSHTimeout, "SSH connection failed", "Check host, port, and network connectivity.", cause)
}

// SSH that never comes up (register OPS-1): the error says what port 22
// did — no answer, refused, dropped — and what the user can do; evo itself
// neither reboots nor recreates the VM.
func TestWaitForSSH_DiagnosisByLastError(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		err        error
	}{
		{"timeout", "Diagnosis: port 22 of 192.0.2.10 does not answer at all", dialErr(timeoutErr{})},
		{"refused", "nothing listens on port 22 (connection refused)", dialErr(stderrors.New("dial tcp 192.0.2.10:22: connect: connection refused"))},
		{"refused on Windows", "nothing listens on port 22", dialErr(stderrors.New("dial tcp 192.0.2.10:22: connectex: No connection could be made because the target machine actively refused it."))},
		{"handshake EOF", "accepts connections and drops them before login", dialErr(stderrors.New("ssh: handshake failed: EOF"))},
		{"unknown", "", dialErr(stderrors.New("something else"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dep := newMockDeployer()
			dep.connectErr = tc.err
			eng, _ := newTestEngine(t, &mockCloudAPI{}, dep)

			err := eng.waitForSSH(context.Background(), "192.0.2.10", "user1", "", 3)
			var ce *clerrors.CLIError
			if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeSSHTimeout || !strings.Contains(ce.Message, "after 3 attempts") {
				t.Fatalf("err = %v, want SSH_TIMEOUT after 3 attempts", err)
			}
			if dep.connectCalls != 3 {
				t.Errorf("connect calls = %d, want 3", dep.connectCalls)
			}
			if tc.want == "" {
				if strings.Contains(ce.Hint, "Diagnosis:") {
					t.Errorf("a diagnosis from an unknown error:\n%s", ce.Hint)
				}
			} else if !strings.HasPrefix(ce.Hint, "Diagnosis:") || !strings.Contains(ce.Hint, tc.want) {
				t.Errorf("hint lacks %q:\n%s", tc.want, ce.Hint)
			}
			for _, want := range []string{"run 'evo deploy' again", "reboot the VM in the Cloud.ru console", "evo has no reboot command", "serial console", "'evo destroy'"} {
				if !strings.Contains(ce.Hint, want) {
					t.Errorf("hint lacks %q:\n%s", want, ce.Hint)
				}
			}
		})
	}
}

// Past Timings.SSHSlowNote deploy says once why it still waits.
func TestWaitForSSH_SlowNoteOnce(t *testing.T) {
	dep := newMockDeployer()
	dep.connectErr = dialErr(timeoutErr{})
	var out bytes.Buffer
	eng, _ := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	eng.Timings.SSHSlowNote = time.Nanosecond

	_ = eng.waitForSSH(context.Background(), "192.0.2.10", "user1", "", 5)
	if n := strings.Count(out.String(), "SSH on 192.0.2.10 does not answer yet"); n != 1 {
		t.Errorf("slow note printed %d times, want once:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "evo keeps trying (5 attempts in all)") {
		t.Errorf("note:\n%s", out.String())
	}
}

// A re-run on a VM that SSH never reached (an earlier ssh_timeout) waits
// longer than on a VM that answered before: it may be booting again after
// a reboot from the console.
func TestDeploy_SSHRetriesOnExistingVM(t *testing.T) {
	for _, tc := range []struct {
		name, hostKey string
		want          int
	}{
		{"never reached", "", sshMaxRetries / 2},
		{"answered before", "SHA256:known", 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dep := newMockDeployer()
			dep.connectErr = dialErr(timeoutErr{})
			eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
			sf := &state.File{Version: 1}
			sf.UpsertDeployment(state.DeploymentRecord{
				ID: "dep-1", VMID: "vm-1", VMIP: "192.0.2.10", SSHUser: "user1", Status: "ssh_timeout",
				SSHHostKeyFingerprint: tc.hostKey, ComposeHash: "old",
			})
			if err := state.Write(state.DefaultPath(dir), sf); err != nil {
				t.Fatal(err)
			}
			err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
			if clerrors.Reason(err) != clerrors.CodeSSHTimeout {
				t.Fatalf("err = %v, want SSH_TIMEOUT", err)
			}
			if dep.connectCalls != tc.want {
				t.Errorf("connect attempts = %d, want %d", dep.connectCalls, tc.want)
			}
		})
	}
}

// The install's own lines name the cause: both mirrors failing is the VM's
// way out, Docker's repository failing is not the image's mirror.
func TestInstallDiagnosis_FallbackAndDockerRepo(t *testing.T) {
	for _, tc := range []struct{ out, want string }{
		{"W: Failed to fetch https://repo.mirror.example.test/ubuntu/dists/jammy/InRelease\n" +
			bootstrap.FallbackFailedLine + " (https://archive.ubuntu.com/ubuntu):\nW: Failed to fetch https://archive.ubuntu.com/ubuntu/dists/jammy/InRelease",
			"Diagnosis: apt failed both with the image's mirror and with the official mirror"},
		{bootstrap.DockerRepoFailedLine + "\nW: Failed to fetch https://download.docker.com/linux/ubuntu/dists/jammy/InRelease",
			"Diagnosis: apt failed on Docker's repository download.docker.com"},
		{"E: Failed to fetch https://repo.mirror.example.test/ubuntu/pool/main/x.deb  Connection timed out",
			"Diagnosis: apt cannot download packages from the image's mirror"},
		// no DNS explains every failed fetch: it comes first
		{"W: Failed to fetch https://repo.mirror.example.test/  Temporary failure resolving 'repo.mirror.example.test'\n" +
			"Temporary failure in name resolution\n" + bootstrap.FallbackFailedLine, "Diagnosis: the VM does not resolve names"},
		// break r4: R4-15, R4-17
		{"W: Failed to fetch https://repo.mirror.example.test/ubuntu/dists/jammy/InRelease\n" +
			bootstrap.NoKeyringLine + " (https://archive.ubuntu.com/ubuntu), and the image's apt mirror does not work",
			"Diagnosis: the image's apt mirror does not work, and the VM has no keyring"},
		{bootstrap.UnsupportedOSLine + " (CentOS, RHEL, Fedora, Rocky Linux, AlmaLinux), not on alpine",
			"Diagnosis: the VM's OS is neither Ubuntu nor Debian nor an RPM-based distribution"},
	} {
		if got := installDiagnosis(tc.out); !strings.HasPrefix(got, tc.want) {
			t.Errorf("installDiagnosis(%q) = %q, want prefix %q", tc.out, got, tc.want)
		}
	}
}
