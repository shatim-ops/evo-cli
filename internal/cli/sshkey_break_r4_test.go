// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Жёсткая ссылка — обычный файл, Lstat её не отличает от ключа evo.
// Содержимое при этом доступно по другому пути: чужой ключ из репозитория
// принимается так же, как раньше принималась символическая ссылка.
func TestBreakR4_R4_9_HardlinkDeployKeyRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("жёсткие ссылки здесь не проверяются")
	}
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
}
