// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Full audit 1.8, FB180-7 (1): deploy_apply checked the keys of
// .evo/user_authorized_keys against the plan (D90, R-68) only after
// consent: a call without confirmed showed the preview with its price, and
// the refusal came after «yes». The check is a local read, so it is a
// refusal before the question (contract axis 1, «a refusal without a
// write is a refusal before the question»). Unchanged keys still get the
// preview.
func TestDeployApply_KeysChangedRefusedBeforeTheQuestion(t *testing.T) {
	for _, changed := range []bool{false, true} {
		r, dir := planRegistry(t, config.Config{Region: planZoneAZ2, Flavor: "auto"})
		file := filepath.Join(config.DataDir(dir), userKeysFile)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(newAuthorizedKey(t, "seen@example.test")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
		var plan struct {
			PlanID string `json:"plan_id"`
		}
		if isErr || json.Unmarshal([]byte(text), &plan) != nil || plan.PlanID == "" {
			t.Fatalf("deploy_plan: %s", text)
		}
		if changed {
			f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteString(newAuthorizedKey(t, "unseen@example.test") + "\n")
			f.Close()
		}
		// No confirmed: the question, or the refusal before it.
		text, _ = r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID})
		var a struct{ Status string }
		_ = json.Unmarshal([]byte(text), &a)
		asked := a.Status == "confirmation_required"
		refused := !asked && strings.Contains(text, "user_ssh_keys")
		switch {
		case changed && asked:
			t.Errorf("keys changed since the plan: deploy_apply asks for consent first, the refusal comes after «yes»:\n%.400s", text)
		case changed && !refused:
			t.Errorf("keys changed since the plan: no refusal naming user_ssh_keys before the question:\n%.400s", text)
		case !changed && !asked:
			t.Errorf("keys unchanged: deploy_apply without confirmed does not ask:\n%.400s", text)
		}
	}
}
