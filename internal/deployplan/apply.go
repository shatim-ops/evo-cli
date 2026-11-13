// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// DefaultPollTimeout is how long Apply waits for the VM to run when the
// caller sets nothing: the wait of deploy and provision
// (config.DefaultTimeout).
const DefaultPollTimeout = config.DefaultTimeout * time.Second

// pollInterval is how often ApplyWithTimeout asks for the VM state (a variable for tests).
var pollInterval = 10 * time.Second

// ApplyResult is the compact result returned to the agent after a deploy.
type ApplyResult struct {
	VMID     string `json:"vm_id"`
	Status   string `json:"status"`
	PublicIP string `json:"public_ip,omitempty"`
	// FloatingIPID is the VM's floating IP, kept in the state so destroy
	// releases exactly it (QA r3 R2).
	FloatingIPID string `json:"floating_ip_id,omitempty"`
	// LostFloatingIP is the name of the VM's floating IP whose create got
	// no answer: it may exist and be billed (floatingip.LostError). The
	// tool keeps its mark in the state.
	LostFloatingIP string `json:"lost_floating_ip,omitempty"`
	SSH            string `json:"ssh,omitempty"`
	Password       string `json:"password,omitempty"` // shown ONCE; not persisted
	// VMStatus is the VM's state when the apply stopped waiting for it
	// (Status "creating", D69).
	VMStatus   string `json:"vm_status,omitempty"`
	ElapsedSec int    `json:"elapsed_sec"`
	Note       string `json:"note,omitempty"`

	// Ports is what Apply recorded about the security group: the group, the
	// ports the VM needs and the rules Apply created (the caller keeps them
	// in state so destroy deletes them). Not part of the agent's answer.
	Ports state.DeploymentRecord `json:"-"`
}

// Progress shows an apply to the person who waits for it, as it goes (the
// CLI's stderr, the notifications/progress of an MCP client that asked):
// its stages on P, the wait for the VM on Wait, the ports opened in the
// words of deploy (Ports). A nil *Progress shows nothing.
type Progress struct {
	P     *output.Printer
	Wait  Waiter
	Ports func(sgrules.OpenResult, *state.DeploymentRecord)
}

// Waiter is the wait for the VM as it is shown (engine.WaitLine): told each
// state polled, shown again when C ticks, done on every way out.
type Waiter interface {
	C() <-chan time.Time
	Show()
	State(string)
	Done()
}

// step and done show a stage and its result; nothing without a Progress.
func (p *Progress) step(msg string) {
	if p != nil {
		p.P.Step(0, 0, msg)
	}
}

func (p *Progress) done(msg string) {
	if p != nil {
		p.P.StepDone(msg)
	}
}

// ApplyWithTimeout executes a resolved plan, waiting at most pollTimeout for
// the VM to run (CLOUDRU_TIMEOUT through the engine's timings; zero means
// the default), and shows it on show. until, when set, ends every wait of
// the apply (the budget of an MCP call or wait=false, D69): a VM still
// being made then is Status "creating", with VMStatus, for a repeat of the
// same plan to finish. It is idempotent: if a VM with the
// plan's name already exists, it adopts that VM instead of creating a
// duplicate.
func ApplyWithTimeout(ctx context.Context, cloud provision.CloudAPI, plan *Plan, pollTimeout time.Duration, until time.Time, show *Progress) (*ApplyResult, error) {
	if pollTimeout <= 0 {
		pollTimeout = DefaultPollTimeout
	}
	start := clock.Now()
	if show != nil && show.Wait != nil {
		defer show.Wait.Done()
	}

	// --- Idempotency: adopt an existing VM with the same name ---
	existing, lookErr := findVMByName(ctx, cloud, plan.ProjectID, plan.VM.Name)
	if lookErr != nil {
		// Unknown is not "absent": creating now could make a second
		// billed VM next to one a lost answer already created (QA H5).
		return nil, fmt.Errorf(i18n.T("cannot check whether VM %q already exists (%w); nothing was created — retry deploy_apply", "не удалось проверить, есть ли уже ВМ %q (%w); ничего не создано — повторите deploy_apply"), plan.VM.Name, lookErr)
	}
	if existing != nil && !ownVM(plan, existing) {
		// Someone else's VM with the plan's name: never adopted (D73) — it
		// would get a billed floating IP, the plan's ports and a record
		// that destroy deletes it by.
		return nil, &ForeignVMError{ID: existing.ID, Name: existing.Name, Status: existing.Status}
	}
	if existing != nil {
		if provision.VMFailed(existing.Status) {
			return &ApplyResult{
				VMID:   existing.ID,
				Status: "error",
				Note:   "a VM with this name already exists in error state; whether to delete it (destroy) before a retry — ask the person",
			}, nil
		}
		// Adopt only the plan's own VM: one with the same name in another
		// zone is someone else's (QA r3 R8).
		if z := existing.ZoneID(); z != "" && plan.VM.Zone.ID != "" && z != plan.VM.Zone.ID {
			return nil, fmt.Errorf("a VM named %q already exists in another availability zone (%s); nothing was created — choose another name in deploy_plan", plan.VM.Name, z)
		}
		// The adopted VM goes the way of a new one: waited for while it is
		// still being made, then its floating IP, SSH and the plan's ports.
		// Answering at once left a VM still creating without an IP and
		// open ports (release QA, B-lifecycle 3). A stopped VM does not
		// start by itself: it is reported as it is, with the address it
		// already has — a new floating IP on it would only be billed.
		switch existing.Status {
		case "", "running", "active", "creating", "building", "starting", "pending":
			show.step(i18n.Tf("Adopting VM %s: it already exists…", "Беру ВМ %s: она уже есть…", plan.VM.Name))
			return finishApply(ctx, cloud, plan, existing, "", start, pollTimeout, until, show,
				"VM already existed — adopted (idempotent), nothing recreated")
		}
		ip, fipID := attachedFloatingIP(ctx, cloud, existing.ID)
		return &ApplyResult{
			VMID:         existing.ID,
			Status:       existing.Status,
			PublicIP:     ip,
			FloatingIPID: fipID,
			SSH:          sshString(plan.SSH.Login, ip),
			ElapsedSec:   int(clock.Since(start).Seconds()),
			Note:         "VM already existed — adopted (idempotent), nothing recreated; it is " + existing.Status + ": start it in the Cloud.ru console, then call deploy_apply again; whether to delete it instead (destroy) — ask the person",
		}, nil
	}

	// --- Build image_metadata (password generated here, never persisted) ---
	// Start from the image's required defaults (copied verbatim), then fill the
	// dynamic fields. Omitting the defaults yields image_metadata_fields_missing.
	meta := map[string]string{}
	for k, v := range plan.MetaStatic {
		meta[k] = v
	}
	// MetaFields are the access-password fields (linux_password, or the
	// plain_password of a Windows image). Plans saved before internal/images
	// also list name, hostname and public_key there: they are still filled.
	password := ""
	for _, f := range plan.MetaFields {
		switch f {
		case "name":
			meta["name"] = plan.SSH.Login
		case "hostname":
			meta["hostname"] = plan.VM.Name
		case "public_key":
			if plan.PublicKey != "" {
				meta["public_key"] = plan.PublicKey
			}
		default:
			// One password for every password field (see images.FillMetadata).
			if password == "" {
				password = generatePassword(16)
			}
			meta[f] = password
		}
	}

	// --- Build the create request (mirrors the proven v1.1 body) ---
	iface := provision.InterfaceReq{
		Type:                     "regular",
		SubnetID:                 plan.Network.SubnetID,
		InterfaceSecurityEnabled: true,
	}
	if plan.Network.SecurityGroupID != "" {
		iface.SecurityGroups = []string{plan.Network.SecurityGroupID}
	}
	if plan.Network.FloatingIP && plan.Network.AttachFIPID != "" {
		iface.AttachExternalIPID = plan.Network.AttachFIPID // variant A
	}

	req := provision.CreateVMRequest{
		ProjectID:          plan.ProjectID,
		Name:               plan.VM.Name,
		FlavorName:         plan.VM.Flavor.Name,
		ImageName:          plan.VM.Image.Name,
		AvailabilityZoneID: plan.VM.Zone.ID,
		Disks: []provision.DiskReq{{
			Name:         provision.FitName(plan.VM.Name, "-disk"),
			Size:         plan.VM.Disk.SizeGB,
			DiskTypeName: firstNonEmpty(plan.VM.Disk.Type, "SSD"),
		}},
		Interfaces: []provision.InterfaceReq{iface},
	}
	if len(meta) > 0 {
		req.ImageMetadata = meta
	}

	show.step(i18n.Tf("Creating VM %s…", "Создаю ВМ %s…", plan.VM.Name))
	createStart := time.Now()
	vm, err := provision.CreateVMChecked(ctx, cloud, req)
	if err != nil {
		// The VM can exist although the call failed (timeout, cancel, a
		// lost answer): name it, so the caller records it, and a retry of
		// the plan adopts it. Not after a refusal (4xx, a 409 for a name
		// taken since the check above): then the VM by that name is
		// another one (release audit, L-01).
		if provision.CreateRejected(err) {
			return nil, fmt.Errorf(i18n.T("create VM: %w", "создание ВМ: %w"), err)
		}
		if found := provision.FindCreatedVM(cloud, plan.ProjectID, plan.VM.Name, createStart); found != nil {
			return &ApplyResult{
					VMID: found.ID, Status: "interrupted",
					ElapsedSec: int(clock.Since(start).Seconds()),
				}, fmt.Errorf("VM %s was created, but the create call failed; it is billed — "+
					"call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), "+
					"whether to delete it instead (destroy) — ask the person: %w", found.ID, err)
		}
		return nil, fmt.Errorf(i18n.T("create VM: %w", "создание ВМ: %w"), err)
	}

	show.done(i18n.Tf("Request accepted: VM %s, id %s", "Запрос принят: ВМ %s, id %s", plan.VM.Name, vm.ID))
	return finishApply(ctx, cloud, plan, vm, password, start, pollTimeout, until, show, "")
}

// finishApply waits for the VM to run, then gives it its floating IP, waits
// for SSH and opens the plan's ports — for a VM just created and for one a
// retry adopts. note goes first in the answer's note.
func finishApply(ctx context.Context, cloud provision.CloudAPI, plan *Plan, vm *provision.VM, password string, start time.Time, pollTimeout time.Duration, until time.Time, show *Progress, note string) (*ApplyResult, error) {
	// Cancelled from here on (the MCP client went away or cancelled the
	// call): the VM exists and is billed. The result names it, so the
	// caller can record it in the state, together with an error that says
	// what to do with it.
	interrupted := func(ip, fipID string) (*ApplyResult, error) {
		return &ApplyResult{
				VMID: vm.ID, Status: "interrupted", PublicIP: ip, FloatingIPID: fipID,
				// Made for this VM and kept nowhere: the repeat that adopts
				// the VM cannot give it (D69).
				Password:   password,
				ElapsedSec: int(clock.Since(start).Seconds()),
			}, fmt.Errorf("VM %s was created, but deploy_apply was interrupted; it is still running — "+
				"call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), "+
				"check it with vm_get; whether to delete it instead (destroy) — ask the person: %w", vm.ID, ctx.Err())
	}

	// --- Poll until running ---
	status := "building"
	if vm.Status == "running" || vm.Status == "active" {
		status = "running"
	}
	deadline := time.After(pollTimeout)
	// The budget of the call (D69), when it ends before pollTimeout.
	var budget <-chan time.Time
	if !until.IsZero() {
		budget = time.After(time.Until(until))
	}
	vmStatus := vm.Status
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var shown <-chan time.Time // the wait's own ticks, when it is shown
	if show != nil && show.Wait != nil {
		shown = show.Wait.C()
		show.Wait.State(vm.Status)
	}
poll:
	for status != "running" {
		select {
		case <-shown:
			show.Wait.Show()
		case <-ctx.Done():
			// The VM exists: say which, or nobody will find it.
			return interrupted("", "")
		case <-budget:
			// The VM is still being made: its state so far, and the
			// password, which the repeat that adopts the VM cannot give.
			return &ApplyResult{
				VMID: vm.ID, Status: "creating", VMStatus: vmStatus, Password: password,
				ElapsedSec: int(clock.Since(start).Seconds()),
			}, nil
		case <-deadline:
			return &ApplyResult{
				VMID: vm.ID, Status: "timeout", Password: password,
				ElapsedSec: int(clock.Since(start).Seconds()),
				Note:       fmt.Sprintf("VM did not reach running within %s; check status later with the status tool", pollTimeout),
			}, nil
		case <-ticker.C:
			cur, e := cloud.GetVM(ctx, vm.ID)
			if e != nil {
				continue
			}
			vmStatus = cur.Status
			if shown != nil {
				show.Wait.State(cur.Status)
			}
			switch cur.Status {
			case "running", "active":
				status = "running"
				break poll
			}
			if provision.VMFailed(cur.Status) {
				return &ApplyResult{
					VMID: vm.ID, Status: "error", Password: password,
					ElapsedSec: int(clock.Since(start).Seconds()),
					Note:       "VM entered state " + cur.Status + "; whether to delete it (destroy) and retry — ask the person",
				}, nil
			}
		}
	}

	if show != nil && show.Wait != nil {
		show.Wait.Done()
	}
	show.done(i18n.Tf("VM %s is running", "ВМ %s запущена", plan.VM.Name))

	// --- Floating IP ---
	var lostFIP string
	ip, fipID, fipNote := ensureFloatingIP(ctx, cloud, plan, vm.ID, &lostFIP)
	if ip != "" {
		show.done(i18n.Tf("Public IP %s", "Публичный IP %s", ip))
	}
	if ctx.Err() != nil {
		// The steps below would only fail on the cancelled context and
		// report a half-done VM as "running".
		r, err := interrupted(ip, fipID)
		if r != nil {
			r.LostFloatingIP = lostFIP
		}
		return r, err
	}

	// --- Wait until SSH (port 22) is actually reachable, so the agent's very
	// first ssh attempt succeeds instead of hitting a boot-time timeout (which
	// has been misread as a "security group blocks 22" problem). Bounded so the
	// tool call stays well under the MCP timeout. ---
	sshNote := ""
	if ip != "" {
		show.done(i18n.Tf("Waiting for SSH on %s…", "Жду SSH на %s…", ip))
		sshWait := SSHPortWait
		if !until.IsZero() {
			sshWait = min(sshWait, time.Until(until))
		}
		if open, seen := waitForSSHPort(ctx, ip, sshWait, show); open {
			sshNote = "SSH port 22 is open — connect now with the ssh command above."
		} else {
			sshNote = sshUnreachedNote(ip, sshWait, seen)
		}
	}

	// --- Open the product's ports in the security group (auto). Same code
	// as the CLI deploy: the rules created are recorded for destroy. ---
	var ports state.DeploymentRecord
	opened := sgrules.Open(ctx, cloud, plan.ProjectID, plan.Network.SecurityGroupID, plan.Ports, nil, plan.PortsSource, vm.ID, &ports)
	if show != nil && show.Ports != nil {
		show.Ports(opened, &ports)
	}

	return &ApplyResult{
		VMID:           vm.ID,
		Status:         status,
		PublicIP:       ip,
		FloatingIPID:   fipID,
		LostFloatingIP: lostFIP,
		SSH:            sshString(plan.SSH.Login, ip),
		Password:       password,
		ElapsedSec:     int(clock.Since(start).Seconds()),
		Note:           joinNotes(note, fipNote, sshNote, opened.Note()),
		Ports:          ports,
	}, nil
}

// SSHPortWait bounds the wait for port 22 after the VM runs. Tests (here and
// in the packages that drive deploy_apply) shorten it.
var SSHPortWait = 120 * time.Second

// DialSSH is the one check of port 22 the wait makes: a TCP connect from
// this machine to addr within timeout, closed at once. A test replaces it,
// so that no test connects to a network outside its own process: a
// documentation address that a proxy or a VPN answers made a test of the
// branch «no answer» fail (#1707).
var DialSSH = func(_ context.Context, addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

// portState is what the check of port 22 saw last.
type portState int

const (
	portUnchecked portState = iota
	// portRefused: something at the address answered and refused the
	// connection — no sshd listens yet.
	portRefused
	// portSilent: no answer within the check: the VM may still boot, or
	// a rule on the way (a security group) drops the packets — one check
	// from here does not tell which.
	portSilent
)

// waitForSSHPort polls TCP port 22 on ip until it accepts a connection or the
// timeout elapses. Returns true if the port became reachable, else what the
// last check saw. It says the port's state on show when it changes, as seen
// from this machine (CLAUDE.md §6, axis 2).
func waitForSSHPort(ctx context.Context, ip string, timeout time.Duration, show *Progress) (bool, portState) {
	deadline := time.Now().Add(timeout)
	last := portUnchecked
	report := func(err error) {
		state, word := portRefused, i18n.T("connection refused", "отказ в соединении")
		if ne := net.Error(nil); errors.As(err, &ne) && ne.Timeout() {
			state, word = portSilent, i18n.T("no answer", "нет ответа")
		}
		if state != last {
			last = state
			show.step(i18n.Tf("SSH on %s, checked from this machine: %s; waiting…", "SSH на %s, проверка с этой машины: %s; жду…", ip, word))
		}
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, last
		default:
		}
		step := min(5*time.Second, time.Until(deadline))
		err := DialSSH(ctx, net.JoinHostPort(ip, "22"), step)
		if err == nil {
			return true, last
		}
		report(err)
		select {
		case <-ctx.Done():
			return false, last
		case <-time.After(min(5*time.Second, max(time.Until(deadline), 0))):
		}
	}
	return false, last
}

// sshUnreachedNote is the note of an apply whose wait ended with port 22
// not open: the fact the check saw and where from, not a cause it did not
// see — no answer comes from a VM still booting and from a rule that drops
// the packets alike (#1707).
func sshUnreachedNote(ip string, waited time.Duration, state portState) string {
	switch state {
	case portUnchecked:
		return "SSH port 22 of " + ip + " was not checked: the call had no time left for it. Retry the same ssh command in about a minute; do NOT recreate the VM."
	case portRefused:
		return "SSH port 22 of " + ip + " refused the connection when evo checked it from this machine: the address answers, but sshd does not listen yet — the VM is still starting. Retry the same ssh command in about a minute; do NOT recreate the VM."
	}
	return "SSH port 22 of " + ip + " did not answer evo's TCP check from this machine within " + waited.Round(time.Second).String() +
		": the VM may still be booting, or a security group or a network on the way may drop the packets — this check does not tell which. Retry the same ssh command in about a minute; if it still gets no answer, check the rules for port 22 of the VM's security group (list_security_groups) against the address you connect from. Do NOT recreate the VM."
}

// ensureFloatingIP returns the VM's public IP, its floating IP id and a
// note. The floating IP is evo's own, as in the CLI deploy: one already on
// the VM (a retried deploy_apply adopts the VM with it — a second FIP used
// to be created, QA r3 R2), the one a plan of an older version named, or a
// new one. A free FIP of the user is never taken over: destroy releases the
// deployment's FIP, and it used to release the user's reserved address with
// it (QA r3 R9).
// attachedFloatingIP is the floating IP already on the VM, if it can be read.
func attachedFloatingIP(ctx context.Context, cloud provision.CloudAPI, vmID string) (ip, id string) {
	detail, err := cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return "", ""
	}
	return detail.AttachedFloatingIP()
}

// lostName is set to the name of an address whose create got no answer.
func ensureFloatingIP(ctx context.Context, cloud provision.CloudAPI, plan *Plan, vmID string, lostName *string) (ip, fipID, note string) {
	if !plan.Network.FloatingIP {
		return "", "", ""
	}
	detail, err := cloud.GetVMDetail(ctx, vmID)
	if err == nil {
		if ip, id := detail.AttachedFloatingIP(); ip != "" {
			return ip, id, "public IP attached"
		}
	}
	// Plans written by older versions may name a FIP attached at creation.
	if plan.Network.AttachFIPID != "" {
		if f, gerr := cloud.GetFloatingIP(ctx, plan.Network.AttachFIPID); gerr == nil && f.IPAddress != "" {
			return f.IPAddress, f.ID, "public IP attached (existing floating IP)"
		}
	}
	if err != nil || len(detail.Interfaces) == 0 {
		return "", "", "could not read the VM interface yet — attach a floating IP later via the Cloud.ru console"
	}
	ifaceID := detail.Interfaces[0].ID
	for _, in := range detail.Interfaces {
		if in.Primary {
			ifaceID = in.ID
			break
		}
	}

	name := FloatingIPName(plan)
	// This VM's address only: bound to it or to its interface (the list may
	// name only one), or still being created or attached — the create named
	// the interface. A free one of the name is an orphan of an earlier VM,
	// not this VM's public IP (Cursor 1.2.3, A3).
	own := func(x provision.FloatingIP) bool { return x.BoundTo(vmID) || x.BoundTo(ifaceID) || x.Pending() }
	// One an earlier apply asked for with no answer, listed now, is taken:
	// not a second create (D107; «Риск», conclusion 180, У2).
	if x := floatingip.Find(ctx, cloud, plan.ProjectID, provision.FitName(plan.VM.Name, "-fip"), own); x != nil {
		return x.IPAddress, x.ID, "public IP attached (found the floating IP an earlier apply asked for by its name)"
	}
	// One create: with no answer it is looked for by its name, and not
	// sent again (floatingip.Reserve; conclusion 180, У1).
	f, e := floatingip.Reserve(ctx, cloud, provision.CreateFloatingIPRequest{
		Name:               name,
		ProjectID:          plan.ProjectID,
		InterfaceID:        ifaceID,
		AvailabilityZoneID: plan.VM.Zone.ID,
	}, own, time.Second, nil)
	if e == nil && f != nil && f.ID != "" {
		return f.IPAddress, f.ID, "public IP attached (created a new floating IP)"
	}
	if provision.IsQuotaError(e) {
		return "", "", "⚠️ Floating-IP quota exhausted — the VM was created WITHOUT a public IP. Increase the floating-IP quota in the Cloud.ru console, then attach an IP to this VM manually."
	}
	if lost, ok := floatingip.AsLost(e); ok {
		*lostName = lost.Name
		return "", "", lostFIPNote(lost.Name, lost.Err)
	}
	// A refusal: no address came of it. deploy_apply again adopts the VM
	// with the address it has, or attaches a new one.
	return "", "", "could not attach a floating IP (" + e.Error() + ") — the VM has no public IP evo could confirm; run deploy_apply again with the same plan: it adopts the VM and the floating IP already on it, or attaches a new one"
}

// lostFIPNote is the note of an apply whose create of the VM's floating IP
// got no answer: the address may exist and be billed, and no second one
// was requested (ApplyResult.LostFloatingIP names it).
func lostFIPNote(name string, err error) string {
	return "the create of floating IP " + name + " got no answer from the cloud (" + err.Error() + "): the request may have been accepted, and the address billed. No second address was requested. Check list_floating_ips for the name " + name +
		"; deploy_apply again with the same plan takes that address if the list shows it on this VM; if the list does not show it, it asks first — whether to reserve another address is the person's to answer"
}

// FloatingIPName is the name the plan's VM's floating IP is created by:
// plan.FloatingIPName when the tool set one, else the plan's own.
func FloatingIPName(plan *Plan) string {
	if plan.FloatingIPName != "" {
		return plan.FloatingIPName
	}
	return provision.FitName(plan.VM.Name, "-fip")
}

// joinNotes concatenates non-empty notes with "; ".
func joinNotes(notes ...string) string {
	var parts []string
	for _, n := range notes {
		if n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "; ")
}

// ownVM reports whether vm, which has the plan's name, is the plan's to
// adopt (D73): the folder's state records it, or it was made after the
// plan — the VM of a create of this plan whose answer was lost.
func ownVM(plan *Plan, vm *provision.VM) bool {
	return slices.Contains(plan.Owned, vm.ID) || provision.MadeSince(vm, plan.CreatedAt)
}

// ForeignVMError refuses a plan whose VM name another VM has, one the
// folder did not make (D73): its id goes into the answer (existing_id).
type ForeignVMError struct {
	ID, Name, Status string
}

func (e *ForeignVMError) Error() string {
	return fmt.Sprintf("a VM named %q already exists (id %s, status %s), and this folder's evo did not create it (its state does not record it); nothing was created", e.Name, e.ID, e.Status)
}

// findVMByName looks the plan's VM up in its project. GET /vms without a
// project_id returns no VMs (.github/review-context/cloudru-api.md), so the
// adoption that a retry of deploy_apply promises never happened and the
// retry created a second VM (QA H5).
func findVMByName(ctx context.Context, cloud provision.CloudAPI, projectID, name string) (*provision.VM, error) {
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range vms {
		if v.Name == name {
			return v, nil
		}
	}
	return nil, nil
}

func sshString(login, ip string) string {
	if ip == "" {
		return ""
	}
	return fmt.Sprintf("ssh %s@%s", firstNonEmpty(login, "user1"), ip)
}

// generatePassword returns a random password of n chars that is GUARANTEED to
// contain at least one lowercase, one uppercase, one digit and one special
// character. Cloud.ru rejects passwords without a digit
// ("the password must contain at least one number"), so a purely random draw is
// not safe — we seed one char from each class, then fill and shuffle.
// Ambiguous characters (0/O/1/l/I) are avoided.
func generatePassword(n int) string {
	if n < 8 {
		n = 8
	}
	const (
		lower   = "abcdefghijkmnpqrstuvwxyz"
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digits  = "23456789"
		special = "!@#%-_+="
	)
	all := lower + upper + digits + special

	pw := make([]byte, 0, n)
	pw = append(pw, pickChar(lower), pickChar(upper), pickChar(digits), pickChar(special))
	for len(pw) < n {
		pw = append(pw, pickChar(all))
	}
	// Fisher-Yates shuffle so the seeded classes aren't in fixed positions.
	for i := len(pw) - 1; i > 0; i-- {
		j := randIntn(i + 1)
		pw[i], pw[j] = pw[j], pw[i]
	}
	return string(pw)
}

func pickChar(set string) byte {
	return set[randIntn(len(set))]
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}
