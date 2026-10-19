// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package cli

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// SEC-2: закрытый ключ деплоя, пришедший с чужим репозиторием (git
// сохраняет его с правами 0644), не используется: его открытая половина
// попала бы в authorized_keys новой ВМ. Как OpenSSH, отказ — при доступе
// группы или других; на Windows прав-битов нет (там — ACL и предупреждения).

// plantKey кладёт в dir/.evo готовую пару ключей с правами mode у закрытого.
func plantKey(t *testing.T, dir string, mode os.FileMode) (priv []byte, pub string) {
	t.Helper()
	priv, pub, err := auth.GenerateSSHKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	privPath := filepath.Join(keyDir, "id_ed25519")
	if err := os.WriteFile(privPath, priv, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privPath, mode); err != nil { // umask
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "id_ed25519.pub"), []byte(pub), 0o644); err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

func wantConfigInvalid(t *testing.T, err error) {
	t.Helper()
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid {
		t.Fatalf("ждали CONFIG_INVALID, получили %v", err)
	}
	if ce.Hint == "" {
		t.Error("нет подсказки (удалить .evo/ или chmod 600)")
	}
}

func TestSSHKey_LoosePrivateKeyIsRefused(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			priv, _ := plantKey(t, dir, mode)
			_, _, err := loadOrGenerateSSHKey(dir)
			wantConfigInvalid(t, err)
			// Ключ не переписан: решать, удалить его или сузить права, — человеку.
			got, rerr := os.ReadFile(filepath.Join(dir, ".evo", "id_ed25519"))
			if rerr != nil || !bytes.Equal(got, priv) {
				t.Fatalf("закрытый ключ изменён: %v", rerr)
			}
		})
	}
}

func TestSSHKey_PrivateKey0600IsLoaded(t *testing.T) {
	dir := t.TempDir()
	priv, pub := plantKey(t, dir, 0o600)
	gotPriv, gotPub, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotPriv, priv) || gotPub != pub {
		t.Fatal("загружен не тот ключ, что лежит в .evo/")
	}
}

// MCP (deploy, deploy_plan, deploy_apply) берёт ключ через тот же загрузчик
// движка, что и CLI: проверка общая.
func TestSSHKey_MCPEngineRefusesLooseKey(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-a")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	plantKey(t, cwd, 0o644)
	eng, err := mcpEngineFactory(nil)(nil)
	if err != nil {
		t.Fatal(err)
	}
	wantConfigInvalid(t, eng.EnsureSSHKey())
	if len(eng.SSHKeyPEM) != 0 || eng.SSHPubKey != "" {
		t.Fatal("ключ из репозитория попал в движок")
	}
}
