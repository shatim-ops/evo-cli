// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"sync"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// sleptVMCloud makes a VM that builds; then the machine sleeps and the
// service refuses the token once (G140-3: evo has dropped it, the next
// request takes a new one); then the VM runs.
type sleptVMCloud struct {
	portsCloud
	mu    sync.Mutex
	polls int
}

func (c *sleptVMCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	switch c.polls {
	case 1:
		return &provision.VM{ID: "vm-made", Status: "building"}, nil
	case 2:
		return nil, clerrors.New(clerrors.CodeAuthExpired, "Cloud.ru API rejected the token (401)", "repeat the command")
	}
	return &provision.VM{ID: "vm-made", Status: "running"}, nil
}

// The wait of deploy_apply for its new VM (finishApply) meets the refused
// token after the sleep and goes on: the VM is there and billed
// (CLAUDE.md §6, axes 2 and 3; process backlog 1.6, priority 1).
func TestApply_VMWaitGoesOnAfterOneRefusedToken(t *testing.T) {
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	cloud := &sleptVMCloud{}
	plan := &Plan{Product: "wiki", VM: VMSpec{Name: "wiki-1"}, Network: NetworkSpec{SecurityGroupID: "sg-1"}, Ports: []int{80}}
	res, err := ApplyWithTimeout(context.Background(), cloud, plan, time.Second, nil)
	if cloud.polls < 3 {
		t.Fatalf("the world did not reach the refusal (%d polls): it checks nothing — %+v %v", cloud.polls, res, err)
	}
	if err != nil || res == nil || res.Status != "running" || res.VMID != "vm-made" {
		t.Fatalf("the VM wait ends at the refused token: %+v %v", res, err)
	}
}
