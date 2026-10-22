// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"math"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// CUR122-6 (adversarial review of 1.2.2): a sum out of int64 or not finite
// printed the overflow of int64 as a negative sum.
func TestCUR122_RubHugeAndNonFinite(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, v := range []float64{1e20, math.Inf(1), math.NaN()} {
		got := rub(v)
		if strings.HasPrefix(got, "-") || strings.Contains(got, "9223372036854775808") {
			t.Errorf("rub(%v) = %q", v, got)
		}
	}
	got := renderRU(t, renderBillingBalance, `{"balance":"NaN","active":true}`)
	if strings.Contains(got, "9223372036854775808") || strings.HasPrefix(strings.TrimSpace(got), "-") {
		t.Errorf("balance NaN:\n%s", got)
	}
}
