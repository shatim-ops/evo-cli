// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D69, variant C: the bound of a deploy (DeployOptions.Until, the budget of
// an MCP call) ends the wait for the cloud and the VM with InProgress — the
// VM in the state as creating, the phase and its state — not an error and
// not a VM left without a record. Without a bound (the CLI) deploy waits as
// before.
func TestDeploy_BoundEndsTheWaitForTheVMInProgress(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "creating"}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Timings.VMPollInterval = time.Millisecond
	eng.Timings.VMPollTimeout = time.Hour
	opts := newDeployOpts(t, dir)
	opts.Until = time.Now().Add(50 * time.Millisecond)

	err := eng.Deploy(context.Background(), opts)
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "vm" || going.Detail != "creating" {
		t.Fatalf("err = %v, want InProgress vm creating", err)
	}
	if a := readActive(t, dir); a == nil || a.Status != "creating" || a.VMID == "" {
		t.Fatalf("the VM is not in the state as creating: %+v", a)
	}
}

// SSH that does not answer yet ends with the bound too.
func TestDeploy_BoundEndsTheWaitForSSHInProgress(t *testing.T) {
	dep := newMockDeployer()
	dep.connectErr = stderrors.New("ssh: connection refused")
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Timings.SSHRetryInterval = time.Millisecond
	opts := newDeployOpts(t, dir)
	opts.Until = time.Now().Add(50 * time.Millisecond)

	err := eng.Deploy(context.Background(), opts)
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "ssh" {
		t.Fatalf("err = %v, want InProgress ssh", err)
	}
}
