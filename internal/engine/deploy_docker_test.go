// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/bootstrap"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func newDeployOpts(t *testing.T, dir string) DeployOptions {
	t.Helper()
	return DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", NonInteractive: true, Yes: true}
}

func readActive(t *testing.T, dir string) *state.DeploymentRecord {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return sf.ActiveDeployment()
}

// A fresh VM from init's Ubuntu image has no Docker: deploy installs it over
// SSH as the image's login user, then reconnects so the docker group applies.
// The disk type comes from list_disk_types and the FIP ID lands in state.
func TestDeploy_NewVMGetsDockerDiskTypeAndFIPInState(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, cloud, dep)

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	install := bootstrap.InstallCommand("user1")
	dep.mu.Lock()
	calls := append([]string(nil), dep.execCalls...)
	connects := dep.connectCalls
	dep.mu.Unlock()
	idx := func(cmd string) int {
		for i, c := range calls {
			if c == cmd {
				return i
			}
		}
		return -1
	}
	iInstall, iReady, iUp := idx(install), idx(dockerReadyCmd), idx("cd /opt/app && docker compose -f docker-compose.yml up -d")
	if iInstall < 0 || iReady < iInstall || iUp < iReady {
		t.Errorf("want install → ready check → compose up, got %v", calls)
	}
	if connects < 2 {
		t.Errorf("connects = %d, want a reconnect after the install", connects)
	}

	cloud.mu.Lock()
	req := cloud.createReq
	cloud.mu.Unlock()
	if len(req.Disks) != 1 || req.Disks[0].DiskTypeName != "SSD" {
		t.Errorf("disks = %+v, want disk type SSD by name from list_disk_types", req.Disks)
	}
	if req.ImageMetadata["name"] != "user1" {
		t.Errorf("image metadata = %v", req.ImageMetadata)
	}

	a := readActive(t, dir)
	if a == nil || a.FloatingIPID != "fip-1" || a.SSHUser != "user1" || a.Status != "running" {
		t.Errorf("state = %+v, want floating_ip_id fip-1, ssh_user user1, running", a)
	}
}

// While first-boot cloud-init holds apt, the install fails; deploy retries
// with Timings.DockerPollInterval instead of giving up.
func TestDeploy_DockerInstallRetries(t *testing.T) {
	dep := newMockDeployer()
	install := bootstrap.InstallCommand("user1")
	busy := stderrors.New("Could not get lock /var/lib/dpkg/lock-frontend")
	dep.streamErrs = map[string][]error{install: {busy, busy}}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	n := 0
	for _, c := range dep.execCalls {
		if c == install {
			n++
		}
	}
	if n != 3 {
		t.Errorf("install attempts = %d, want 3", n)
	}
	if !strings.Contains(out.String(), "Docker not ready yet (attempt 2") {
		t.Errorf("no retry progress:\n%s", out.String())
	}
}

// Docker that never becomes ready ends with an error after
// DockerWaitTimeout, and the record is marked so a plain re-run retries.
func TestDeploy_DockerTimeoutMarksStateAndRerunRetries(t *testing.T) {
	dep := newMockDeployer()
	dep.streamHook = func(_ context.Context, cmd string) error {
		if strings.Contains(cmd, "base64 -d") {
			return stderrors.New("curl: (6) Could not resolve host: get.docker.com")
		}
		return nil
	}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Timings.DockerWaitTimeout = 20 * time.Millisecond
	opts := newDeployOpts(t, dir)

	err := eng.Deploy(context.Background(), opts)
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeSSHTimeout || !strings.Contains(cliErr.Message, "Docker is not ready") {
		t.Fatalf("err = %v, want SSH_TIMEOUT 'Docker is not ready'", err)
	}
	// The install printed nothing: the hint names how the last attempt
	// ended, or there is nothing to diagnose (QA r4 Q14, run #24).
	if !strings.Contains(cliErr.Hint, "Last attempt ended with: curl: (6) Could not resolve host") {
		t.Errorf("hint lacks the last attempt's error:\n%s", cliErr.Hint)
	}
	if a := readActive(t, dir); a == nil || a.Status != "docker_failed" {
		t.Fatalf("state = %+v, want status docker_failed", a)
	}

	// Same compose file, no --force: must not answer "No changes".
	dep.streamHook = nil
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if a := readActive(t, dir); a.Status != "running" {
		t.Errorf("re-run status = %q, want running", a.Status)
	}
}

// The last attempt is cut by the deadline with a line or two printed; the
// apt error of an earlier attempt that failed on its own goes into the
// warning and the hint, not lost (cloud-e2e 16.10.2026).
func TestDeploy_DockerTimeoutKeepsLastFailedAttemptOutput(t *testing.T) {
	dep := newMockDeployer()
	n := 0
	dep.streamOut = func(cmd string) string {
		if !strings.Contains(cmd, "base64 -d") {
			return ""
		}
		n++
		if n == 1 {
			return "# Executing docker install script\nE: Failed to fetch http://mirror.example.test/ubuntu/pool/x.deb  Connection timed out\n"
		}
		return "# Executing docker install script\n"
	}
	dep.streamHook = func(ctx context.Context, cmd string) error {
		if !strings.Contains(cmd, "base64 -d") {
			return nil
		}
		if n == 1 {
			return stderrors.New("Process exited with status 100")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	eng.Timings.DockerWaitTimeout = 300 * time.Millisecond
	eng.Timings.DockerPollInterval = time.Millisecond

	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeSSHTimeout {
		t.Fatalf("err = %v, want SSH_TIMEOUT", err)
	}
	if !strings.Contains(cliErr.Hint, "Output of the last attempt that failed on its own") || !strings.Contains(cliErr.Hint, "E: Failed to fetch") {
		t.Errorf("hint lacks the failed attempt's apt error:\n%s", cliErr.Hint)
	}
	if !strings.Contains(cliErr.Hint, "Diagnosis: apt cannot download packages") {
		t.Errorf("hint lacks the diagnosis:\n%s", cliErr.Hint)
	}
	if !strings.Contains(out.String(), "Last line of the install: E: Failed to fetch") {
		t.Errorf("warning lacks the attempt's last line:\n%s", out.String())
	}
}

func TestDeploy_DiskTypeMatchedByNameCaseInsensitive(t *testing.T) {
	cloud := &mockCloudAPI{listDiskTypesFn: func(context.Context, string) ([]*provision.DiskType, error) {
		return []*provision.DiskType{{ID: "hdd-id", Name: "HDD"}, {ID: "ssd-id", Name: "ssd"}}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if got := cloud.createReq.Disks[0].DiskTypeName; got != "ssd" {
		t.Errorf("disk type = %q, want ssd as listed by the API", got)
	}
}

func TestDeploy_NoSSDDiskTypeFailsBeforeCreatingVM(t *testing.T) {
	cloud := &mockCloudAPI{listDiskTypesFn: func(context.Context, string) ([]*provision.DiskType, error) {
		return []*provision.DiskType{{ID: "hdd-id", Name: "HDD"}}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeConfigInvalid || !strings.Contains(cliErr.Hint, "HDD") {
		t.Fatalf("err = %v, want CONFIG_INVALID listing available types", err)
	}
	if cloud.createCalled {
		t.Error("VM created despite the missing disk type")
	}
}

func assertInterruptedWithVMHint(t *testing.T, err error, out string) {
	t.Helper()
	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || !strings.Contains(cliErr.Hint, "vm-123") || !strings.Contains(cliErr.Hint, "evo destroy --yes") {
		t.Errorf("hint must name the VM and how to delete it: %v", err)
	}
	if !strings.Contains(out, "VM vm-123 left running") {
		t.Errorf("no warning that the VM is still running:\n%s", out)
	}
}

// Ctrl-C after the VM became active (here: while the floating IP is being
// created) still says which VM is left running and how to delete it.
func TestDeploy_InterruptDuringFIP_HintsVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &mockCloudAPI{createFloatingIPFn: func(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		cancel()
		return nil, context.Canceled
	}}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	err := eng.Deploy(ctx, newDeployOpts(t, dir))
	assertInterruptedWithVMHint(t, err, out.String())
	if a := readActive(t, dir); a == nil || a.VMID != "vm-123" {
		t.Errorf("VM not recorded in state: %+v", a)
	}
}

// The same during the Docker install over SSH.
func TestDeploy_InterruptDuringDockerInstall_HintsVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dep := newMockDeployer()
	dep.streamHook = func(ctx context.Context, cmd string) error {
		cancel()
		return clerrors.Wrap(clerrors.CodeSSHTimeout, "command execution cancelled", "", context.Canceled)
	}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	assertInterruptedWithVMHint(t, eng.Deploy(ctx, newDeployOpts(t, dir)), out.String())
}

// A redeploy to an existing VM is not wrapped: nothing new was created.
func TestDeploy_InterruptOnExistingVM_NotWrapped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dep := newMockDeployer()
	dep.streamHook = func(context.Context, string) error { cancel(); return context.Canceled }
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-old", VMID: "vm-old", VMIP: "127.0.0.1", SSHUser: "user1", Status: "running", ComposeHash: "other"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}

	err := eng.Deploy(ctx, newDeployOpts(t, dir))
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var cliErr *clerrors.CLIError
	if stderrors.As(err, &cliErr) && strings.Contains(cliErr.Hint, "destroy --yes") {
		t.Errorf("existing VM must not get the 'left running' hint: %v", err)
	}
}

// The floating-IP wait prints progress instead of staying silent for
// minutes.
func TestAssignFloatingIP_ReportsProgress(t *testing.T) {
	var polls atomic.Int32
	cloud := &mockCloudAPI{getVMDetailFn: func(_ context.Context, id string) (*provision.VMDetail, error) {
		polls.Add(1)
		return &provision.VMDetail{ID: id, Interfaces: []provision.VMInterface{{ID: "iface-1", Primary: true, State: "creating"}}}, nil
	}}
	var msgs []string
	ip, fipID, err := assignFloatingIP(context.Background(), cloud, fastTimings,
		func(m string) { msgs = append(msgs, m) }, "vm-1", "proj", "az")
	if err != nil || ip != "127.0.0.1" || fipID != "fip-1" {
		t.Fatalf("ip=%q fip=%q err=%v", ip, fipID, err)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"Waiting for the VM network interface", "Still waiting for the network interface", "Creating floating IP"} {
		if !strings.Contains(all, want) {
			t.Errorf("progress lacks %q:\n%s", want, all)
		}
	}
}

// Cancelling the floating-IP wait returns at once with ctx's error and the
// FIP ID, so the caller can record the FIP for destroy.
func TestAssignFloatingIP_CancelKeepsFIPID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cloud := &mockCloudAPI{createFloatingIPFn: func(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		cancel()
		return &provision.FloatingIP{ID: "fip-7", IPAddress: "198.51.100.7", State: "creating"}, nil
	}}
	slow := fastTimings
	slow.FIPPollInterval = time.Hour

	ip, fipID, err := assignFloatingIP(ctx, cloud, slow, nil, "vm-1", "proj", "az")
	if !stderrors.Is(err, context.Canceled) || fipID != "fip-7" || ip != "198.51.100.7" {
		t.Errorf("ip=%q fip=%q err=%v, want fip-7 and context.Canceled", ip, fipID, err)
	}
}

func TestEnsureSSHKey_LoadsOnceAndOnlyWhenMissing(t *testing.T) {
	calls := 0
	e := &Engine{SSHKeyLoader: func() ([]byte, string, error) {
		calls++
		return []byte("pem"), "ssh-ed25519 AAA", nil
	}}
	for i := 0; i < 2; i++ {
		if err := e.EnsureSSHKey(); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || string(e.SSHKeyPEM) != "pem" || e.SSHPubKey != "ssh-ed25519 AAA" {
		t.Errorf("calls=%d key=%q pub=%q", calls, e.SSHKeyPEM, e.SSHPubKey)
	}

	failing := &Engine{SSHKeyLoader: func() ([]byte, string, error) { return nil, "", stderrors.New("disk full") }}
	if err := failing.EnsureSSHKey(); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v", err)
	}
}

// A refusal the next attempt would repeat (an unsupported OS, no keyring
// for the official mirror) ends the install at once, with the diagnosis,
// instead of retrying until the deadline (break r4, R4-15, R4-17).
func TestDeploy_DockerPermanentRefusalNotRetried(t *testing.T) {
	for _, line := range []string{
		bootstrap.UnsupportedOSLine + " (CentOS, RHEL, Fedora, Rocky Linux, AlmaLinux), not on alpine",
		bootstrap.NoKeyringLine + " (https://archive.ubuntu.com/ubuntu), and the image's apt mirror does not work",
	} {
		dep := newMockDeployer()
		var attempts atomic.Int32
		dep.streamOut = func(cmd string) string {
			if !strings.Contains(cmd, "base64 -d") {
				return ""
			}
			attempts.Add(1)
			return line + "\n"
		}
		dep.streamHook = func(_ context.Context, cmd string) error {
			if strings.Contains(cmd, "base64 -d") {
				return stderrors.New("Process exited with status 1")
			}
			return nil
		}
		eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
		eng.Timings.DockerWaitTimeout = time.Minute
		eng.Timings.DockerPollInterval = time.Millisecond

		start := time.Now()
		err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
		var cliErr *clerrors.CLIError
		if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeConfigInvalid {
			t.Fatalf("err = %v, want CONFIG_INVALID", err)
		}
		if n := attempts.Load(); n != 1 || time.Since(start) > 30*time.Second {
			t.Errorf("%d install attempts in %s, want one", n, time.Since(start))
		}
		if !strings.Contains(cliErr.Message, "Docker cannot be installed on VM") ||
			!strings.Contains(cliErr.Hint, "Diagnosis:") || !strings.Contains(cliErr.Hint, line) {
			t.Errorf("message %q, hint:\n%s", cliErr.Message, cliErr.Hint)
		}
	}
}

// A successful install shows what it did on the way: the switch to the
// official apt mirror was invisible (cloud-e2e 20.10, register v2 IMG-6).
func TestInstallNotes(t *testing.T) {
	out := "Hit:1 http://x jammy InRelease\nevo: switching apt to the official mirror for this install: https://archive.ubuntu.com/ubuntu (the image's apt sources stay as they are)\n" +
		"Reading package lists...\n  evo: re-running the image's apt mirror setup (cloud-init bootcmd)\n"
	got := installNotes(out)
	if len(got) != 2 || !strings.HasPrefix(got[0], "evo: switching apt") || !strings.HasPrefix(got[1], "evo: re-running") {
		t.Errorf("installNotes = %q", got)
	}
	if n := installNotes(strings.Repeat("evo: x\n", 9)); len(n) != 5 {
		t.Errorf("%d notes, want at most 5", len(n))
	}
}
