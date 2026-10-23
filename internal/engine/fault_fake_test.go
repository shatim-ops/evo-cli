// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//lint:file-ignore U1000 the fake is one file kept in engine and tools (TestFaultFakeCopiesMatch); each package uses part of it

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deploy"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Fault kinds for faultCloud.failAt. "lost" and "empty" apply on creates:
// the resource is stored, then the answer is an error or a success with an
// empty id. Other methods treat both as a transport error.
const (
	faultCancel       = "ctx-cancel"
	faultTransport    = "transport"
	faultLost         = "lost"
	faultEmpty        = "empty"
	faultAuth         = "auth"
	faultReject       = "reject"        // HTTP 409, nothing stored
	faultHTTP500      = "http500"       // HTTP 500, nothing stored
	faultLostNameless = "lost-nameless" // FIP stored with an empty name, answer lost
	faultSlow         = "slow"          // stored, the answer comes after the client's timeout
)

const (
	faultProject = "00000000-0000-4000-8000-000000000001"
	faultZone    = "00000000-0000-4000-8000-0000000000a1"
	faultImage   = "00000000-0000-4000-8000-0000000000b1"
	faultFlavor  = "00000000-0000-4000-8000-0000000000c1"
	faultSubnet  = "00000000-0000-4000-8000-0000000000d1"
	faultSG      = "00000000-0000-4000-8000-0000000000e1"
	faultDisk    = "00000000-0000-4000-8000-0000000000d2"
)

// faultCloud is a CloudAPI whose world (VMs, floating IPs, security-group
// rules) survives a lost answer. failAt arms the Nth method call (1-based,
// every CloudAPI method counts).
type faultCloud struct {
	mu  sync.Mutex
	seq int
	log []string
	// point is the armed failure. n is the global 1-based call number
	// (failAt). op+nth is the nth call of that method (failNth). sticky
	// keeps firing from that call on (a token that stays expired).
	point  faultPoint
	seen   map[string]int
	cancel context.CancelFunc
	// noDial makes create answers carry an empty address, so a caller that
	// dials TCP/22 (deploy_apply) does not touch the network. The world
	// still stores the address.
	noDial bool
	// pauseOp blocks the matching method once, after the world is updated,
	// until pauseCh is closed. pauseNote is closed when the pause begins.
	pauseOp   string
	pauseCh   chan struct{}
	pauseNote chan struct{}

	vms   []faultVM
	fips  []faultFIP
	rules []provision.SGRule
	next  int

	// fipDown keeps every floating IP in state "down" and rejects attach.
	fipDown bool
	// deleteVM / deleteFIP / deleteRule override the next delete, once.
	deleteVM   int // 404 or 409
	deleteFIP  int
	deleteRule int
}

type faultVM struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
	FIPID     string
	gone      bool
}

type faultFIP struct {
	ID      string
	Name    string
	IP      string
	State   string
	VMID    string
	deleted bool
}

// faultPoint is one armed failure. Exactly one of n or op is used.
type faultPoint struct {
	n      int
	op     string
	nth    int
	kind   string
	sticky bool
}

// failAt arms global call n (1-based, every CloudAPI method counts).
func (c *faultCloud) failAt(n int, kind string) {
	c.mu.Lock()
	c.point = faultPoint{n: n, kind: kind}
	c.mu.Unlock()
}

// failNth arms the nth call of op (1-based).
func (c *faultCloud) failNth(op string, nth int, kind string) {
	c.mu.Lock()
	c.point = faultPoint{op: op, nth: nth, kind: kind}
	c.mu.Unlock()
}

// failNthSticky is failNth that keeps failing that op.
func (c *faultCloud) failNthSticky(op string, nth int, kind string) {
	c.mu.Lock()
	c.point = faultPoint{op: op, nth: nth, kind: kind, sticky: true}
	c.mu.Unlock()
}

// pauseOnce blocks the next call of op after it has updated the world.
// release unblocks it. note is closed when the pause starts.
func (c *faultCloud) pauseOnce(op string) (note <-chan struct{}, release func()) {
	n := make(chan struct{})
	ch := make(chan struct{})
	c.mu.Lock()
	c.pauseOp, c.pauseCh, c.pauseNote = op, ch, n
	c.mu.Unlock()
	var once sync.Once
	return n, func() { once.Do(func() { close(ch) }) }
}

func (c *faultCloud) reachedPause(op string) {
	c.mu.Lock()
	if c.pauseOp != op {
		c.mu.Unlock()
		return
	}
	note, ch := c.pauseNote, c.pauseCh
	c.pauseOp = ""
	c.pauseNote = nil
	c.pauseCh = nil
	c.mu.Unlock()
	close(note)
	<-ch
}

func (c *faultCloud) disarm() {
	c.mu.Lock()
	c.point = faultPoint{}
	c.mu.Unlock()
}

func (c *faultCloud) callLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

// gate counts the call. errFaultLost, errFaultEmpty and errFaultNameless
// tell a create method to store the resource and then fail the answer.
func (c *faultCloud) gate(op string) error {
	c.mu.Lock()
	c.seq++
	c.log = append(c.log, op)
	if c.seen == nil {
		c.seen = map[string]int{}
	}
	c.seen[op]++
	p := c.point
	fire := p.n > 0 && c.seq == p.n
	if p.op == op && p.nth > 0 && c.seen[op] >= p.nth && (c.seen[op] == p.nth || p.sticky) {
		fire = true
	}
	if fire && p.op != "" && !p.sticky {
		c.point = faultPoint{}
	}
	kind := p.kind
	cancel := c.cancel
	c.mu.Unlock()
	if !fire {
		return nil
	}
	switch kind {
	case faultCancel:
		if cancel != nil {
			cancel()
		}
		return context.Canceled
	case faultTransport:
		return clerrors.New(clerrors.CodeAPIUnreachable, "timeout", "network")
	case faultAuth:
		return clerrors.New(clerrors.CodeAuthExpired, "token expired", "evo configure")
	case faultReject:
		return clerrors.Wrap(clerrors.CodeAPIError, "name taken", "", &provision.StatusError{Status: 409, Body: "vm_with_this_data_already_exists"})
	case faultHTTP500:
		return clerrors.Wrap(clerrors.CodeAPIError, "upstream", "", &provision.StatusError{Status: 500, Body: "boom"})
	case faultLost:
		return errFaultLost
	case faultSlow:
		return errFaultSlow
	case faultEmpty:
		return errFaultEmpty
	case faultLostNameless:
		return errFaultNameless
	default:
		return clerrors.New(clerrors.CodeAPIError, "fault", "")
	}
}

var (
	errFaultLost = errors.New("fault: lost")
	// errFaultSlow is a lost answer whose error is the client's deadline,
	// as http.Client.Timeout reports it.
	errFaultSlow     = fmt.Errorf("%w: the answer came after the client's timeout: %w", errFaultLost, context.DeadlineExceeded)
	errFaultEmpty    = errors.New("fault: empty")
	errFaultNameless = errors.New("fault: nameless")
)

// lostAnswer is the error of a create whose answer did not come back:
// lost, the client's deadline for faultSlow.
func lostAnswer(fault, lost error) error {
	if errors.Is(fault, context.DeadlineExceeded) {
		return fault
	}
	return lost
}

func (c *faultCloud) bump() int {
	c.next++
	return c.next
}

func (c *faultCloud) SetToken(string) {}

func (c *faultCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	err := c.gate("CreateVM")
	if err != nil && !errors.Is(err, errFaultLost) && !errors.Is(err, errFaultEmpty) {
		return nil, err
	}
	vm, taken := c.storeVM(req)
	// CMP-13: a second VM with the same name is 409 and is not stored.
	// The check shares storeVM's lock, so two creates cannot both land.
	if taken {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "name taken", "", &provision.StatusError{Status: 409, Body: "vm_with_this_data_already_exists"})
	}
	c.reachedPause("CreateVM")
	if errors.Is(err, errFaultLost) {
		return nil, lostAnswer(err, clerrors.New(clerrors.CodeAPIUnreachable, "lost response", "network"))
	}
	if errors.Is(err, errFaultEmpty) {
		return &provision.VM{Name: vm.Name, CreatedAt: vm.CreatedAt}, nil
	}
	ip := c.ipOf(vm.FIPID)
	if c.noDial {
		ip = ""
	}
	return &provision.VM{ID: vm.ID, Name: vm.Name, Status: "active", CreatedAt: vm.CreatedAt, PublicIP: ip}, nil
}

func (c *faultCloud) storeVM(req provision.CreateVMRequest) (faultVM, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if req.Name != "" {
		for _, vm := range c.vms {
			if !vm.gone && vm.Name == req.Name {
				return faultVM{}, true
			}
		}
	}
	n := c.bump()
	id := faultID(0x10 + n)
	fipID := ""
	if len(req.Interfaces) > 0 {
		fipID = req.Interfaces[0].AttachExternalIPID
	}
	vm := faultVM{ID: id, Name: req.Name, Status: "active", CreatedAt: time.Now(), FIPID: fipID}
	c.vms = append(c.vms, vm)
	if fipID != "" {
		for i := range c.fips {
			if c.fips[i].ID == fipID {
				c.fips[i].VMID = id
				c.fips[i].State = "in_use"
			}
		}
	}
	return vm, false
}

func (c *faultCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	err := c.gate("GetVM")
	if err != nil && !errors.Is(err, errFaultEmpty) {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, vm := range c.vms {
		if vm.ID == id && !vm.gone {
			status := vm.Status
			if errors.Is(err, errFaultEmpty) {
				status = ""
			}
			return &provision.VM{ID: vm.ID, Name: vm.Name, Status: status, CreatedAt: vm.CreatedAt, PublicIP: c.ipOfLocked(vm.FIPID)}, nil
		}
	}
	return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) DeleteVM(_ context.Context, id string) error {
	if err := c.gate("DeleteVM"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleteVM == 404 {
		// The platform says the VM is already gone.
		c.deleteVM = 0
		for i := range c.vms {
			if c.vms[i].ID == id {
				c.vms[i].gone = true
			}
		}
		return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	if c.deleteVM == 409 {
		// Stays until the test clears it: a VM that is not deletable yet.
		return clerrors.Wrap(clerrors.CodeAPIError, "not deletable yet", "", &provision.StatusError{Status: 409, Body: "not deletable yet"})
	}
	for i := range c.vms {
		if c.vms[i].ID == id {
			c.vms[i].gone = true
			return nil
		}
	}
	return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) DeleteVMInProject(ctx context.Context, id, _ string) error {
	return c.DeleteVM(ctx, id)
}

func (c *faultCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	if err := c.gate("ListVMsInProject"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*provision.VM
	for _, vm := range c.vms {
		if vm.gone {
			continue
		}
		out = append(out, &provision.VM{ID: vm.ID, Name: vm.Name, Status: vm.Status, CreatedAt: vm.CreatedAt})
	}
	return out, nil
}

func (c *faultCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	err := c.gate("CreateFloatingIP")
	if err != nil && !errors.Is(err, errFaultLost) && !errors.Is(err, errFaultEmpty) && !errors.Is(err, errFaultNameless) {
		return nil, err
	}
	c.mu.Lock()
	n := c.bump()
	id := faultID(0x20 + n)
	name := req.Name
	if err == errFaultNameless {
		name = ""
	}
	fip := faultFIP{ID: id, Name: name, IP: faultIP(n), State: "available"}
	// interface_id on the create binds the address (the request field the
	// spec documents). A later GetVMDetail then shows it, so a retry does
	// not reserve a second one.
	if req.InterfaceID != "" {
		fip.State = "in_use"
		for i := range c.vms {
			if !c.vms[i].gone && c.vms[i].FIPID == "" {
				c.vms[i].FIPID = id
				fip.VMID = c.vms[i].ID
				break
			}
		}
	}
	c.fips = append(c.fips, fip)
	c.mu.Unlock()
	c.reachedPause("CreateFloatingIP")
	if errors.Is(err, errFaultLost) || errors.Is(err, errFaultNameless) {
		return nil, lostAnswer(err, clerrors.New(clerrors.CodeAPIUnreachable, "lost response", ""))
	}
	ip := fip.IP
	if c.noDial {
		ip = ""
	}
	if errors.Is(err, errFaultEmpty) {
		return &provision.FloatingIP{Name: name, IPAddress: ip, State: "available"}, nil
	}
	return &provision.FloatingIP{ID: id, Name: req.Name, IPAddress: ip, State: "available"}, nil
}

func (c *faultCloud) GetFloatingIP(_ context.Context, id string) (*provision.FloatingIP, error) {
	if err := c.gate("GetFloatingIP"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.fips {
		if f.ID == id && !f.deleted {
			st := f.State
			if c.fipDown {
				st = "down"
			}
			return &provision.FloatingIP{ID: f.ID, Name: f.Name, IPAddress: f.IP, State: st}, nil
		}
	}
	return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	if err := c.gate("ListFloatingIPs"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*provision.FloatingIP
	for _, f := range c.fips {
		if f.deleted {
			continue
		}
		out = append(out, &provision.FloatingIP{ID: f.ID, Name: f.Name, IPAddress: f.IP, State: f.State})
	}
	return out, nil
}

func (c *faultCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	if err := c.gate("DeleteFloatingIP"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleteFIP == 409 {
		return clerrors.Wrap(clerrors.CodeAPIError, "in use", "", &provision.StatusError{Status: 409, Body: "in use"})
	}
	if c.deleteFIP == 500 {
		return clerrors.Wrap(clerrors.CodeAPIError, "upstream", "", &provision.StatusError{Status: 500, Body: "boom"})
	}
	for i := range c.fips {
		if c.fips[i].ID == id {
			c.fips[i].deleted = true
			return nil
		}
	}
	return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) AttachFloatingIP(_ context.Context, fipID, _, _, _ string) (*provision.FloatingIP, error) {
	if err := c.gate("AttachFloatingIP"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fipDown {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "still down", "", &provision.StatusError{Status: 500, Body: "down"})
	}
	for i := range c.fips {
		if c.fips[i].ID == fipID {
			c.fips[i].State = "in_use"
			return &provision.FloatingIP{ID: c.fips[i].ID, Name: c.fips[i].Name, IPAddress: c.fips[i].IP, State: "in_use"}, nil
		}
	}
	return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) AddSecurityGroupRule(_ context.Context, _, _ string, rule provision.SGRule) (*provision.SGRule, error) {
	err := c.gate("AddSecurityGroupRule")
	if err != nil && !errors.Is(err, errFaultLost) && !errors.Is(err, errFaultEmpty) {
		return nil, err
	}
	c.mu.Lock()
	n := c.bump()
	rule.ID = faultID(0x30 + n)
	rule.State = "created"
	c.rules = append(c.rules, rule)
	id := rule.ID
	c.mu.Unlock()
	if errors.Is(err, errFaultLost) {
		return nil, lostAnswer(err, clerrors.Wrap(clerrors.CodeAPIError, "lost", "", &provision.StatusError{Status: 500, Body: "lost"}))
	}
	if errors.Is(err, errFaultEmpty) {
		rule.ID = ""
		return &rule, nil
	}
	return &provision.SGRule{ID: id, Direction: rule.Direction, IPProtocol: rule.IPProtocol, PortRange: rule.PortRange, Description: rule.Description, State: "created"}, nil
}

func (c *faultCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	if err := c.gate("ListSecurityGroupRules"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*provision.SGRule, 0, len(c.rules))
	for i := range c.rules {
		r := c.rules[i]
		out = append(out, &r)
	}
	return out, nil
}

func (c *faultCloud) DeleteSecurityGroupRule(_ context.Context, _, ruleID string) error {
	if err := c.gate("DeleteSecurityGroupRule"); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleteRule == 404 {
		// 404: the rule is already gone.
		for i := range c.rules {
			if c.rules[i].ID == ruleID {
				c.rules = append(c.rules[:i], c.rules[i+1:]...)
				break
			}
		}
		return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	for i := range c.rules {
		if c.rules[i].ID == ruleID {
			c.rules = append(c.rules[:i], c.rules[i+1:]...)
			return nil
		}
	}
	return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

func (c *faultCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	if err := c.gate("ListFlavors"); err != nil {
		return nil, err
	}
	return []*provision.Flavor{{ID: faultFlavor, Name: "low-1-1", VCPUs: 1, RAMGB: 1}}, nil
}

func (c *faultCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	if err := c.gate("ListAvailabilityZones"); err != nil {
		return nil, err
	}
	on := true
	return []*provision.AvailabilityZone{{ID: faultZone, Name: "ru.AZ-1", Enabled: &on}}, nil
}

func (c *faultCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	if err := c.gate("ListImages"); err != nil {
		return nil, err
	}
	return []*provision.Image{{ID: faultImage, Name: "ubuntu-22.04", OS: "linux", Status: "active"}}, nil
}

func (c *faultCloud) ListImagesInProject(ctx context.Context, _ string) ([]*provision.Image, error) {
	return c.ListImages(ctx)
}

func (c *faultCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	if err := c.gate("GetImage"); err != nil {
		return nil, err
	}
	return &provision.ImageDetail{
		ID: faultImage, Name: "ubuntu-22.04", DisplayName: "Ubuntu 22.04", OS: "linux",
		MetadataFields: []provision.ImageMetadataField{
			{ID: "mf-1", Name: "ssh_key", MandatoryGroup: "ssh"},
			{ID: "mf-2", Name: "hostname"},
			{ID: "mf-3", Name: "name", MandatoryGroup: "name", DefaultValue: "user1"},
		},
	}, nil
}

func (c *faultCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	if err := c.gate("ListDiskTypes"); err != nil {
		return nil, err
	}
	return []*provision.DiskType{{ID: faultDisk, Name: "SSD"}}, nil
}

func (c *faultCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	if err := c.gate("ListSubnets"); err != nil {
		return nil, err
	}
	dns := []any{"203.0.113.53"}
	return []*provision.Subnet{{
		ID: faultSubnet, Name: "default", AvailabilityZoneID: faultZone,
		Default: true, RoutedNetwork: true, State: "created", DNSServers: &dns,
	}}, nil
}

func (c *faultCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	if err := c.gate("ListSecurityGroups"); err != nil {
		return nil, err
	}
	return []*provision.SecurityGroup{{
		ID: faultSG, Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: faultZone, Default: true,
	}}, nil
}

func (c *faultCloud) GetVMDetail(_ context.Context, id string) (*provision.VMDetail, error) {
	if err := c.gate("GetVMDetail"); err != nil {
		return nil, err
	}
	c.mu.Lock()
	var detail *provision.VMDetail
	for _, vm := range c.vms {
		if vm.ID == id && !vm.gone {
			in := provision.VMInterface{ID: faultID(0x40), Primary: true, State: "created", Type: "regular", IPAddress: "10.0.0.8"}
			if vm.FIPID != "" {
				in.FloatingIP = &provision.IfaceFloatingIP{ID: vm.FIPID, IPAddress: c.ipOfLocked(vm.FIPID), State: "in_use"}
			}
			detail = &provision.VMDetail{ID: vm.ID, Name: vm.Name, State: vm.Status, Interfaces: []provision.VMInterface{in}}
			break
		}
	}
	c.mu.Unlock()
	if detail == nil {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	// After the snapshot is taken, so a second caller can attach an address
	// while this one is still between the read and CreateFloatingIP.
	c.reachedPause("GetVMDetail")
	return detail, nil
}

func (c *faultCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	if err := c.gate("PriceCalculation"); err != nil {
		return nil, err
	}
	return nil, clerrors.New(clerrors.CodeAPIError, "no price", "")
}

func (c *faultCloud) CreateImage(context.Context, provision.CreateImageReq) (map[string]any, error) {
	return map[string]any{"id": faultImage}, c.gate("CreateImage")
}
func (c *faultCloud) DeleteImage(context.Context, string, string) error {
	return c.gate("DeleteImage")
}
func (c *faultCloud) ListNATGateways(context.Context, string) ([]map[string]any, error) {
	if err := c.gate("ListNATGateways"); err != nil {
		return nil, err
	}
	return nil, nil
}
func (c *faultCloud) CreateNATGateway(context.Context, provision.CreateNATGatewayRequest) (map[string]any, error) {
	return nil, c.gate("CreateNATGateway")
}
func (c *faultCloud) DeleteNATGateway(context.Context, string, string) error {
	return c.gate("DeleteNATGateway")
}
func (c *faultCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	if err := c.gate("ListDisks"); err != nil {
		return nil, err
	}
	return nil, nil
}
func (c *faultCloud) CreateDisk(context.Context, provision.CreateDiskReq) (map[string]any, error) {
	return nil, c.gate("CreateDisk")
}
func (c *faultCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	return nil, c.gate("AttachDisk")
}
func (c *faultCloud) DetachDisk(context.Context, string, string, string) (map[string]any, error) {
	return nil, c.gate("DetachDisk")
}
func (c *faultCloud) ResizeDisk(context.Context, string, int, string) (map[string]any, error) {
	return nil, c.gate("ResizeDisk")
}
func (c *faultCloud) DeleteDisk(context.Context, string, string) error { return c.gate("DeleteDisk") }

func (c *faultCloud) liveVMs() []faultVM {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []faultVM
	for _, vm := range c.vms {
		if !vm.gone {
			out = append(out, vm)
		}
	}
	return out
}

func (c *faultCloud) liveRules() []provision.SGRule {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]provision.SGRule(nil), c.rules...)
}

func (c *faultCloud) liveFIPs() []faultFIP {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []faultFIP
	for _, f := range c.fips {
		if !f.deleted {
			out = append(out, f)
		}
	}
	return out
}

func (c *faultCloud) creates() (vms, fips int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.vms), len(c.fips)
}

func (c *faultCloud) ipOf(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ipOfLocked(id)
}

func (c *faultCloud) ipOfLocked(id string) string {
	for _, f := range c.fips {
		if f.ID == id {
			return f.IP
		}
	}
	return ""
}

func faultID(n int) string {
	const hex = "0123456789abcdef"
	b := []byte("00000000-0000-4000-8000-000000000000")
	for i := 0; i < 3; i++ {
		b[len(b)-1-i] = hex[(n>>uint(i*4))&0xf]
	}
	return string(b)
}

func faultIP(n int) string {
	return "203.0.113." + itoa(10+(n%200))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// faultDeployer is an SSH deployer with the same fault kinds on Upload,
// on the compose-up ExecStream, and (via probe) on verify.
type faultDeployer struct {
	mu       sync.Mutex
	failOp   string // "upload", "compose", "connect"
	failKind string
	uploads  int
	composes int
	connects int
}

func (d *faultDeployer) failAt(op, kind string) {
	d.mu.Lock()
	d.failOp, d.failKind = op, kind
	d.mu.Unlock()
}

func (d *faultDeployer) disarm() { d.failAt("", "") }

func (d *faultDeployer) Connect(context.Context, string, int, string, []byte, string) error {
	d.mu.Lock()
	d.connects++
	kind := ""
	if d.failOp == "connect" {
		kind = d.failKind
	}
	d.mu.Unlock()
	if kind == faultTransport || kind == faultLost {
		return clerrors.New(clerrors.CodeAPIUnreachable, "ssh timeout", "")
	}
	return nil
}

func (d *faultDeployer) HostKeyFingerprint() string { return "SHA256:fault" }

func (d *faultDeployer) Upload(context.Context, []deploy.FileEntry) error {
	d.mu.Lock()
	d.uploads++
	kind := ""
	if d.failOp == "upload" {
		kind = d.failKind
	}
	d.mu.Unlock()
	if kind != "" {
		return clerrors.New(clerrors.CodeAPIUnreachable, "upload interrupted", "")
	}
	return nil
}

func (d *faultDeployer) Exec(context.Context, string) (string, error) {
	return "Docker Compose version v2.24.0", nil
}

func (d *faultDeployer) ExecStream(_ context.Context, cmd string, stdout, _ io.Writer) error {
	d.mu.Lock()
	compose := false
	if containsComposeUp(cmd) {
		d.composes++
		compose = d.failOp == "compose"
	}
	kind := d.failKind
	d.mu.Unlock()
	if compose && kind != "" {
		return clerrors.New(clerrors.CodeAPIUnreachable, "compose interrupted", "")
	}
	_, _ = io.WriteString(stdout, "ok\n")
	return nil
}

func (d *faultDeployer) Close() error { return nil }

func containsComposeUp(cmd string) bool {
	return faultHas(cmd, "compose") && faultHas(cmd, " up")
}

func faultHas(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
