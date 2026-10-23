// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

func faultIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
}

var faultTimings = Timings{
	VMPollInterval:        time.Millisecond,
	VMPollTimeout:         40 * time.Millisecond,
	VMProgressInterval:    time.Hour, // the progress ticker must not spin
	IfacePollInterval:     time.Millisecond,
	FIPRetryInterval:      time.Millisecond,
	FIPLookupInterval:     time.Millisecond,
	FIPPollInterval:       time.Millisecond,
	FIPSettleTimeout:      20 * time.Millisecond,
	SSHRetryInterval:      time.Millisecond,
	SettleDelay:           time.Millisecond,
	VerifyTimeout:         time.Millisecond,
	DockerPollInterval:    time.Millisecond,
	DockerWaitTimeout:     25 * time.Millisecond,
	DestroyPollInterval:   time.Millisecond,
	DestroyPollTimeout:    40 * time.Millisecond,
	DeletableWaitTimeout:  25 * time.Millisecond,
	DeletablePollInterval: time.Millisecond,
	StatusRefreshTimeout:  20 * time.Millisecond,
}

func faultEngine(t *testing.T, cloud *faultCloud, dep *faultDeployer) (*Engine, *bytes.Buffer, string) {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	eng := &Engine{
		Config: config.Config{
			ProjectID: faultProject,
			ImageID:   faultImage,
			DiskSize:  10,
			Flavor:    "low-1-1",
			Region:    faultZone,
		},
		Auth:      &mockTokenProvider{token: auth.Token{Value: "test-token", ExpiresAt: time.Now().Add(time.Hour)}},
		Cloud:     cloud,
		Deployer:  dep,
		Printer:   output.NewWithWriter(&buf, output.ModePlain, false),
		SSHKeyPEM: []byte("fake-key"),
		SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
		StateDir:  dir,
		Timings:   faultTimings,
		Probe: func(context.Context, string, []int, time.Duration) []verify.ProbeResult {
			return []verify.ProbeResult{{Port: 8080, StatusCode: 200, Healthy: true}}
		},
	}
	return eng, &buf, dir
}

func faultCompose(t *testing.T, dir string, ports int) string {
	t.Helper()
	if ports < 1 {
		ports = 2
	}
	var b strings.Builder
	b.WriteString("services:\n  web:\n    image: nginx:latest\n    ports:\n")
	for i := 0; i < ports; i++ {
		p := 8080 + i
		b.WriteString("      - \"")
		b.WriteString(itoa(p))
		b.WriteString(":")
		b.WriteString(itoa(p))
		b.WriteString("\"\n")
	}
	path := dir + "/docker-compose.yml"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func faultDeploy(t *testing.T, eng *Engine, dir string) error {
	t.Helper()
	return faultDeployCtx(t, eng, dir, context.Background(), 2)
}

func faultDeployCtx(t *testing.T, eng *Engine, dir string, ctx context.Context, ports int) error {
	t.Helper()
	compose := faultCompose(t, dir, ports)
	return eng.Deploy(ctx, DeployOptions{
		ComposeFile:    compose,
		Flavor:         "low-1-1",
		Region:         faultZone,
		Yes:            true,
		NonInteractive: true,
	})
}

func faultState(t *testing.T, dir string) *state.File {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return sf
}

// faultAccounted fails when a live VM or floating IP is neither recorded nor
// named in the error or the printer text, and when state is stuck in a
// status deploy cannot leave.
func faultAccounted(t *testing.T, cloud *faultCloud, dir string, err error, buf *bytes.Buffer) {
	t.Helper()
	sf := faultState(t, dir)
	raw := sfJSON(sf) + "\n" + buf.String()
	if err != nil {
		raw += "\n" + err.Error()
	}
	for _, vm := range cloud.liveVMs() {
		if !strings.Contains(raw, vm.ID) {
			t.Errorf("live VM %s is not in state and not named in the output", vm.ID)
		}
	}
	for _, fip := range cloud.liveFIPs() {
		if !strings.Contains(raw, fip.ID) {
			t.Errorf("live floating IP %s (%s) is not in state and not named in the output", fip.ID, fip.IP)
		}
	}
	for _, d := range sf.Deployments {
		if d.Status == "destroyed" {
			continue
		}
		if d.Status == "creating" && d.VMID == "" {
			t.Errorf("state stuck at creating with an empty VM id")
		}
		if d.Status == "destroying" && d.VMID == "" && d.FloatingIPID == "" {
			t.Errorf("state stuck at destroying with nothing to delete")
		}
	}
}

func sfJSON(sf *state.File) string {
	var b strings.Builder
	for _, d := range sf.Deployments {
		b.WriteString(d.ID)
		b.WriteByte(' ')
		b.WriteString(d.VMID)
		b.WriteByte(' ')
		b.WriteString(d.FloatingIPID)
		b.WriteByte(' ')
		b.WriteString(d.Status)
		b.WriteByte('\n')
	}
	return b.String()
}

func faultCreates(t *testing.T, cloud *faultCloud) (vms, fips int) {
	t.Helper()
	return cloud.creates()
}

func faultDestroy(t *testing.T, eng *Engine) error {
	t.Helper()
	return eng.Destroy(context.Background(), DestroyOptions{Force: true})
}

func faultErrText(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) && ce.Hint != "" {
		s += "\n" + ce.Hint
	}
	return s
}

func faultWantExit(t *testing.T, err error, code int, reason string) {
	t.Helper()
	if clerrors.ExitCode(err) != code || clerrors.Reason(err) != reason {
		t.Fatalf("exit %d reason %s, want %d %s (%v)", clerrors.ExitCode(err), clerrors.Reason(err), code, reason, err)
	}
}

func TestFault_Deploy_HappyPath(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	dep := &faultDeployer{}
	eng, buf, dir := faultEngine(t, cloud, dep)
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nlog=%v\nout=%s", err, cloud.callLog(), buf.String())
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("world vms=%d fips=%d log=%v", len(cloud.liveVMs()), len(cloud.liveFIPs()), cloud.callLog())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.Status != "running" || active.VMID == "" || active.FloatingIPID == "" {
		t.Fatalf("state %+v", active)
	}
	vms, fips := faultCreates(t, cloud)
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 || len(cloud.liveRules()) != 0 {
		t.Fatalf("after destroy vms=%v fips=%v rules=%d", cloud.liveVMs(), cloud.liveFIPs(), len(cloud.liveRules()))
	}
	if vms != 1 || fips != 1 {
		t.Fatalf("creates vm=%d fip=%d", vms, fips)
	}
	if faultState(t, dir).ActiveDeployment() != nil {
		t.Fatal("active record left after destroy")
	}
}

func TestFault_Deploy_ExitCodeMatchesReason(t *testing.T) {
	faultIsolate(t)
	// A 409 on create is CONFIG_INVALID, exit 2 (docs/exit-codes.md).
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultReject)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitAuth, clerrors.CodeConfigInvalid)
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("409 left vms=%v fips=%v out=%s", cloud.liveVMs(), cloud.liveFIPs(), buf.String())
	}
	faultAccounted(t, cloud, dir, err, buf)
}

func TestFault_Deploy_LostCreateVMIsRecorded(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultLost)
	err := faultDeploy(t, eng, dir)
	if err == nil {
		t.Fatal("lost create returned success")
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("world vms=%d fips=%d err=%v", len(cloud.liveVMs()), len(cloud.liveFIPs()), err)
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(err.Error(), id) {
		t.Fatalf("error does not name %s: %v", id, err)
	}
	faultWantExit(t, err, clerrors.ExitGeneral, clerrors.CodeVMProvisionFailed)
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID != id || active.FloatingIPID == "" {
		t.Fatalf("state %+v", active)
	}
	faultAccounted(t, cloud, dir, err, buf)
	vms, fips := cloud.creates()
	cloud.disarm()
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("retry: %v log=%v", err, cloud.callLog())
	}
	if v2, f2 := cloud.creates(); v2 != vms || f2 != fips {
		t.Fatalf("retry created vm %d→%d fip %d→%d", vms, v2, fips, f2)
	}
	if err := faultDestroy(t, eng); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("destroy left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Deploy_TransportBeforeCreateReleasesFIP(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultTransport)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitGeneral, clerrors.CodeAPIUnreachable)
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("transport left vms=%v fips=%v out=%s", cloud.liveVMs(), cloud.liveFIPs(), buf.String())
	}
	if faultState(t, dir).ActiveDeployment() != nil {
		t.Fatal("state recorded a VM that was not created")
	}
}

func TestFault_Deploy_LostBootFIPIsAdopted(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateFloatingIP", 1, faultLost)
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nout=%s", err, buf.String())
	}
	if vms, fips := cloud.creates(); fips != 1 || vms != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("creates vm=%d fip=%d live=%d", vms, fips, len(cloud.liveFIPs()))
	}
	if err := faultDestroy(t, eng); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveFIPs()) != 0 || len(cloud.liveVMs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Deploy_EmptyBootFIPIDIsAdoptedByName(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateFloatingIP", 1, faultEmpty)
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nout=%s", err, buf.String())
	}
	if _, fips := cloud.creates(); fips != 1 {
		t.Fatalf("empty id created %d floating IPs", fips)
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.FloatingIPID == "" || active.FloatingIPID != cloud.liveFIPs()[0].ID {
		t.Fatalf("state %+v fips %+v", active, cloud.liveFIPs())
	}
}

func TestFault_Deploy_FIPTransportAttachesAfterBoot(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateFloatingIP", 1, faultTransport)
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nout=%s", err, buf.String())
	}
	if vms, fips := cloud.creates(); vms != 1 || fips != 1 {
		t.Fatalf("creates vm=%d fip=%d (a lost pre-boot FIP must not be joined by a second)", vms, fips)
	}
	faultAccounted(t, cloud, dir, nil, buf)
}

func TestFault_Deploy_AuthOnCreateExitsAuth(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultAuth)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitAuth, clerrors.CodeAuthExpired)
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("401 left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
	faultAccounted(t, cloud, dir, err, buf)
}

func TestFault_Deploy_HTTP500OnCreateReleasesFIP(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultHTTP500)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitAPI, clerrors.CodeAPIError)
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("500 left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Deploy_EmptyStatusTimesOutAndRetryReuses(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNthSticky("GetVM", 1, faultEmpty)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitGeneral, clerrors.CodeVMProvisionFailed)
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v", cloud.liveVMs())
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(err.Error(), id) {
		t.Fatalf("timeout error does not name %s: %v", id, err)
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID != id || active.Status == "running" {
		t.Fatalf("state %+v", active)
	}
	vms, fips := cloud.creates()
	cloud.disarm()
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if v2, f2 := cloud.creates(); v2 != vms || f2 != fips {
		t.Fatalf("retry created vm %d→%d fip %d→%d", vms, v2, fips, f2)
	}
	_ = buf
}

func TestFault_Deploy_FIPStaysDownIsRecorded(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{fipDown: true}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nout=%s", err, buf.String())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.FloatingIPID == "" || active.Status != "running" {
		t.Fatalf("state %+v", active)
	}
	if len(cloud.liveFIPs()) != 1 || cloud.liveFIPs()[0].ID != active.FloatingIPID {
		t.Fatalf("fips %+v", cloud.liveFIPs())
	}
	cloud.fipDown = false
	if err := faultDestroy(t, eng); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Deploy_ThirdRuleLostAdopted(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("AddSecurityGroupRule", 2, faultLost)
	ctx := context.Background()
	compose := faultCompose(t, dir, 3)
	err := eng.Deploy(ctx, DeployOptions{ComposeFile: compose, Flavor: "low-1-1", Region: faultZone, Yes: true, NonInteractive: true})
	if err != nil {
		t.Fatalf("deploy: %v\nout=%s\nrules=%v", err, buf.String(), cloud.liveRules())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || len(active.OpenedRules) != 3 {
		t.Fatalf("opened=%v rules=%v", active.OpenedRules, cloud.liveRules())
	}
	if len(cloud.liveRules()) != 3 {
		t.Fatalf("world rules=%d", len(cloud.liveRules()))
	}
	if err := faultDestroy(t, eng); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveRules()) != 0 || len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("destroy left rules=%d vms=%d fips=%d", len(cloud.liveRules()), len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
}

func TestFault_Deploy_UploadFailKeepsVMAndRetryDoesNotDuplicate(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	dep := &faultDeployer{}
	dep.failAt("upload", faultTransport)
	eng, buf, dir := faultEngine(t, cloud, dep)
	err := faultDeploy(t, eng, dir)
	if err == nil || len(cloud.liveVMs()) != 1 {
		t.Fatalf("err=%v vms=%d", err, len(cloud.liveVMs()))
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(err.Error(), id) && !strings.Contains(buf.String(), id) {
		// upload fails after the VM is recorded; the error may not repeat the id
		// when the status file has it. The hint path is vmLeftRunning only on cancel.
		active := faultState(t, dir).ActiveDeployment()
		if active == nil || active.VMID != id {
			t.Fatalf("state %+v err=%v", active, err)
		}
	}
	vms, fips := cloud.creates()
	dep.disarm()
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if v2, f2 := cloud.creates(); v2 != vms || f2 != fips {
		t.Fatalf("retry created vm %d→%d fip %d→%d", vms, v2, fips, f2)
	}
	if err := faultDestroy(t, eng); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Deploy_ComposeFailKeepsVM(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	dep := &faultDeployer{}
	dep.failAt("compose", faultTransport)
	eng, _, dir := faultEngine(t, cloud, dep)
	err := faultDeploy(t, eng, dir)
	faultWantExit(t, err, clerrors.ExitGeneral, clerrors.CodeSSHTimeout)
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID == "" || active.FloatingIPID == "" || active.Status == "running" {
		t.Fatalf("state %+v err=%v", active, err)
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("world vms=%d fips=%d", len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
}

func TestFault_Deploy_UnhealthyVerifyStillRunning(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	eng.Probe = func(context.Context, string, []int, time.Duration) []verify.ProbeResult {
		return []verify.ProbeResult{{Port: 8080, Healthy: false, Error: "refused"}}
	}
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("unhealthy verify failed the deploy: %v\nout=%s", err, buf.String())
	}
	if faultState(t, dir).ActiveDeployment().Status != "running" {
		t.Fatal("status")
	}
}

func TestFault_Deploy_CancelWhileWaitingForVM(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud.cancel = cancel
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("GetVM", 1, faultCancel)
	err := faultDeployCtx(t, eng, dir, ctx, 2)
	faultWantExit(t, err, clerrors.ExitInterrupted, clerrors.ReasonInterrupted)
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v err=%v", cloud.liveVMs(), err)
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(faultErrText(err)+"\n"+buf.String(), id) {
		t.Fatalf("130 error does not name %s: %v\nout=%s", id, err, buf.String())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID != id {
		t.Fatalf("state %+v", active)
	}
	faultAccounted(t, cloud, dir, err, buf)
	cloud.disarm()
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("destroy after cancel: %v", err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Destroy_VM404ThenFIPReleased(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	cloud.deleteVM = 404
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Destroy_FIP409KeepsRecordThenRetry(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	fip := cloud.liveFIPs()[0].ID
	cloud.deleteVM = 404
	cloud.deleteFIP = 409
	err := faultDestroy(t, eng)
	if err == nil {
		t.Fatal("FIP 409 was success")
	}
	if !strings.Contains(err.Error(), fip) {
		t.Fatalf("error does not name %s: %v\nout=%s", fip, err, buf.String())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.Status != "destroying" || active.FloatingIPID != fip {
		t.Fatalf("state %+v", active)
	}
	if len(cloud.liveFIPs()) != 1 {
		t.Fatalf("fips %+v", cloud.liveFIPs())
	}
	cloud.deleteFIP = 0
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("retry destroy: %v", err)
	}
	if len(cloud.liveFIPs()) != 0 || len(cloud.liveVMs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Destroy_Rule404IsClosed(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveRules()) == 0 {
		t.Fatal("no rules")
	}
	cloud.deleteRule = 404
	// 404 means the rule is already gone: the fake drops it, as the platform would.
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(cloud.liveRules()) != 0 || len(cloud.liveVMs()) != 0 {
		t.Fatalf("rules=%d vms=%d", len(cloud.liveRules()), len(cloud.liveVMs()))
	}
}

func TestFault_Destroy_CancelBetweenVMAndFIPThenRetry(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	fip := cloud.liveFIPs()[0].ID
	vm := cloud.liveVMs()[0].ID
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud.cancel = cancel
	cloud.failNth("DeleteFloatingIP", 1, faultCancel)
	err := eng.Destroy(ctx, DestroyOptions{Force: true})
	faultWantExit(t, err, clerrors.ExitInterrupted, clerrors.ReasonInterrupted)
	if len(cloud.liveVMs()) != 0 {
		t.Fatalf("VM still live: %v", cloud.liveVMs())
	}
	if len(cloud.liveFIPs()) != 1 {
		t.Fatalf("fips %+v", cloud.liveFIPs())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.FloatingIPID != fip || active.VMID != vm {
		t.Fatalf("state %+v err=%v out=%s", active, err, buf.String())
	}
	cloud.disarm()
	if err := faultDestroy(t, eng); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(cloud.liveFIPs()) != 0 {
		t.Fatalf("fips left %+v", cloud.liveFIPs())
	}
}

func TestFault_Destroy_VM409KeepsRecord(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	id := cloud.liveVMs()[0].ID
	cloud.deleteVM = 409
	err := faultDestroy(t, eng)
	faultWantExit(t, err, clerrors.ExitAPI, clerrors.CodeAPIError)
	if !strings.Contains(err.Error(), id) {
		t.Fatalf("error does not name %s: %v", id, err)
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID != id || active.Status == "destroyed" {
		t.Fatalf("state %+v", active)
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("world changed vms=%d fips=%d", len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
}

func TestFault_Deploy_CancelBeforeVMReleasesFIP(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud.cancel = cancel
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultCancel)
	err := faultDeployCtx(t, eng, dir, ctx, 2)
	faultWantExit(t, err, clerrors.ExitInterrupted, clerrors.ReasonInterrupted)
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("cancel before the VM left vms=%v fips=%v out=%s err=%v", cloud.liveVMs(), cloud.liveFIPs(), buf.String(), err)
	}
	if faultState(t, dir).ActiveDeployment() != nil {
		t.Fatal("state has a record")
	}
}

func TestFault_Deploy_SSHConnectFailRecordsVM(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	dep := &faultDeployer{}
	dep.failAt("connect", faultTransport)
	eng, _, dir := faultEngine(t, cloud, dep)
	err := faultDeploy(t, eng, dir)
	if err == nil {
		t.Fatal("ssh succeeded")
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.VMID == "" || active.FloatingIPID == "" || active.Status == "running" {
		t.Fatalf("state %+v err=%v", active, err)
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("world vms=%d fips=%d", len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
	vms, fips := cloud.creates()
	dep.disarm()
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if v2, f2 := cloud.creates(); v2 != vms || f2 != fips {
		t.Fatalf("retry created vm %d→%d fip %d→%d", vms, v2, fips, f2)
	}
}

func TestFault_Deploy_JSONNamesCreatedVM(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	var stdout, stderr bytes.Buffer
	eng.Printer = output.NewWithWriters(&stdout, &stderr, output.ModeJSON, false)
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(stdout.String(), id) {
		t.Fatalf("stdout does not name %s: %s", id, stdout.String())
	}
}
