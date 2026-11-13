// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// evo vm get --diagnose: the first line is the result, and «incomplete»
// says not everything was checked — never a table that reads as fine with
// the gap below it («Риск», conclusion 209, У2). Without diagnose there is
// no result line.
func TestVMCard_TheResultIsTheFirstLine(t *testing.T) {
	for _, c := range []struct{ answer, first string }{
		{`{"id":"vm-1","name":"web","result":"incomplete","not_checked":[{"check":"interfaces","reason":"AUTH_FAILED"}]}`, "incomplete"},
		{`{"id":"vm-1","name":"web","result":"problem"}`, "problem"},
		{`{"id":"vm-1","name":"web","result":"ok"}`, "ok"},
	} {
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModeText, false)
		if err := renderVMCard(p, &buf, c.answer); err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(buf.String(), "\n")
		if !strings.Contains(first, c.first) {
			t.Errorf("first line %q, want the result %s", first, c.first)
		}
	}
	var buf bytes.Buffer
	_ = renderVMCard(output.NewWithWriter(&buf, output.ModeText, false), &buf, `{"id":"vm-1","name":"web"}`)
	if strings.Contains(buf.String(), "Result") || strings.Contains(buf.String(), "Итог") {
		t.Errorf("a plain vm get prints a result line: %s", buf.String())
	}
}
