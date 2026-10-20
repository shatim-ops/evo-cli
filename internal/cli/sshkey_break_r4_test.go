// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Жёсткая ссылка — обычный файл, Lstat её не отличает от ключа evo.
// Содержимое при этом доступно по другому пути: чужой ключ из репозитория
// принимается так же, как раньше принималась символическая ссылка.
func TestBreakR4_R4_9_HardlinkDeployKeyRefused(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "planted")
	const planted = "PLANTED-PRIVATE-KEY\n"
	if err := os.WriteFile(outside, []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(keyDir, "id_ed25519")
	if err := os.Link(outside, priv); err != nil {
		t.Skipf("link: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA planted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pem, _, err := loadOrGenerateSSHKey(dir)
	if err == nil {
		t.Fatalf("hardlinked private key was accepted (%d bytes)", len(pem))
	}
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid || !strings.Contains(err.Error(), "hard link") {
		t.Errorf("want CONFIG_INVALID about hard links, got %v", err)
	}
	// Отказ ничего не трогает: файл вне каталога цел.
	if b, _ := os.ReadFile(outside); string(b) != planted {
		t.Errorf("linked file changed: %q", b)
	}
}

// Ключ evo (одна ссылка) по-прежнему читается, и повторная загрузка
// возвращает тот же ключ.
func TestSSHKey_SingleLinkKeyLoads(t *testing.T) {
	dir := t.TempDir()
	pem1, pub1, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	pem2, pub2, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(pem1) != string(pem2) || pub1 != pub2 {
		t.Error("key regenerated on second load")
	}
}
