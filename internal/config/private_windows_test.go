// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// checkPrivateDACL проверяет ACL созданного объекта: DACL защищён от
// наследования, и в нём только текущий пользователь, SYSTEM и
// Administrators — ни Everyone, ни Users, ни «Прошедшие проверку».
func checkPrivateDACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo(%s): %v", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s: DACL наследуется (SE_DACL_PROTECTED не выставлен): %s", path, sd)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("%s: нет DACL (%v)", path, err)
	}
	user, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sawUser := false
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Errorf("%s: унаследованная запись: %s", path, sd)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.String() == user:
			sawUser = true
		case sid.IsWellKnown(windows.WinLocalSystemSid), sid.IsWellKnown(windows.WinBuiltinAdministratorsSid):
		default:
			// Everyone, Users, Authenticated Users и любые другие.
			t.Errorf("%s: доступ у постороннего %s: %s", path, sid, sd)
		}
	}
	if !sawUser {
		t.Errorf("%s: нет записи текущего пользователя: %s", path, sd)
	}
}

func TestWindowsPrivateFileAndDirHaveOwnDACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg", "nested")
	path := filepath.Join(dir, "credentials.yaml")
	if err := WriteFilePrivate(path, []byte("secret: x\n")); err != nil {
		t.Fatal(err)
	}
	checkPrivateDACL(t, path)
	checkPrivateDACL(t, dir)
	checkPrivateDACL(t, filepath.Dir(dir))
	if w := PrivateFileWarning(path); w != "" {
		t.Errorf("предупреждение о своём файле: %s", w)
	}
	if w := PrivateDirWarning(dir); w != "" {
		t.Errorf("предупреждение о своём каталоге: %s", w)
	}

	// Перезапись (configure второй раз) оставляет тот же ACL.
	if err := WriteFilePrivate(path, []byte("secret: y\n")); err != nil {
		t.Fatal(err)
	}
	checkPrivateDACL(t, path)
}

// Файл, открытый Everyone (так выглядит старый файл в C:\work), даёт
// предупреждение; RestrictToOwner его закрывает.
func TestWindowsForeignAccessWarnsAndRestrictFixes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;WD)(A;;FA;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if len(ForeignAccess(path, false)) < 2 {
		t.Errorf("Everyone и Users не найдены: %v", ForeignAccess(path, false))
	}
	if PrivateFileWarning(path) == "" {
		t.Error("нет предупреждения о файле, открытом Everyone")
	}
	if err := RestrictToOwner(path, false); err != nil {
		t.Fatal(err)
	}
	checkPrivateDACL(t, path)
	if w := PrivateFileWarning(path); w != "" {
		t.Errorf("после RestrictToOwner: %s", w)
	}
}

// Каталог, созданный MkdirPrivate, передаёт свои записи новым файлам:
// state.json и замок пишутся обычным os.CreateTemp/OpenFile.
func TestWindowsMkdirPrivateChildrenInherit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".evo")
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(dir); err != nil {
		t.Fatalf("повторный вызов: %v", err)
	}
	child := filepath.Join(dir, "state.json")
	if err := os.WriteFile(child, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if who := ForeignAccess(child, false); len(who) > 0 {
		t.Errorf("файл в закрытом каталоге доступен: %v", who)
	}
}

// credentials.yaml старой версии унаследовал ACL каталога, открытого
// «Прошедшим проверку»: команды предупреждают, как на Unix о chmod 644.
func TestWindowsReadCredentialsWarnsOnForeignACL(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	if w := PrivateFileWarning(CredentialsPath()); w != "" {
		t.Fatalf("свежий файл с предупреждением: %s", w)
	}
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;AU)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(CredentialsPath(), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, CredentialsPath()) && strings.Contains(w, "icacls") {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет предупреждения об ACL credentials.yaml: %q", cfg.Warnings)
	}
}
