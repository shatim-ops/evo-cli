// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestPrefixes_Russian(t *testing.T) {
	i18n.SetLang("ru")
	defer i18n.SetLang("en")

	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)
	p.Warning("порты не открыты")
	p.Errorf("ключ не найден")
	want := "Предупреждение: порты не открыты\nОшибка: ключ не найден\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
