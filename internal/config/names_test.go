// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureWarnings собирает предупреждения о старых (legacy) именах.
func captureWarnings(t *testing.T) *[]string {
	t.Helper()
	var got []string
	warnMu.Lock()
	old := warnFunc
	warnMu.Unlock()
	SetWarnFunc(func(msg string) { got = append(got, msg) })
	t.Cleanup(func() { SetWarnFunc(old) })
	return &got
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDataDir(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(root string)
		want   string // DataDirName или LegacyDataDirName
		warned bool
	}{
		{"нет ни одного — новый", func(string) {}, DataDirName, false},
		{"только новый", func(r string) { mkdir(t, filepath.Join(r, DataDirName)) }, DataDirName, false},
		{"только legacy — работаем в нём", func(r string) {
			writeFile(t, filepath.Join(r, LegacyDataDirName, "state.json"), "{}")
		}, LegacyDataDirName, true},
		{"оба, state только в legacy — legacy", func(r string) {
			mkdir(t, filepath.Join(r, DataDirName, "plans"))
			writeFile(t, filepath.Join(r, LegacyDataDirName, "state.json"), "{}")
		}, LegacyDataDirName, true},
		{"оба со state — новый, с предупреждением о legacy", func(r string) {
			writeFile(t, filepath.Join(r, DataDirName, "state.json"), "{}")
			writeFile(t, filepath.Join(r, LegacyDataDirName, "state.json"), "{}")
		}, DataDirName, true},
		{"оба, legacy пустой — новый молча", func(r string) {
			mkdir(t, filepath.Join(r, DataDirName))
			mkdir(t, filepath.Join(r, LegacyDataDirName))
		}, DataDirName, false},
		{"домашний каталог: stack-jobs в legacy", func(r string) {
			mkdir(t, filepath.Join(r, DataDirName))
			mkdir(t, filepath.Join(r, LegacyDataDirName, "stack-jobs"))
		}, LegacyDataDirName, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warnings := captureWarnings(t)
			root := t.TempDir()
			c.setup(root)
			if got := DataDir(root); got != filepath.Join(root, c.want) {
				t.Errorf("DataDir = %s, want %s", got, filepath.Join(root, c.want))
			}
			if (len(*warnings) > 0) != c.warned {
				t.Errorf("warnings = %q, want warned=%v", *warnings, c.warned)
			}
			// Повторный вызов (MCP — на каждый инструмент) не повторяет предупреждение.
			n := len(*warnings)
			DataDir(root)
			if len(*warnings) != n {
				t.Errorf("warning repeated: %q", *warnings)
			}
		})
	}
}

func TestProjectFile(t *testing.T) {
	warnings := captureWarnings(t)
	dir := t.TempDir()
	if got := ProjectFile(dir); got != filepath.Join(dir, ProjectFileName) || len(*warnings) != 0 {
		t.Fatalf("empty dir: %s, %q", got, *warnings)
	}

	legacy := filepath.Join(dir, LegacyProjectFileName)
	writeFile(t, legacy, "region: x\n")
	if got := ProjectFile(dir); got != legacy {
		t.Errorf("only legacy file: got %s", got)
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], ProjectFileName) {
		t.Errorf("warnings = %q", *warnings)
	}

	writeFile(t, filepath.Join(dir, ProjectFileName), "region: y\n")
	if got := ProjectFile(dir); got != filepath.Join(dir, ProjectFileName) {
		t.Errorf("both files: got %s", got)
	}
	if len(*warnings) != 2 || !strings.Contains((*warnings)[1], "ignored") {
		t.Errorf("warnings = %q", *warnings)
	}
}

// fakeUserConfigDir подменяет os.UserConfigDir и снимает CLOUDRU_CONFIG_DIR.
func fakeUserConfigDir(t *testing.T) string {
	t.Helper()
	isolate(t)
	t.Setenv(EnvConfigDir, "")
	base := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return base, nil }
	t.Cleanup(func() { userConfigDir = old })
	return base
}

func TestMigrateLegacyDir(t *testing.T) {
	base := fakeUserConfigDir(t)
	legacy := filepath.Join(base, LegacyAppName)
	writeFile(t, filepath.Join(legacy, settingsName), "profiles:\n  default:\n    project_id: p-1\n")
	writeFile(t, filepath.Join(legacy, credentialsName), "profiles:\n  default:\n    key_id: fake-key\n    secret: fake-secret\n")
	writeFile(t, filepath.Join(legacy, lockName), "1\n")
	writeFile(t, filepath.Join(legacy, "cache", "token-default.json"), "{}")

	msg, err := MigrateLegacyDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, AppName)
	if Dir() != dir || !strings.Contains(msg, dir) {
		t.Fatalf("Dir() = %s, msg = %q", Dir(), msg)
	}
	p, c, ok, err := LoadProfile(DefaultProfile)
	if err != nil || !ok || p.ProjectID != "p-1" || c.KeyID != "fake-key" || c.Secret != "fake-secret" {
		t.Fatalf("profile after migration: %+v %+v %v %v", p, c, ok, err)
	}
	// Только два файла профилей: ни кэша токена, ни лока, ни временного каталога.
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != settingsName+","+credentialsName {
		t.Errorf("files in %s: %v", dir, names)
	}
	baseEntries, _ := os.ReadDir(base)
	if len(baseEntries) != 2 {
		t.Errorf("leftovers in %s: %v", base, baseEntries)
	}
	// Права: каталог 0700, файлы 0600; на Windows — ACL только владельца.
	for _, path := range []string{dir, SettingsPath(), CredentialsPath()} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if PermissionsEnforced() && LooseMode(st.Mode()) {
			t.Errorf("%s: mode %04o", path, st.Mode().Perm())
		}
	}
	if w := PrivateDirWarning(dir); w != "" {
		t.Error(w)
	}
	if w := PrivateFileWarning(CredentialsPath()); w != "" {
		t.Error(w)
	}
	// Старый каталог не тронут.
	if _, err := os.Stat(filepath.Join(legacy, credentialsName)); err != nil {
		t.Errorf("legacy credentials: %v", err)
	}

	// Один раз: новый каталог есть — второй вызов ничего не делает, даже
	// если в старом что-то поменялось.
	writeFile(t, filepath.Join(legacy, settingsName), "profiles:\n  default:\n    project_id: p-2\n")
	if msg, err := MigrateLegacyDir(); err != nil || msg != "" {
		t.Fatalf("second run: %q, %v", msg, err)
	}
	if p, _, _, _ := LoadProfile(DefaultProfile); p.ProjectID != "p-1" {
		t.Errorf("second run overwrote the profile: %+v", p)
	}
}

func TestMigrateLegacyDirSkipped(t *testing.T) {
	t.Run("нет старого каталога", func(t *testing.T) {
		base := fakeUserConfigDir(t)
		if msg, err := MigrateLegacyDir(); err != nil || msg != "" {
			t.Fatalf("%q, %v", msg, err)
		}
		if _, err := os.Stat(filepath.Join(base, AppName)); !os.IsNotExist(err) {
			t.Errorf("created %s without a legacy dir: %v", AppName, err)
		}
	})
	t.Run("CLOUDRU_CONFIG_DIR задан", func(t *testing.T) {
		base := fakeUserConfigDir(t)
		writeFile(t, filepath.Join(base, LegacyAppName, credentialsName), "profiles: {}\n")
		t.Setenv(EnvConfigDir, filepath.Join(t.TempDir(), "custom"))
		if msg, err := MigrateLegacyDir(); err != nil || msg != "" {
			t.Fatalf("%q, %v", msg, err)
		}
		if _, err := os.Stat(filepath.Join(base, AppName)); !os.IsNotExist(err) {
			t.Errorf("migrated despite %s: %v", EnvConfigDir, err)
		}
	})
}
