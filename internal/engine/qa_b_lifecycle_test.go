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

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Релизный QA, B-lifecycle: повторный deploy на существующую запись.

func qaBEngine(t *testing.T, cloud *mockCloudAPI) (*Engine, string, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	e := &Engine{
		Config: config.Config{ProjectID: "00000000-0000-0000-0000-000000000001", ImageID: "00000000-0000-0000-0000-000000000002", DiskSize: 10, Region: testZoneID},
		Auth:   &mockTokenProvider{token: auth.Token{Value: "t", ExpiresAt: time.Now().Add(time.Hour)}},
		Cloud:  cloud, Deployer: newMockDeployer(),
		Printer:   output.NewWithWriter(&out, output.ModePlain, false),
		SSHKeyPEM: []byte("k"), SSHPubKey: "ssh-ed25519 AAAA test",
		StateDir: dir, Timings: fastTimings, Probe: (&stubProbe{}).Check,
	}
	return e, dir, &out
}

func writeRecord(t *testing.T, dir string, rec state.DeploymentRecord) {
	t.Helper()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
}

func countFIPCreates(cloud *mockCloudAPI) *int {
	n := 0
	cloud.createFloatingIPFn = func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		n++
		return &provision.FloatingIP{ID: "fip-new", Name: req.Name, IPAddress: "203.0.113.9", State: "in_use"}, nil
	}
	return &n
}

// №4: ВМ в error_creating не переиспользуется — отказ с «evo destroy», без
// floating IP и попыток SSH.
func TestQAB_Deploy_ErrorRecordRefused(t *testing.T) {
	cloud := &mockCloudAPI{}
	creates := countFIPCreates(cloud)
	e, dir, _ := qaBEngine(t, cloud)
	writeRecord(t, dir, state.DeploymentRecord{ID: "vm-err", VMID: "vm-err", ProjectID: e.Config.ProjectID, Region: "az-1", SSHUser: "ubuntu", Status: "error"})
	err := e.Deploy(context.Background(), DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", NonInteractive: true, Yes: true})
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeVMProvisionFailed || !strings.Contains(ce.Hint, "evo destroy") || *creates != 0 {
		t.Fatalf("err %v, FIP создан %d раз", err, *creates)
	}
}

// №1: у ВМ записи нет адреса — повторный deploy создаёт платный floating
// IP только с согласием; --dry-run так и говорит.
func TestQAB_Redeploy_PrivateVMAsksBeforeFIP(t *testing.T) {
	cloud := &mockCloudAPI{}
	creates := countFIPCreates(cloud)
	e, dir, out := qaBEngine(t, cloud)
	writeRecord(t, dir, state.DeploymentRecord{ID: "vm-priv", VMID: "vm-priv", ProjectID: e.Config.ProjectID, Region: "az-1", SSHUser: "user1", Status: "running"})
	opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", NonInteractive: true}

	dry := opts
	dry.DryRun = true
	if err := e.Deploy(context.Background(), dry); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "floating IP") || strings.Contains(out.String(), "no new resources") || strings.Contains(out.String(), "новые ресурсы не создаются") {
		t.Fatalf("превью не говорит о floating IP:\n%s", out.String())
	}

	err := e.Deploy(context.Background(), opts)
	if clerrors.Reason(err) != clerrors.CodeConfirmationRequired || *creates != 0 {
		t.Fatalf("без --yes: err %v, FIP создан %d раз", err, *creates)
	}

	opts.Yes = true
	if err := e.Deploy(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if *creates != 1 || sf.ActiveDeployment().FloatingIPID != "fip-new" {
		t.Fatalf("с --yes: FIP создан %d раз, запись %+v", *creates, sf.ActiveDeployment())
	}
}
