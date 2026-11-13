// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// lostFIPCloud makes the floating IP it is asked for and loses the answer:
// the create holds until the call ends (the budget of an MCP call, D69, or
// a cancel). The list then shows the floating IP to a call still running.
type lostFIPCloud struct {
	provision.CloudAPI
	mu   sync.Mutex
	made []*provision.FloatingIP
}

func (*lostFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-1", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, State: "created"}}}, nil
}

func (c *lostFIPCloud) CreateFloatingIP(ctx context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.mu.Lock()
	c.made = append(c.made, &provision.FloatingIP{ID: "fip-lost", Name: req.Name, IPAddress: "203.0.113.7", State: "down"})
	c.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *lostFIPCloud) ListFloatingIPs(ctx context.Context, _ string) ([]*provision.FloatingIP, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*provision.FloatingIP(nil), c.made...), nil
}

// A call that ends after the create of the floating IP was sent names the
// floating IP the cloud made, so the caller records it and destroy releases
// it: it was billed with no record when the budget of the call cut the
// answer (Qwen on #759; CLAUDE.md §6, axis 3).
func TestAssignFloatingIP_CutAfterTheCreateNamesTheFIP(t *testing.T) {
	cloud := &lostFIPCloud{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ip, id, err := assignFloatingIP(ctx, cloud, Timings{IfacePollInterval: time.Millisecond, FIPRetryInterval: time.Millisecond, FIPPollInterval: time.Millisecond}, nil, "vm-1", fipName("vm-1"), "p", "z")
	if err == nil {
		t.Fatalf("a call cut short is not a success: %s %s", ip, id)
	}
	if id != "fip-lost" || ip != "203.0.113.7" {
		t.Errorf("the floating IP the cloud made is not named: ip %q id %q (%v)", ip, id, err)
	}
	if n := len(cloud.made); n != 1 {
		t.Errorf("%d floating IPs created, want 1", n)
	}
}
