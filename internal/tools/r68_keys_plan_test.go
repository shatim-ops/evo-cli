// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func newAuthorizedKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment
}

// R-68 («Риск», conclusion 72; plan 1.8): deploy_plan shows the keys of
// .evo/user_authorized_keys and the person approves them with the plan, but
// deploy_apply read the file anew — a key added between the approval and
// the apply got an SSH login on the VM that nobody saw. The plan keeps the
// hash of the keys it showed, as deploy keeps compose_hash (#1050); keys
// that differ at the apply refuse it before anything is written.
func TestDeployApply_UserKeysChangedSinceThePlan(t *testing.T) {
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
		// The world has no create of a VM: a call that reaches it went past
		// the check, which is what the unchanged keys must do.
		refused := func() (refused bool) {
			defer func() {
				if recover() != nil {
					refused = false
				}
			}()
			text, _ := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
			return answerReason(text) == clierrors.CodeConfirmationRequired && strings.Contains(text, "user_ssh_keys")
		}()
		if refused != changed {
			t.Errorf("keys changed %v: refused before any write %v", changed, refused)
		}
	}
}
