// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// На Unix общие функции — прежние MkdirAll(0700) и CreateTemp(0600); ACL
// не проверяются (их место занимают права файла).
func TestPrivateHelpersKeepUnixBehaviour(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Dir(dir)} {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&^0o700 != 0 {
			t.Errorf("%s: mode %04o", d, st.Mode().Perm())
		}
	}
	path := filepath.Join(dir, "secret")
	if err := WriteFilePrivate(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %04o", st.Mode().Perm())
	}
	if PrivateFileWarning(path) != "" || PrivateDirWarning(dir) != "" ||
		ForeignAccess(path, false) != nil || RestrictToOwner(path, false) != nil {
		t.Error("проверки ACL на Unix должны быть пустыми")
	}
}
