// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/pricing"
)

// The source of a price in the Russian table is Russian, wherever it is in
// the answer (cost of deploy_plan, a line of stack_plan); the English CLI
// shows it as it is (D6; Астра #1835).
func TestRenderAnswer_ThePriceSourceInRussian(t *testing.T) {
	was := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(was) })
	text := `{"name":"web","cost":{"monthly_rub":100,"source":"` + pricing.SourceVCPURAM + `"},"lines":[{"source":"` + pricing.SourceAPI + `"}]}`
	for lang, want := range map[string]string{i18n.RU: "не сумма счёта", i18n.EN: "not a billed price"} {
		i18n.SetLang(lang)
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModeText, false)
		if err := renderAnswer(p, &buf, text, tableView{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), want) {
			t.Errorf("%s: %s", lang, buf.String())
		}
	}
}
