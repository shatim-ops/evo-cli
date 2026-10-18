// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// Файлы 0600 не спасают, если каталог с ними открыт на запись другим
// (CLOUDRU_CONFIG_DIR на общем каталоге, chmod 777): чужой пользователь
// подменяет credentials.yaml своим файлом или ссылкой — и deploy уходит в
// его проект вместе с секретами приложения, — а кэш токена подменяет своим
// токеном.
func TestReadCredentials_WarnsOnWritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not enforced on Windows")
	}
	dir := isolate(t)
	saveFakeProfile(t, "default", Profile{})
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, dir) && strings.Contains(w, "writable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет предупреждения о каталоге, открытом на запись: %q", cfg.Warnings)
	}
}

func TestReadCredentials_PrivateDirNoWarning(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	cfg, err := resolveConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Fatalf("лишние предупреждения: %q", cfg.Warnings)
	}
}
