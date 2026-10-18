// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Ключ проекта лежит в ./.evo/ — каталоге, который приходит вместе с
// чужим репозиторием. Ни символическая ссылка там, ни заранее созданный файл
// с широкими правами не должны ни переписать чужой файл, ни оставить
// приватный ключ читаемым для всех.

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink недоступен: %v", err)
	}
}

func TestSSHKey_ExistingLooseFileGetsPrivateMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права 0600 на Windows не выставляются")
	}
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Пустой файл 0644 (остался от прерванного запуска или из git): ключа
	// в нём нет, поэтому создаётся новый — и он не должен унаследовать 0644.
	priv := filepath.Join(keyDir, "id_ed25519")
	if err := os.WriteFile(priv, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(priv, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOrGenerateSSHKey(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(priv)
	if err != nil {
		t.Fatal(err)
	}
	if m := st.Mode().Perm(); m != 0o600 {
		t.Fatalf("приватный ключ с правами %04o, нужно 0600", m)
	}
	dst, err := os.Stat(keyDir)
	if err != nil {
		t.Fatal(err)
	}
	if m := dst.Mode().Perm(); m != 0o700 {
		t.Fatalf("каталог ключа с правами %04o, нужно 0700", m)
	}
}

func TestSSHKey_SymlinkedKeyFileIsNotFollowed(t *testing.T) {
	for _, name := range []string{"id_ed25519", "id_ed25519.pub"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "victim")
			orig := []byte("чужой файл, например ~/.ssh/authorized_keys\n")
			if err := os.WriteFile(outside, orig, 0o644); err != nil {
				t.Fatal(err)
			}
			keyDir := filepath.Join(dir, ".evo")
			if err := os.MkdirAll(keyDir, 0o700); err != nil {
				t.Fatal(err)
			}
			symlinkOrSkip(t, outside, filepath.Join(keyDir, name))

			_, _, err := loadOrGenerateSSHKey(dir)
			got, rerr := os.ReadFile(outside)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !bytes.Equal(got, orig) {
				t.Fatalf("файл по ссылке %s переписан:\n%s", name, got)
			}
			if err == nil {
				t.Fatalf("ссылка %s вместо ключа принята молча", name)
			}
		})
	}
}

func TestSSHKey_SymlinkedKeyDirIsRefused(t *testing.T) {
	dir := t.TempDir()
	// Чужой репозиторий: .evo -> ~/.ssh с настоящим ключом без .pub.
	sshDir := t.TempDir()
	realKey := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nреальный ключ пользователя\n")
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), realKey, 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, sshDir, filepath.Join(dir, ".evo"))

	if _, _, err := loadOrGenerateSSHKey(dir); err == nil {
		t.Fatal(".evo — ссылка на другой каталог, а ключ создан/прочитан через неё")
	}
	got, err := os.ReadFile(filepath.Join(sshDir, "id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, realKey) {
		t.Fatal("настоящий ключ пользователя переписан")
	}
	if _, err := os.Lstat(filepath.Join(sshDir, "id_ed25519.pub")); err == nil {
		t.Fatal("в чужой каталог записан id_ed25519.pub")
	}
}

func TestSSHKey_GeneratedThenReused(t *testing.T) {
	dir := t.TempDir()
	priv1, pub1, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv2, pub2, err := loadOrGenerateSSHKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priv1, priv2) || pub1 != pub2 {
		t.Fatal("второй запуск создал новый ключ вместо существующего")
	}
	// «git add .» в каталоге проекта не должен забирать приватный ключ.
	gi, err := os.ReadFile(filepath.Join(dir, ".evo", ".gitignore"))
	if err != nil || string(gi) != "*\n" {
		t.Fatalf(".evo/.gitignore: %q, %v; ждали «*»", gi, err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dir, ".evo", "id_ed25519.pub"))
		if err != nil {
			t.Fatal(err)
		}
		if m := st.Mode().Perm(); m != 0o644 {
			t.Fatalf("публичный ключ с правами %04o, ожидали 0644", m)
		}
	}
}
