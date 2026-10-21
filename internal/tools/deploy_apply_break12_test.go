// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/pricing"
)

// TestBreak12_B12_8_DeployApplyBlindPreview: deploy_apply answers
// confirmation_required before it loads the plan, so the gate names no
// price, no VM and no project. The stored price is what the user is asked
// to approve; apply does not quote again.
func TestBreak12_B12_8_DeployApplyBlindPreview(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	plan := faultPlan()
	plan.Cost = pricing.Estimate{Currency: "RUB", TotalMonthlyRub: 1234.5, Source: "test"}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID})
	if !isErr || !strings.Contains(text, "confirmation_required") {
		t.Fatalf("the gate did not stop the apply: %s", text)
	}
	for _, want := range []string{"1234.5", "web", faultProject} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation_required omits %q:\n%s", want, text)
		}
	}
	if len(cloud.liveVMs()) != 0 {
		t.Errorf("the unconfirmed apply created %d VMs", len(cloud.liveVMs()))
	}
}

// TestBreak12_B12_9_ReadOnlyDeployPlanWrites: deploy_plan takes no
// confirmed, so NeedsConsent is false and --read-only (H11) lets the call
// through. The handler still creates the agent's SSH key when none is set
// and writes the plan under .evo/plans. deploy_apply is refused, and the
// plan file is left for any other process that shares the state directory.
func TestBreak12_B12_9_ReadOnlyDeployPlanWrites(t *testing.T) {
	loaded := false
	dir := t.TempDir()
	cfg := config.Config{Region: planZoneAZ2, Flavor: "auto", ProjectID: "00000000-0000-4000-8000-000000000001", KeyID: "k", Secret: "s"}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   cfg,
			Auth:     staticToken{},
			Cloud:    planCloud{},
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			SSHKeyLoader: func() ([]byte, string, error) {
				loaded = true
				return []byte("key"), "ssh-ed25519 AAAA test", nil
			},
		}, nil
	})
	r.SetReadOnly()
	args := map[string]any{"name": "web"}
	tl, ok := r.Lookup("deploy_plan")
	if !ok {
		t.Fatal("deploy_plan")
	}
	if NeedsConsent(tl, args) {
		t.Fatal("NeedsConsent is true: H11 would refuse deploy_plan before a write")
	}
	text, isErr := r.handlers["deploy_plan"](context.Background(), args)
	plans, _ := filepath.Glob(filepath.Join(config.DataDir(dir), "plans", "plan-*.json"))
	if len(plans) != 0 || loaded {
		t.Errorf("read-only deploy_plan wrote %v, SSHKeyLoader called %v (tool error %v):\n%s", plans, loaded, isErr, text)
	}
}
