// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"slices"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type receiptCloud struct {
	*faultCloud
	reads, deletes []string
	readErr        error
}

func (c *receiptCloud) GetFloatingIP(ctx context.Context, id string) (*provision.FloatingIP, error) {
	c.reads = append(c.reads, id)
	if c.readErr != nil {
		return nil, c.readErr
	}
	f, err := c.faultCloud.GetFloatingIP(ctx, id)
	if clerrors.Reason(err) == clerrors.CodeVMNotFound {
		return nil, clerrors.Wrap(clerrors.CodeNotFound, "not found", "", &provision.StatusError{Status: 404, Body: `{"message":"floating IP not found"}`})
	}
	return f, err
}

func (c *receiptCloud) DeleteFloatingIP(ctx context.Context, id, project string) error {
	c.deletes = append(c.deletes, id)
	return c.faultCloud.DeleteFloatingIP(ctx, id, project)
}

const oldReceiptVM = "00000000-0000-4000-8000-000000000071"
const oldReceiptAddress = "00000000-0000-4000-8000-000000000072"
const foreignReceiptAddress = "00000000-0000-4000-8000-000000000073"

// Seed a directory as it would exist before recovery. Losing a create reply,
// where applicable, happens through Deploy; all further state changes are made by evo.
func unfinishedAddress(t *testing.T, scenario string) (*Engine, *receiptCloud, string) {
	t.Helper()
	faultIsolate(t)
	c := &receiptCloud{faultCloud: &faultCloud{vms: []faultVM{{ID: oldReceiptVM, Name: "old-vm", Status: "active"}}}}
	e, _, dir := faultEngine(t, c.faultCloud, &faultDeployer{})
	e.Cloud = c
	rec := state.DeploymentRecord{ID: oldReceiptVM, VMID: oldReceiptVM, VMName: "old-vm", ProjectID: faultProject, Region: faultZone, Status: "running"}
	sf := &state.File{Version: 1}
	name := fipName(oldReceiptVM)
	switch scenario {
	case "legacy address":
		rec.VMIP = "198.51.100.73"
	case "name-only receipt":
		sf.LostFloatingIPs = []state.LostFloatingIP{{Name: name, VMID: oldReceiptVM, ProjectID: faultProject}}
	case "lost create reply":
		c.failNth("CreateFloatingIP", 1, faultLostNameless)
	default:
		t.Fatalf("unknown fixture %q", scenario)
	}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if scenario == "lost create reply" {
		err := faultDeploy(t, e, dir)
		lost, ok := floatingip.AsLost(err)
		got := faultState(t, dir).LostFloatingIPs
		if !ok || lost.Attempt == "" || len(got) != 1 || got[0].Attempt != lost.Attempt || got[0].ID != "" || got[0].Name != name || len(c.liveFIPs()) != 1 {
			t.Fatalf("lost response did not produce an unresolved receipt: err=%v receipts=%+v world=%+v", err, got, c.liveFIPs())
		}
	}
	// The old address has been recycled, or another free address has the same
	// name as the failed request. Neither circumstance proves ownership.
	c.fips = append(c.fips, faultFIP{ID: foreignReceiptAddress, Name: name, IP: "198.51.100.73", State: "available"})
	return e, c, dir
}

func assertAddressStillPending(t *testing.T, e *Engine, c *receiptCloud, dir string) {
	t.Helper()
	var result DestroyResult
	if err := e.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result}); err != nil {
		t.Fatal(err)
	}
	active := faultState(t, dir).ActiveDeployment()
	if !result.CleanupPending || active == nil || active.ID != oldReceiptVM || active.Status != "destroying" || len(c.deletes) != 0 {
		t.Fatalf("want unresolved deployment and no address DELETE; result=%+v active=%+v DELETE=%v", result, active, c.deletes)
	}
	before := len(c.callLog())
	if err := faultDeploy(t, e, dir); err == nil {
		t.Fatal("deploy passed while destroy remained pending")
	}
	if now := c.callLog()[before:]; slices.Contains(now, "CreateVM") || slices.Contains(now, "CreateFloatingIP") {
		t.Fatalf("deploy started new resources before reconciliation: %v", now)
	}
}

func TestDestroy_UnconfirmedAddressSurvivesEmptyAndSameNameLists(t *testing.T) {
	for _, scenario := range []string{"legacy address", "name-only receipt", "lost create reply"} {
		t.Run(scenario, func(t *testing.T) {
			e, c, dir := unfinishedAddress(t, scenario)
			assertAddressStillPending(t, e, c, dir)
			before := faultState(t, dir).LostFloatingIPs
			// A subsequent list is behind the cloud; no address is actually removed.
			c.behind = map[string]int{}
			for _, f := range c.fips {
				c.behind[f.ID] = 50
			}
			assertAddressStillPending(t, e, c, dir)
			after := faultState(t, dir).LostFloatingIPs
			if (scenario != "legacy address" && (len(before) != 1 || len(after) != 1 || after[0].Name != before[0].Name || after[0].Attempt != before[0].Attempt)) || len(c.liveFIPs()) == 0 {
				t.Fatalf("empty list lost a receipt: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestDestroy_ReconcileFloatingIPAndRedeploy(t *testing.T) {
	for _, scenario := range []string{"legacy address", "name-only receipt", "lost create reply"} {
		t.Run(scenario, func(t *testing.T) {
			e, c, dir := unfinishedAddress(t, scenario)
			assertAddressStillPending(t, e, c, dir)
			receipts, err := e.PendingFloatingIPReceipts(oldReceiptVM)
			if err != nil || len(receipts) != 1 {
				t.Fatalf("expected one address to verify, got %+v (%v)", receipts, err)
			}
			receipt := receipts[0]
			kind := "lost_request"
			if scenario == "legacy address" {
				kind = "legacy_deployment"
			}
			if receipt.Kind != kind || receipt.ReceiptID == "" || receipt.DeploymentID != oldReceiptVM || receipt.ProjectID != faultProject {
				t.Fatalf("receipt missing its exact scope: %+v", receipt)
			}
			req := FloatingIPReconciliation{ReceiptID: receipt.ReceiptID, DeploymentID: receipt.DeploymentID, Outcome: "not_created", Evidence: "provider support case 12345"}
			if scenario != "name-only receipt" {
				req.Outcome = "released"
				req.AddressID = oldReceiptAddress
				if scenario == "lost create reply" {
					own := c.liveFIPs()[0]
					if own.Description != receipt.Attempt || own.ID == foreignReceiptAddress {
						t.Fatalf("wrong address for the create attempt: %+v", own)
					}
					req.AddressID = own.ID
				}
			}
			// A bare name does not establish the outcome of this request.
			unchecked := req
			for _, evidence := range []string{"", fipName(oldReceiptVM), receipt.DeploymentID, receipt.ReceiptID, "[]", req.AddressID} {
				unchecked.Evidence = evidence
				if err := e.ReconcileFloatingIP(t.Context(), unchecked); err == nil {
					t.Fatalf("accepted %q as independent evidence", evidence)
				}
			}
			invalid := req
			invalid.Outcome, invalid.AddressID = "released", fipName(oldReceiptVM)
			if err := e.ReconcileFloatingIP(t.Context(), invalid); err == nil || slices.Contains(c.reads, invalid.AddressID) {
				t.Fatalf("accepted a name as an address ID or sent GET by name: err=%v reads=%v", err, c.reads)
			}
			if req.Outcome == "released" {
				wrong := req
				wrong.AddressID = foreignReceiptAddress
				if err := e.ReconcileFloatingIP(t.Context(), wrong); err == nil {
					t.Fatal("accepted a currently live address owned by someone else")
				}
			}
			if scenario == "lost create reply" {
				// An accepted create really created a resource although its response was lost.
				// The card by ID is still present. After outside verification it is released
				// out of band in the simulated provider; evo never issues this DELETE.
				notCreated := req
				notCreated.Outcome, notCreated.AddressID = "not_created", ""
				if err := e.ReconcileFloatingIP(t.Context(), notCreated); err == nil {
					t.Fatal("accepted not_created while a live address carries this attempt marker")
				}
				if err := e.ReconcileFloatingIP(t.Context(), req); err == nil {
					t.Fatal("accepted released while the address is still visible by ID")
				}
				c.fips[0].deleted = true
			}
			again, err := e.PendingFloatingIPReceipts(oldReceiptVM)
			if err != nil || len(again) != 1 || again[0].ReceiptID != req.ReceiptID {
				t.Fatalf("unconfirmed evidence changed the receipt: %+v (%v)", again, err)
			}
			c.reads = nil
			if err := e.ReconcileFloatingIP(t.Context(), req); err != nil {
				t.Fatalf("confirmed reconciliation: %v", err)
			}
			if req.Outcome == "released" && !slices.Contains(c.reads, req.AddressID) {
				t.Fatalf("release was not checked by the supplied ID: reads=%v", c.reads)
			}
			if left, err := e.PendingFloatingIPReceipts(oldReceiptVM); err != nil || len(left) != 0 {
				t.Fatalf("confirmed receipt still pending: %+v (%v)", left, err)
			}
			// A retried confirmation cannot use another same-name address as a new target.
			if err := e.ReconcileFloatingIP(t.Context(), req); clerrors.Reason(err) != clerrors.CodeNotFound {
				t.Fatalf("repeated resolution of the closed receipt: %v, want NOT_FOUND", err)
			}
			var result DestroyResult
			if err := e.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result}); err != nil || result.CleanupPending || result.VMID != oldReceiptVM {
				t.Fatalf("destroy after reconciliation: result=%+v err=%v", result, err)
			}
			if sf := faultState(t, dir); sf.ActiveDeployment() != nil || len(sf.LostFloatingIPs) != 0 {
				t.Fatalf("unfinished local cleanup after destroy: %+v", sf)
			}
			if err := faultDestroy(t, e); err != nil {
				t.Fatalf("repeated destroy: %v", err)
			}
			if len(c.deletes) != 0 || len(c.liveFIPs()) != 1 || c.liveFIPs()[0].ID != foreignReceiptAddress {
				t.Fatalf("foreign address changed during recovery: DELETE=%v live=%+v", c.deletes, c.liveFIPs())
			}
			if err := faultDeploy(t, e, dir); err != nil {
				t.Fatalf("new deploy in same directory: %v", err)
			}
			if now := faultState(t, dir).ActiveDeployment(); now == nil || now.Status != "running" || now.VMID == oldReceiptVM || now.FloatingIPID == foreignReceiptAddress {
				t.Fatalf("redeploy did not start with its own resources: %+v", now)
			}
		})
	}
}

// Only the selected attempt can be reconciled. A gateway 404 does not say
// whether this address exists in the project.
func TestReconcileFloatingIP_ExactReceiptAndServiceRead(t *testing.T) {
	faultIsolate(t)
	c := &receiptCloud{faultCloud: &faultCloud{}}
	e, _, dir := faultEngine(t, c.faultCloud, &faultDeployer{})
	e.Cloud = c
	one := state.LostFloatingIP{Name: "same-name", Attempt: "first-request", ProjectID: faultProject, VMID: oldReceiptVM, ID: oldReceiptAddress}
	other := one
	other.Attempt = "second-request"
	other.ID = ""
	f := &state.File{Version: 1, LostFloatingIPs: []state.LostFloatingIP{one, other}}
	f.UpsertDeployment(state.DeploymentRecord{ID: oldReceiptVM, VMID: oldReceiptVM, ProjectID: faultProject, Status: "destroying"})
	if err := state.Write(state.DefaultPath(dir), f); err != nil {
		t.Fatal(err)
	}
	receipts, err := e.PendingFloatingIPReceipts(oldReceiptVM)
	if err != nil || len(receipts) != 2 {
		t.Fatalf("expected two unresolved attempts: %+v (%v)", receipts, err)
	}
	req := FloatingIPReconciliation{DeploymentID: oldReceiptVM, Outcome: "released", AddressID: oldReceiptAddress, Evidence: "provider support case 45678"}
	var otherReceipt string
	for _, r := range receipts {
		switch r.Attempt {
		case one.Attempt:
			req.ReceiptID = r.ReceiptID
		case other.Attempt:
			otherReceipt = r.ReceiptID
		}
	}
	if req.ReceiptID == "" || otherReceipt == "" || req.ReceiptID == otherReceipt {
		t.Fatalf("two attempts share a receipt identity: %+v", receipts)
	}
	for _, tc := range []struct {
		name, outcome, id string
		readErr           error
	}{
		{"known ID cannot be not created", "not_created", "", nil},
		{"different ID", "released", foreignReceiptAddress, nil},
		{"gateway 404", "released", oldReceiptAddress, clerrors.Wrap(clerrors.CodeAPIError, "missing route", "", &provision.StatusError{Status: 404, Body: `{"error_msg":"Not Found"}`})},
		{"access refused", "released", oldReceiptAddress, clerrors.New(clerrors.CodeAuthFailed, "denied", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempt := req
			attempt.Outcome, attempt.AddressID = tc.outcome, tc.id
			c.readErr = tc.readErr
			if err := e.ReconcileFloatingIP(t.Context(), attempt); err == nil {
				t.Fatal("receipt cleared without a verified release of the recorded ID")
			}
			if got := faultState(t, dir).LostFloatingIPs; len(got) != 2 {
				t.Fatalf("failed verification discarded a receipt: %+v", got)
			}
		})
	}
	c.readErr = nil
	if err := e.ReconcileFloatingIP(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	left, err := e.PendingFloatingIPReceipts(oldReceiptVM)
	if err != nil || len(left) != 1 || left[0].ReceiptID != otherReceipt || len(c.deletes) != 0 {
		t.Fatalf("resolving one attempt changed the other or issued DELETE: left=%+v err=%v deletes=%v", left, err, c.deletes)
	}
}

func TestCleanup_LegacyReceiptProject(t *testing.T) {
	const otherProject = "00000000-0000-4000-8000-000000000075"
	for _, recorded := range []bool{false, true} {
		name := "independent project"
		if recorded {
			name = "recorded in lost request"
		}
		t.Run(name, func(t *testing.T) {
			faultIsolate(t)
			c := &receiptCloud{faultCloud: &faultCloud{}}
			e, _, dir := faultEngine(t, c.faultCloud, &faultDeployer{})
			e.Cloud = c
			d := state.DeploymentRecord{ID: oldReceiptVM, VMID: oldReceiptVM, VMName: "old-vm", Status: "destroying"}
			f := &state.File{Version: 1}
			if recorded {
				f.LostFloatingIPs = []state.LostFloatingIP{{Name: fipName(oldReceiptVM), VMID: oldReceiptVM, ProjectID: otherProject}}
			} else {
				d.VMIP = "198.51.100.73"
			}
			f.UpsertDeployment(d)
			if err := state.Write(state.DefaultPath(dir), f); err != nil {
				t.Fatal(err)
			}
			if recorded {
				// The profile points to a second project while an old deployment was
				// already destroying. The lost request still names its original project.
				var result DestroyResult
				err := e.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result})
				sf := faultState(t, dir)
				if err == nil && !result.CleanupPending || sf.ActiveDeployment() == nil || len(sf.LostFloatingIPs) != 1 || slices.Contains(c.callLog(), "DeleteVM") || len(c.deletes) != 0 {
					t.Fatalf("wrong profile retired the target or sent a DELETE: result=%+v err=%v active=%+v marks=%+v calls=%v", result, err, sf.ActiveDeployment(), sf.LostFloatingIPs, c.callLog())
				}
				e.Config.ProjectID = otherProject
			}
			rs, err := e.PendingFloatingIPReceipts(oldReceiptVM)
			if err != nil || len(rs) != 1 || rs[0].NeedsEvidenceProject == recorded || rs[0].ProjectID != e.Config.ProjectID {
				t.Fatalf("unexpected project evidence: receipts=%+v err=%v", rs, err)
			}
			req := FloatingIPReconciliation{ReceiptID: rs[0].ReceiptID, DeploymentID: d.ID, Outcome: "not_created", Evidence: "provider support case 333"}
			if !recorded {
				req.Outcome, req.AddressID = "released", oldReceiptAddress
				for _, project := range []string{"", otherProject} {
					req.EvidenceProjectID = project
					if err := e.ReconcileFloatingIP(t.Context(), req); err == nil {
						t.Fatalf("accepted independent evidence from project %q", project)
					}
				}
				req.EvidenceProjectID = faultProject
			}
			if err := e.ReconcileFloatingIP(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			if err := faultDestroy(t, e); err != nil || faultState(t, dir).ActiveDeployment() != nil || len(c.deletes) != 0 {
				t.Fatalf("cleanup after verified project: err=%v deletes=%v", err, c.deletes)
			}
		})
	}
}

// The owner may have previewed a resolution before an addressed destroy
// observed another binding. Validate the current receipt under the state lock.
func TestReconcileFloatingIP_ConflictingBindingAfterPreview(t *testing.T) {
	e, c, dir := unfinishedAddress(t, "legacy address")
	assertAddressStillPending(t, e, c, dir)
	rs, err := e.PendingFloatingIPReceipts(oldReceiptVM)
	if err != nil || len(rs) != 1 {
		t.Fatalf("receipts: %v %v", rs, err)
	}
	original := FloatingIPReconciliation{DeploymentID: oldReceiptVM, ReceiptID: rs[0].ReceiptID, Outcome: "released", AddressID: oldReceiptAddress, Evidence: "https://Records.example/Reports/AbC"}
	if err := e.ReconcileFloatingIP(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	// This is a different address ID, now bound to the exact VM and carrying
	// its recorded public address. Later it detaches, but remains live and billed.
	c.fips[0].VMID = oldReceiptVM
	assertAddressStillPending(t, e, c, dir)
	c.fips[0].VMID = ""
	assertAddressStillPending(t, e, c, dir)
	for _, tc := range []struct{ name, outcome, id, evidence, reason string }{
		{"same evidence", "released", foreignReceiptAddress, original.Evidence, clerrors.CodeUsage},
		{"same host and scheme", "released", foreignReceiptAddress, "HTTPS://RECORDS.EXAMPLE/Reports/AbC", clerrors.CodeUsage},
		{"stale verified ID", "released", oldReceiptAddress, "new case 102", clerrors.CodeAPIError},
		{"known address cannot be treated as never created", "not_created", "", "new case 102", clerrors.CodeAPIError},
		{"observed address still exists", "released", foreignReceiptAddress, "new case 102", clerrors.CodeAPIError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := original
			r.Outcome, r.AddressID, r.Evidence = tc.outcome, tc.id, tc.evidence
			if err := e.ReconcileFloatingIP(t.Context(), r); clerrors.Reason(err) != tc.reason {
				t.Fatalf("rechecked old preview: want %s, got %v", tc.reason, err)
			}
			d := faultState(t, dir).ActiveDeployment()
			if d == nil || len(d.FloatingIPResolutions) != 1 || !d.FloatingIPResolutions[0].Contradicted || d.FloatingIPResolutions[0].Evidence != original.Evidence || len(c.deletes) != 0 {
				t.Fatalf("rejected proof changed the audit or released an address: %+v DELETE=%v", d, c.deletes)
			}
		})
	}
	c.fips[0].deleted = true // provider records the externally verified release
	verified := original
	verified.AddressID, verified.Evidence = foreignReceiptAddress, "https://records.example/Reports/abc"
	if err := e.ReconcileFloatingIP(t.Context(), verified); err != nil {
		t.Fatal(err)
	}
	d := faultState(t, dir).ActiveDeployment()
	if len(d.FloatingIPResolutions) != 2 || !d.FloatingIPResolutions[0].Contradicted || d.FloatingIPResolutions[0].Evidence != original.Evidence ||
		d.FloatingIPResolutions[1].Contradicted || d.FloatingIPResolutions[1].Evidence != verified.Evidence {
		t.Fatalf("old and new decisions: %+v", d.FloatingIPResolutions)
	}
}

func TestFloatingIPEvidenceReferenceIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, previous, ref string
		same                bool
	}{
		{"number", "audit-operation-7341", "AUDIT-OPERATION-7341", true},
		{"UUID", "feed0000-0000-4000-8000-000000000001", "FEED0000-0000-4000-8000-000000000001", true},
		{"opaque record ID", "AbCZ42", "abcz42", false},
		{"escaped octet", "https://records.example/A%2fB?record=%4A", "https://records.example/A%2FB?record=%4a", true},
		{"escaped slash differs from separator", "https://records.example/A%2FB", "https://records.example/A/B", false},
		{"user", "https://Reviewer@records.example/1", "https://reviewer@records.example/1", false},
		{"IPv6 zone", "https://[fe80::1%25ethA]/1", "https://[fe80::1%25etha]/1", false},
		{"relative link", "reports/AbC", "reports/abc", false},
		{"relative path with numbered fragment", "Evidence#123", "evidence#123", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := FloatingIPReconciliation{Evidence: tc.ref}
			err := req.CheckEvidence(&FloatingIPReceipt{contradictedEvidence: []string{tc.previous}})
			if tc.same && clerrors.Reason(err) != clerrors.CodeUsage || !tc.same && err != nil {
				t.Fatalf("previous %q, supplied %q, same=%t: %v", tc.previous, tc.ref, tc.same, err)
			}
		})
	}
}
