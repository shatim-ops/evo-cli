// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The confirmation_required answer of provision carries a structured
// preview with the resolved configuration (ревью r5 U2), built from one
// engine. That engine has no credentials and no cloud (Cloud is nil here, a
// call would panic): the reads of the preview (spec REQ-7) are skipped, and
// what needs them stays unknown. The existing fields stay.
func TestProvisionPreview_ResolvedFromConfig(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000001"
	engines := 0
	r := New(func(o map[string]any) (*engine.Engine, error) {
		engines++
		cfg := config.Config{ProjectID: id, ProjectOrigin: config.OriginProfile, Region: id, ImageID: id, Flavor: "lowcost10-2-4"}
		if v, ok := o["DiskSize"].(float64); ok {
			cfg.DiskSize = int(v)
		}
		return &engine.Engine{Config: cfg}, nil
	})
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "disk_size": float64(30), "no_public_ip": true})
	if !isErr || engines != 1 {
		t.Fatalf("isErr %v, engines %d, want true and 1", isErr, engines)
	}
	var resp struct {
		Status     string                `json:"status"`
		Message    string                `json:"message"`
		WillCreate map[string]any        `json:"will_create"`
		Preview    *engine.CreatePreview `json:"preview"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "confirmation_required" || resp.Message == "" || resp.WillCreate["project_id"] != id ||
		resp.WillCreate["public_ip"] != false {
		t.Fatalf("existing fields changed: %s", text)
	}
	p := resp.Preview
	if p == nil || p.Resource != engine.ResourceVM || p.ProjectID != id || p.ProjectFrom != config.OriginProfile ||
		p.VM == nil || p.VM.Zone.ID != id || p.VM.Image.ID != id || p.VM.Flavor.Name != "lowcost10-2-4" ||
		p.VM.Disk.SizeGB != 30 || p.PublicIP == nil || *p.PublicIP || len(p.Missing) != 0 {
		t.Fatalf("preview: %s", text)
	}
	// The flavor size needs the cloud: no price, and a note that says why.
	if p.Cost != nil || p.CostNote == "" {
		t.Fatalf("cost %v, note %q", p.Cost, p.CostNote)
	}
}

// Without a configuration there is no preview: a call without a project
// cannot create a VM, so it is refused before consent is asked for, as the
// call with confirmed=true is (ProjectRequired, from engineFor). The zone
// and the image: TestProvision_NoZoneOrImageRefusedBeforePreview.
func TestProvisionPreview_NoProjectRefused(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) { return &engine.Engine{}, nil })
	for _, confirmed := range []bool{false, true} {
		text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": confirmed})
		var resp map[string]any
		if err := json.Unmarshal([]byte(text), &resp); err != nil {
			t.Fatal(err)
		}
		if want := clierrors.ProjectRequired(); !isErr || resp["reason"] != clierrors.CodeConfigInvalid || resp["error"] != want.Message {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
	}
}

// provision picks its flavor with engine.PickVMFlavor: the smallest
// lowcost one, free tier skipped; a name or UUID as given.
func TestPickVMFlavor(t *testing.T) {
	flavors := []*provision.Flavor{
		{ID: "f-free", Name: "lowcost-free", VCPUs: 1, RAMGB: 1, FreeTier: true},
		{ID: "f-big", Name: "lowcost10-4-8", VCPUs: 4, RAMGB: 8},
		{ID: "f-small", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4},
		{ID: "f-gen", Name: "gen-2-2", VCPUs: 2, RAMGB: 2},
	}
	for want, in := range map[string]string{"f-small": "auto", "f-big": "lowcost10-4-8", "f-gen": "f-gen"} {
		if got, err := engine.PickVMFlavor(flavors, in); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	if got, _ := engine.PickVMFlavor(flavors, ""); got != "f-small" {
		t.Errorf("empty: %q", got)
	}
	if _, err := engine.PickVMFlavor(flavors, "nope"); err == nil {
		t.Error("an unknown flavor name was accepted")
	}
}
