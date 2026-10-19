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
)

// Аудит безопасности 19.10, продолжение SEC-2: .evo/user_authorized_keys
// мог прийти с чужим репозиторием, и deploy_apply молча пускал эти ключи
// на ВМ. deploy_plan показывает их (тип, отпечаток, комментарий), чтобы
// пользователь одобрил их вместе с планом.
func TestDeployPlan_ShowsUserKeys(t *testing.T) {
	r, dir := planRegistry(t, config.Config{Region: planZoneAZ2, Flavor: "auto"})
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " someone@example.test"
	keyDir := config.DataDir(dir)
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, userKeysFile), []byte("# ключи\n"+line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
	if isErr {
		t.Fatalf("deploy_plan: %s", text)
	}
	var view struct {
		Keys []map[string]string `json:"user_ssh_keys"`
		File string              `json:"user_ssh_keys_file"`
		Next string              `json:"next"`
	}
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Keys) != 1 || view.Keys[0]["fingerprint"] != ssh.FingerprintSHA256(sp) ||
		view.Keys[0]["comment"] != "someone@example.test" || view.Keys[0]["type"] != "ssh-ed25519" {
		t.Errorf("user_ssh_keys = %v", view.Keys)
	}
	if view.File != filepath.Join(filepath.Base(keyDir), userKeysFile) || !strings.Contains(view.Next, "user_ssh_keys") {
		t.Errorf("file %q, next %q", view.File, view.Next)
	}
}

// Без файла — ни ключей, ни упоминания в next.
func TestDeployPlan_NoUserKeys(t *testing.T) {
	r, _ := planRegistry(t, config.Config{Region: planZoneAZ2, Flavor: "auto"})
	text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
	if isErr {
		t.Fatalf("deploy_plan: %s", text)
	}
	if strings.Contains(text, "user_ssh_keys") {
		t.Errorf("план без файла ключей: %s", text)
	}
}
