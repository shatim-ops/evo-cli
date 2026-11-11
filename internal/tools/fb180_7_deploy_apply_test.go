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

	"golang.org/x/crypto/ssh"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// planWithUserKey makes a plan of deploy_plan with one key in
// .evo/user_authorized_keys; it returns the plan_id and the keys file.
func planWithUserKey(t *testing.T, r *Registry, dir string) (string, string, string) {
	t.Helper()
	file := filepath.Join(config.DataDir(dir), userKeysFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	key := newAuthorizedKey(t, "seen@example.test")
	if err := os.WriteFile(file, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
	var plan struct {
		PlanID string `json:"plan_id"`
	}
	if isErr || json.Unmarshal([]byte(text), &plan) != nil || plan.PlanID == "" {
		t.Fatalf("deploy_plan: %s", text)
	}
	return plan.PlanID, file, key
}

// Audit Fable 1.8, FB180-7 (1): the check of the keys against the plan
// (D90, R-68) is a local read, so a call bound to fail by it is refused
// before the question of consent (axis 1), not after the person said yes
// to a preview with a price.
func TestDeployApply_KeysChangedRefusedBeforeTheQuestion(t *testing.T) {
	r, dir := planRegistry(t, config.Config{Region: planZoneAZ2, Flavor: "auto"})
	planID, file, _ := planWithUserKey(t, r, dir)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(newAuthorizedKey(t, "unseen@example.test") + "\n")
	f.Close()

	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": planID})
	var a struct {
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Preview any    `json:"preview"`
	}
	_ = json.Unmarshal([]byte(text), &a)
	if !isErr || a.Status == "confirmation_required" || a.Preview != nil ||
		a.Reason != clierrors.CodeConfirmationRequired || !strings.Contains(text, "user_ssh_keys") {
		t.Errorf("deploy_apply without confirmed, keys changed since the plan: want the refusal of D90 before the question, got %s", text)
	}
}

// Audit Fable 1.8, FB180-7 (2): the preview the person consents to names
// the SSH keys that get a login on the VM — the key it is created with and
// the user's keys deploy_apply appends (axis 4); they were only in the
// answer of deploy_plan.
func TestDeployApply_PreviewNamesTheSSHKeys(t *testing.T) {
	r, dir := planRegistry(t, config.Config{Region: planZoneAZ2, Flavor: "auto"})
	planID, _, key := planWithUserKey(t, r, dir)

	text, _ := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": planID})
	var a struct {
		Status  string          `json:"status"`
		Preview json.RawMessage `json:"preview"`
	}
	if json.Unmarshal([]byte(text), &a) != nil || a.Status != "confirmation_required" {
		t.Fatalf("deploy_apply without confirmed: %s", text)
	}
	preview := string(a.Preview)
	user, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, ssh.FingerprintSHA256(user)) || !strings.Contains(preview, "seen@example.test") {
		t.Errorf("the preview does not name the user's key %s: %s", ssh.FingerprintSHA256(user), preview)
	}
	eng, err := r.localEngine(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.EnsureSSHKey(); err != nil {
		t.Fatal(err)
	}
	// The world's deploy key is no real key: the preview names such a key
	// by its text; a real one by its fingerprint.
	want := eng.SSHPubKey
	if deploy, _, _, _, err := ssh.ParseAuthorizedKey([]byte(eng.SSHPubKey)); err == nil {
		want = ssh.FingerprintSHA256(deploy)
	}
	if !strings.Contains(preview, want) {
		t.Errorf("the preview does not name the key the VM is created with %s: %s", want, preview)
	}
}
