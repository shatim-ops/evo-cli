// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Release plan 1.7, P3: a delete preview says whether this folder's evo
// made what goes — provenance {created_by_evo: yes|no|unknown, note} — by
// the folder's state and the time the cloud made the resource. It is
// information, not a boundary (owner's decision 03.11): nothing else of
// the delete changes.

// provenanceOf reads the provenance of a preview answer.
func provenanceOf(t *testing.T, tool, text string) engine.Provenance {
	t.Helper()
	var m struct {
		Status     string             `json:"status"`
		Provenance *engine.Provenance `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.Status != "confirmation_required" {
		t.Fatalf("%s: want a preview: %s", tool, text)
	}
	if m.Provenance == nil || m.Provenance.Note == "" {
		t.Fatalf("%s: the preview has no provenance with a note: %s", tool, text)
	}
	return *m.Provenance
}

// provenanceCloud is sgCloud whose VM is read by its id, with no floating
// IPs in the project.
type provenanceCloud struct{ sgCloud }

func (c *provenanceCloud) GetVM(_ context.Context, id string) (*provision.VM, error) {
	if c.vm != nil && c.vm.ID == id {
		return c.vm, nil
	}
	return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
}
func (*provenanceCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}

func TestProvenance_DestroyPreviews(t *testing.T) {
	recorded := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	vm := &provision.VM{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "web", Status: "running",
		CreatedAt: recorded.Add(-time.Minute)}
	own := state.DeploymentRecord{ID: "dep-1", VMID: vm.ID, VMName: vm.Name, ProjectID: "00000000-0000-0000-0000-000000000001",
		Status: "running", CreatedAt: recorded}

	t.Run("targeted, recorded own VM: yes", func(t *testing.T) {
		r, dir := provenanceRegistry(t, &provenanceCloud{sgCloud{vm: vm}})
		writeRecords(t, dir, own)
		text, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vm.ID})
		if p := provenanceOf(t, "destroy vm_id", text); p.CreatedByEvo != engine.ProvenanceYes || !strings.Contains(p.Note, "dep-1") {
			t.Errorf("a recorded own VM: %+v", p)
		}
	})
	t.Run("no target, recorded own VM: yes", func(t *testing.T) {
		r, dir := provenanceRegistry(t, &provenanceCloud{sgCloud{vm: vm}})
		writeRecords(t, dir, own)
		text, _ := r.handlers["destroy"](context.Background(), map[string]any{})
		if p := provenanceOf(t, "destroy", text); p.CreatedByEvo != engine.ProvenanceYes {
			t.Errorf("the recorded deployment: %+v", p)
		}
	})
	t.Run("targeted, a VM this folder does not record: unknown", func(t *testing.T) {
		r, _ := provenanceRegistry(t, &provenanceCloud{sgCloud{vm: vm}})
		text, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vm.ID})
		if p := provenanceOf(t, "destroy vm_id", text); p.CreatedByEvo != engine.ProvenanceUnknown || !strings.Contains(p.Note, "another folder") {
			t.Errorf("an unrecorded VM: %+v", p)
		}
	})
	t.Run("targeted, the state cannot be read: unknown with the class, the preview stays", func(t *testing.T) {
		r, dir := provenanceRegistry(t, &provenanceCloud{sgCloud{vm: vm}})
		if err := os.MkdirAll(filepath.Dir(state.DefaultPath(dir)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(state.DefaultPath(dir), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		text, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vm.ID})
		if p := provenanceOf(t, "destroy vm_id", text); p.CreatedByEvo != engine.ProvenanceUnknown || !strings.Contains(p.Note, clerrors.CodeStateCorrupted) {
			t.Errorf("an unread state: %+v", p)
		}
	})
}

// failingVMCloud is provenanceCloud whose GET of a VM fails with err.
type failingVMCloud struct {
	provenanceCloud
	err error
}

func (c *failingVMCloud) GetVM(context.Context, string) (*provision.VM, error) { return nil, c.err }

// The time of the recorded VM comes from the read D73 already makes before
// destroy's question (no read of its own). A read it lets pass — a key the
// cloud refuses — leaves the preview as it was, with unknown and the class
// in the note; a 503 stays D73's refusal before the question, unchanged.
func TestProvenance_DestroyWhenTheVMsTimeIsUnread(t *testing.T) {
	recorded := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	vm := &provision.VM{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "web", Status: "running"}
	rec := state.DeploymentRecord{ID: "dep-1", VMID: vm.ID, VMName: vm.Name, ProjectID: "00000000-0000-0000-0000-000000000001",
		Status: "running", CreatedAt: recorded}

	r, dir := provenanceRegistry(t, &failingVMCloud{provenanceCloud{sgCloud{vm: vm}}, clerrors.New(clerrors.CodeAuthFailed, "key refused", "")})
	writeRecords(t, dir, rec)
	text, _ := r.handlers["destroy"](context.Background(), map[string]any{})
	if p := provenanceOf(t, "destroy", text); p.CreatedByEvo != engine.ProvenanceUnknown || !strings.Contains(p.Note, clerrors.CodeAuthFailed) {
		t.Errorf("the VM's time refused for the key: %+v", p)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if res, _ := m["resources"].([]any); len(res) == 0 {
		t.Errorf("the preview lost its resources: %s", text)
	}

	r, dir = provenanceRegistry(t, &failingVMCloud{provenanceCloud{sgCloud{vm: vm}}, clerrors.New(clerrors.CodeAPIError, "API 503", "")})
	writeRecords(t, dir, rec)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{})
	if !isErr || strings.Contains(text, "confirmation_required") || strings.Contains(text, "provenance") {
		t.Errorf("a 503 on D73's read: want its refusal before the question, as before: %s", text)
	}
}

// provenanceRegistry is sgRegistry on any cloud.
func provenanceRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "00000000-0000-0000-0000-000000000001", KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	}), dir
}

// fipCloud has one available floating IP of any id asked for.
type fipCloud struct{ provision.CloudAPI }

func (*fipCloud) SetToken(string) {}

// The list of the project, which the preview reads for the project of the
// floating IP (namedProject): empty, the tests here read the provenance.
func (*fipCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
func (*fipCloud) GetFloatingIP(_ context.Context, id string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: id, IPAddress: "203.0.113.5", State: "available"}, nil
}

// release_floating_ip of the floating IP the folder's record names: yes;
// of another: unknown (the cloud gives no time of a floating IP, so a
// recorded one is taken as own, as AdoptedVM takes a VM without a time).
func TestProvenance_ReleaseFloatingIP(t *testing.T) {
	fip := "0000f1f1-0000-4000-8000-000000000001"
	r, dir := provenanceRegistry(t, &fipCloud{})
	writeRecords(t, dir, state.DeploymentRecord{ID: "dep-1", VMID: "0000a1a1-0000-4000-8000-000000000001", FloatingIPID: fip,
		Status: "running", CreatedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)})
	_, text, _ := callNamed(t, r, "release_floating_ip", map[string]any{"id": fip})
	if p := provenanceOf(t, "release_floating_ip", text); p.CreatedByEvo != engine.ProvenanceYes {
		t.Errorf("the recorded floating IP: %+v", p)
	}
	_, text, _ = callNamed(t, r, "release_floating_ip", map[string]any{"id": "00000000-0000-4000-8000-0000000000f2"})
	if p := provenanceOf(t, "release_floating_ip", text); p.CreatedByEvo != engine.ProvenanceUnknown {
		t.Errorf("a floating IP no record names: %+v", p)
	}
}

// A delete of deletePlan — a kind the state does not record — says
// unknown, and why.
func TestProvenance_DeletePlanIsUnknown(t *testing.T) {
	r := newTestServer()
	id := "00000000-0000-4000-8000-0000000000e1"
	p := r.deletePlan("pg_delete", "pg_delete deletes %s.", "pg_delete удаляет %s.", id,
		func() (map[string]any, error) {
			return map[string]any{"id": id, "name": "pg-1", "status": "RUNNING"}, nil
		},
		func(context.Context) (string, bool) { return "{}", false })
	if p.Execute == nil {
		t.Fatalf("a running cluster: want a question: %v", p.Preview)
	}
	b, _ := json.Marshal(p.Preview)
	var m struct {
		Provenance *engine.Provenance `json:"provenance"`
	}
	_ = json.Unmarshal(b, &m)
	if m.Provenance == nil || m.Provenance.CreatedByEvo != engine.ProvenanceUnknown || !strings.Contains(m.Provenance.Note, "does not record this kind") {
		t.Errorf("pg_delete: %s", b)
	}
}

// The rule itself, at the one point: no — recorded, but older than the
// record (an adoption, D73; destroy refuses it before any preview, so the
// function answers it); yes when a time is missing, as AdoptedVM.
func TestProvenanceOf_TheRule(t *testing.T) {
	recorded := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	id := "0000a1a1-0000-4000-8000-000000000001"
	sf := &state.File{Deployments: []state.DeploymentRecord{{ID: "dep-1", VMID: id, FloatingIPID: "fip-1", CreatedAt: recorded}}}
	for _, c := range []struct {
		name, kind, id string
		made           time.Time
		madeErr        error
		sf             *state.File
		readErr        error
		want           string
	}{
		{"made with the record", engine.ProvenanceVM, id, recorded.Add(-time.Minute), nil, sf, nil, engine.ProvenanceYes},
		{"made a day before the record", engine.ProvenanceVM, id, recorded.Add(-24 * time.Hour), nil, sf, nil, engine.ProvenanceNo},
		{"no time from the cloud", engine.ProvenanceVM, id, time.Time{}, nil, sf, nil, engine.ProvenanceYes},
		{"no time on the record", engine.ProvenanceVM, id, recorded.Add(-24 * time.Hour), nil,
			&state.File{Deployments: []state.DeploymentRecord{{ID: "dep-1", VMID: id}}}, nil, engine.ProvenanceYes},
		{"the floating IP of the record", engine.ProvenanceFloatingIP, "fip-1", time.Time{}, nil, sf, nil, engine.ProvenanceYes},
		{"the VM's id is not a floating IP's", engine.ProvenanceFloatingIP, id, time.Time{}, nil, sf, nil, engine.ProvenanceUnknown},
		{"not recorded", engine.ProvenanceVM, "0000a1a1-0000-4000-8000-000000000002", time.Time{}, nil, sf, nil, engine.ProvenanceUnknown},
		{"no state", engine.ProvenanceVM, id, time.Time{}, nil, nil, nil, engine.ProvenanceUnknown},
		{"unread state", engine.ProvenanceVM, id, time.Time{}, nil, nil, clerrors.New(clerrors.CodeStateCorrupted, "bad", ""), engine.ProvenanceUnknown},
		{"a kind the state does not record", "PostgreSQL cluster", id, time.Time{}, nil, sf, nil, engine.ProvenanceUnknown},
		{"the cloud's time could not be read", engine.ProvenanceVM, id, time.Time{}, clerrors.New(clerrors.CodeAuthFailed, "no", ""), sf, nil, engine.ProvenanceUnknown},
		{"the cloud's time could not be read, not recorded", engine.ProvenanceVM, "0000a1a1-0000-4000-8000-000000000002", time.Time{}, clerrors.New(clerrors.CodeAuthFailed, "no", ""), sf, nil, engine.ProvenanceUnknown},
	} {
		if got := engine.ProvenanceOf(c.kind, c.id, c.made, c.madeErr, c.sf, c.readErr); got.CreatedByEvo != c.want || got.Note == "" {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
}
