// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// F17 (agent runs of Grok on 1.6.0): a hint for a project with no subnet
// in any zone sent the person to the console, though evo creates subnets
// itself since 1.6 (subnet_create). The hint names the tool, in one place
// (NoSubnetAnywhere); no text of evo sends to the console for a subnet.
func TestNoSubnetHint_NamesSubnetCreate(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		i18n.SetLang(lang)
		var ce *clerrors.CLIError
		if !errors.As(NoSubnetError("ru.AZ-1", "az-1"), &ce) || !strings.Contains(ce.Hint, "subnet_create") {
			t.Errorf("%s: hint of NoSubnetError %v does not name subnet_create", lang, ce)
		}
	}
	i18n.SetLang("en")
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range []string{"create one in the Cloud.ru console", "создайте её в консоли", "Create a subnet in the Cloud.ru console", "Создайте подсеть в консоли"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s sends to the console for a subnet: %q", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
