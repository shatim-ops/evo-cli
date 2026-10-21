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

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The preview of deploy_apply is its saved plan (break 1.2, B12-8): the
// answer keeps status and message and adds plan_id, expires_at and preview —
// the VM, the project and the price as the plan stores them, with the note
// of the price's source — and nothing reaches the cloud before consent.
func TestDeployApplyPreview_FromThePlan(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	plan := faultPlan()
	plan.Ports = []int{80}
	plan.Cost = pricing.Estimate{Currency: "RUB", TotalMonthlyRub: 1308.16, TotalHourlyRub: 1.792, Source: pricing.SourceAPI}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID})
	var got struct {
		Status    string               `json:"status"`
		Message   string               `json:"message"`
		PlanID    string               `json:"plan_id"`
		ExpiresAt string               `json:"expires_at"`
		Preview   engine.CreatePreview `json:"preview"`
	}
	if !isErr || json.Unmarshal([]byte(text), &got) != nil || got.Status != "confirmation_required" {
		t.Fatalf("not the gate: %s", text)
	}
	p := got.Preview
	if got.PlanID != plan.PlanID || got.ExpiresAt == "" || !strings.Contains(got.Message, "web") ||
		p.Resource != engine.ResourceVM || p.Name != "web" || p.ProjectID != faultProject || p.ProjectFrom != "plan" ||
		p.VM == nil || p.VM.Flavor.Name != "low-1-1" || p.VM.Zone.Name != "ru.AZ-1" || p.VM.Image.ID != faultImage ||
		p.SubnetID != faultSubnet ||
		p.PublicIP == nil || !*p.PublicIP || p.Cost == nil || *p.Cost != plan.Cost ||
		!strings.Contains(p.CostNote, "price-calculation API") {
		t.Errorf("the preview is not the plan:\n%s", text)
	}
	if calls := cloud.callLog(); len(calls) != 0 {
		t.Errorf("the unconfirmed apply called the cloud: %v", calls)
	}
}

// A plan_id the gate cannot show is refused before it, as with
// confirmed=true: a malformed one, and one with no plan file (which the
// read makes no directory for).
func TestDeployApplyPreview_UnknownPlanRefused(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	for id, want := range map[string]string{
		"../x":              "a plan_id looks like plan-",
		"plan-0123456789ab": "not found",
	} {
		for _, confirmed := range []bool{false, true} {
			text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": id, "confirmed": confirmed})
			if !isErr || strings.Contains(text, "confirmation_required") || !strings.Contains(text, want) {
				t.Errorf("%s (confirmed=%v): want the refusal %q: %s", id, confirmed, want, text)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(config.DataDir(dir), "plans")); !os.IsNotExist(err) {
		t.Errorf("loading a missing plan made the plans directory: %v", err)
	}
	if calls := cloud.callLog(); len(calls) != 0 {
		t.Errorf("a refused apply called the cloud: %v", calls)
	}
}

// keyImageCloud is planCloud with an image whose one way in is the SSH
// key: its authentication group has only public_key.
type keyImageCloud struct{ planCloud }

func (keyImageCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "Ubuntu-24.04", MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
	}}, nil
}

// On a --read-only server deploy_plan makes no deploy key and saves no plan
// (B12-9), yet shows the plan a writable server makes: the deploy key all
// the same (an image that takes only a key is planned, not refused for a
// missing field), the price, no plan_id and a note why.
func TestDeployPlan_ReadOnlyKeyLater(t *testing.T) {
	plan := func(readOnly bool) (map[string]any, string) {
		t.Helper()
		dir := t.TempDir()
		cfg := config.Config{Region: planZoneAZ2, ProjectID: faultProject, KeyID: "k", Secret: "s"}
		r := New(func(map[string]any) (*engine.Engine, error) {
			var buf bytes.Buffer
			return &engine.Engine{Config: cfg, Auth: staticToken{}, Cloud: keyImageCloud{}, StateDir: dir,
				Printer: output.NewWithWriter(&buf, output.ModeText, false),
				SSHKeyLoader: func() ([]byte, string, error) {
					return []byte("key"), "ssh-ed25519 AAAA test", nil
				}}, nil
		})
		if readOnly {
			r.SetReadOnly()
		}
		text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
		var view map[string]any
		if isErr || json.Unmarshal([]byte(text), &view) != nil {
			t.Fatalf("read-only %v: no plan: %s", readOnly, text)
		}
		return view, dir
	}
	ro, dir := plan(true)
	rw, _ := plan(false)
	if _, ok := ro["plan_id"]; ok {
		t.Errorf("a read-only plan has a plan_id: %v", ro)
	}
	if _, ok := ro["expires_at"]; ok {
		t.Errorf("a read-only plan has expires_at: %v", ro)
	}
	if note, _ := ro["note"].(string); !strings.Contains(note, "--read-only") {
		t.Errorf("no note of --read-only: %v", ro)
	}
	if id, _ := rw["plan_id"].(string); id == "" {
		t.Fatalf("the writable plan has no plan_id: %v", rw)
	}
	for _, k := range []string{"ssh", "cost", "vm", "network"} {
		a, _ := json.Marshal(ro[k])
		b, _ := json.Marshal(rw[k])
		if string(a) != string(b) {
			t.Errorf("%s differs with --read-only:\n  ro: %s\n  rw: %s", k, a, b)
		}
	}
	if ssh, _ := ro["ssh"].(map[string]any); ssh["key_name"] != "ssh-key (deploy)" {
		t.Errorf("the read-only plan is not for the deploy key: %v", ro["ssh"])
	}
	if entries, _ := os.ReadDir(config.DataDir(dir)); len(entries) != 0 {
		t.Errorf("read-only deploy_plan wrote %v", entries)
	}
}
