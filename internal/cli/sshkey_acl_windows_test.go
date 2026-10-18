// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/shatim-ops/evo-cli/internal/config"
)

func daclProtected(t *testing.T, path string) bool {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	return control&windows.SE_DACL_PROTECTED != 0
}

// Ключ проекта на Windows: каталог и приватный ключ с собственным DACL без
// наследования, без доступа посторонних — каким бы ни был ACL проекта.
func TestSSHKey_WindowsKeyHasPrivateDACL(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := loadOrGenerateSSHKey(dir); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, ".evo")
	priv := filepath.Join(keyDir, "id_ed25519")
	for _, p := range []string{keyDir, priv} {
		if !daclProtected(t, p) {
			t.Errorf("%s: DACL наследуется", p)
		}
	}
	if who := config.ForeignAccess(priv, false); len(who) > 0 {
		t.Errorf("ключ доступен: %v", who)
	}
	if w := sshKeyWarnings(dir); len(w) > 0 {
		t.Errorf("предупреждения о своём ключе: %q", w)
	}
}

// Каталог старой версии, открытый Everyone, закрывается при загрузке ключа
// (как chmod 0700 на Unix) — вместе с ключом, который наследовал его ACL.
func TestSSHKey_WindowsOpenKeyDirIsRestricted(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.Mkdir(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(keyDir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if len(sshKeyWarnings(dir)) == 0 {
		t.Fatal("нет предупреждения о каталоге, открытом Everyone")
	}
	if _, _, err := loadOrGenerateSSHKey(dir); err != nil {
		t.Fatal(err)
	}
	if !daclProtected(t, keyDir) {
		t.Error("каталог ключа не закрыт")
	}
	if w := sshKeyWarnings(dir); len(w) > 0 {
		t.Errorf("после загрузки ключа: %q", w)
	}
}
