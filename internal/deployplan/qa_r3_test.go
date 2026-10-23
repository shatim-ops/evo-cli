// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"time"
)

// adoptFIPCloud: the plan's VM exists with a floating IP already attached.
type adoptFIPCloud struct {
	existingCloud
	fipCreates int
}

func (c *adoptFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular",
		FloatingIP: &provision.IfaceFloatingIP{ID: "fip-have", IPAddress: "203.0.113.5", State: "in_use"}}}}, nil
}
func (c *adoptFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{ID: "fip-second", IPAddress: "203.0.113.6"}, nil
}

// R2: a retried deploy_apply uses the FIP already on the VM and returns its
// id for the state, instead of creating a second one.
func TestQAr3_Apply_AdoptUsesAttachedFIP(t *testing.T) {
	c := &adoptFIPCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.fipCreates != 0 || res.PublicIP != "203.0.113.5" || res.FloatingIPID != "fip-have" {
		t.Fatalf("FIP creates %d, result %+v; want the attached fip-have", c.fipCreates, res)
	}
}

// lostFIPCloud: the VM has no FIP; the create answer is lost, the FIP exists.
type lostFIPCloud struct {
	adoptFIPCloud
}

func (c *lostFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular"}}}, nil
}
func (c *lostFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return nil, context.DeadlineExceeded
}
func (c *lostFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-user", Name: "my-reserved", IPAddress: "198.51.100.1", State: "available"},
		{ID: "fip-made", Name: "x-fip", IPAddress: "203.0.113.7", State: "in_use", VM: json.RawMessage(`{"id":"vm-old"}`)}}, nil
}

// R2/R9: the lost create is found by its name and recorded; a free FIP of
// the user is never taken.
func TestQAr3_Apply_LostFIPCreateFoundByName(t *testing.T) {
	c := &lostFIPCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FloatingIPID != "fip-made" || res.PublicIP != "203.0.113.7" {
		t.Fatalf("result %+v; want fip-made found by name, never the user's fip-user", res)
	}
}

// noIDFIPCloud: the create answers success without an id; the FIP exists
// under its name.
type noIDFIPCloud struct{ lostFIPCloud }

func (c *noIDFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{Name: "x-fip", IPAddress: "203.0.113.7", State: "available"}, nil
}

// Fault matrix (process audit 1.2.1, 6.1): a create that answered success
// without an id was taken as done — the address was billed, recorded
// nowhere, and destroy could not release it. It is found by its name, as
// deploy does (TestFault_Deploy_EmptyBootFIPIDIsAdoptedByName).
func TestApply_FIPCreateWithoutIDFoundByName(t *testing.T) {
	c := &noIDFIPCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FloatingIPID != "fip-made" || res.PublicIP != "203.0.113.7" || c.fipCreates != 1 {
		t.Fatalf("result %+v after %d creates; want fip-made found by name, one create", res, c.fipCreates)
	}
}

// unlistedFIPCloud: the create answer is lost and the list fails too.
type unlistedFIPCloud struct{ lostFIPCloud }

func (c *unlistedFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, context.DeadlineExceeded
}

// With the create answer lost and the list failed, the floating IP may
// exist on the VM. The note sends the user to deploy_apply again, which
// adopts the VM with the address on it, never to the console, where a
// second billed address would be attached (Qwen review of #211).
func TestApply_LostFIPUnlistedRetryNotConsole(t *testing.T) {
	_, _, note := ensureFloatingIP(context.Background(), &unlistedFIPCloud{}, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, "vm-old")
	if !strings.Contains(note, "deploy_apply again") || strings.Contains(note, "console") {
		t.Fatalf("note %q: want deploy_apply again, not the console", note)
	}
}

// R8: a VM with the plan's name in another zone is not adopted; a stopped
// one is reported as stopped, not running.
type zoneCloud struct {
	existingCloud
	vm *provision.VM
}

func (c *zoneCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{c.vm}, nil
}
func (c *zoneCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{}, nil
}

func TestQAr3_Apply_AdoptChecksZoneAndState(t *testing.T) {
	other := &zoneCloud{vm: &provision.VM{ID: "vm-old", Name: "x", Status: "running", Region: "zone-A"}}
	if res, err := ApplyWithTimeout(context.Background(), other, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x", Zone: RefSpec{ID: "zone-B"}}}, 0, nil); err == nil {
		t.Fatalf("VM of another zone adopted: %+v", res)
	}
	stopped := &zoneCloud{vm: &provision.VM{ID: "vm-old", Name: "x", Status: "stopped", Region: "zone-B"}}
	res, err := ApplyWithTimeout(context.Background(), stopped, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x", Zone: RefSpec{ID: "zone-B"}}}, 0, nil)
	if err != nil || res.Status != "stopped" {
		t.Fatalf("stopped VM: %+v, %v; want status stopped", res, err)
	}
}

// Релизный QA (B-lifecycle 3): подхваченная ВМ в creating — ожидание, затем
// floating IP и порты плана, как у новой ВМ; не готова к сроку — timeout.
type adoptCreatingCloud struct {
	existingCloud
	polls, fipCreates, rules int
}

func (c *adoptCreatingCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-old", Name: "x", Status: "creating"}}, nil
}
func (c *adoptCreatingCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	c.polls++
	if c.polls < 2 {
		return &provision.VM{ID: id, Status: "creating"}, nil
	}
	return &provision.VM{ID: id, Status: "running"}, nil
}
func (c *adoptCreatingCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular", State: "created"}}}, nil
}
func (c *adoptCreatingCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
func (c *adoptCreatingCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{ID: "fip-1", IPAddress: "203.0.113.20", State: "in_use"}, nil
}
func (c *adoptCreatingCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}
func (c *adoptCreatingCloud) AddSecurityGroupRule(context.Context, string, string, provision.SGRule) (*provision.SGRule, error) {
	c.rules++
	return &provision.SGRule{ID: "r"}, nil
}

func TestQAB_Apply_AdoptCreatingVMWaitsThenFinishes(t *testing.T) {
	old := pollInterval
	pollInterval = time.Millisecond
	defer func() { pollInterval = old }()
	c := &adoptCreatingCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"},
		Network: NetworkSpec{FloatingIP: true, SecurityGroupID: "sg-1"}, Ports: []int{80}}, time.Second, nil)
	if err != nil || res.Status != "running" || res.PublicIP != "203.0.113.20" || c.fipCreates != 1 || c.rules != 1 {
		t.Fatalf("err=%v res=%+v fip=%d rules=%d", err, res, c.fipCreates, c.rules)
	}
}

// Ревью Grok по #87: остановленной ВМ новый floating IP не создаётся —
// он бы только оплачивался; адрес, что уже на ней, показывается.
type stoppedFIPCloud struct {
	zoneCloud
	fipCreates int
}

func (c *stoppedFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true,
		FloatingIP: &provision.IfaceFloatingIP{ID: "fip-9", IPAddress: "203.0.113.9"}}}}, nil
}
func (c *stoppedFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{ID: "fip-new"}, nil
}

func TestQAB_Apply_AdoptStoppedVMCreatesNoFIP(t *testing.T) {
	c := &stoppedFIPCloud{zoneCloud: zoneCloud{vm: &provision.VM{ID: "vm-old", Name: "x", Status: "stopped"}}}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"},
		Network: NetworkSpec{FloatingIP: true}}, 0, nil)
	if err != nil || res.Status != "stopped" || c.fipCreates != 0 || res.PublicIP != "203.0.113.9" {
		t.Fatalf("err=%v res=%+v fipCreates=%d", err, res, c.fipCreates)
	}
}

// orphanOnlyFIPCloud: the create answer is lost, and the project holds a
// free floating IP of the plan's name — an orphan of an earlier VM — not
// bound to this one.
type orphanOnlyFIPCloud struct{ lostFIPCloud }

func (c *orphanOnlyFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-orphan", Name: "x-fip", IPAddress: "203.0.113.8", State: "available"}}, nil
}

// A3 (Cursor 1.2.3): the address found by name is taken only when it is
// bound to this VM. An orphan "<name>-fip" of an earlier VM was recorded as
// the new VM's public IP with the note "attached"; now none is recorded and
// the note sends the user to deploy_apply again, which adopts the VM with
// the address it has.
func TestApply_LostFIPOrphanByNameNotTaken(t *testing.T) {
	ip, id, note := ensureFloatingIP(context.Background(), &orphanOnlyFIPCloud{}, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, "vm-old")
	if ip != "" || id != "" || !strings.Contains(note, "deploy_apply again") {
		t.Errorf("orphan taken: ip %q id %q note %q", ip, id, note)
	}
}
