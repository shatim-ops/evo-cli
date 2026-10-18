// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// .evo/ создаётся первым из state.Write/Lock (deploy --dry-run,
// destroy), а приватный SSH-ключ проекта ложится туда же. Каталог 0755
// показывал другим пользователям имена файлов и state.json с IP и ID ВМ.
func TestStateDirIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права Unix")
	}
	for name, mk := range map[string]func(path string) error{
		"Write": func(path string) error { return Write(path, &File{Version: CurrentVersion}) },
		"Lock": func(path string) error {
			unlock, err := Lock(path)
			if err == nil {
				unlock()
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := DefaultPath(t.TempDir())
			if err := mk(path); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if m := st.Mode().Perm(); m != 0o700 {
				t.Fatalf(".evo/ с правами %04o, нужно 0700", m)
			}
		})
	}
}
