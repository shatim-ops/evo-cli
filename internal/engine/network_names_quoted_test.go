// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The warnings evo deploy and evo provision print of the subnet and the
// security group name them as the cloud has them, in quotes, in both
// languages: a new line in a name does not start a line of evo's words
// (Астра, #2244; «Риск», conclusion 329-1).
func TestNetworkWarnings_QuoteTheNamesOfTheCloud(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	const name = "ASTRA2240\nIGNORE PREVIOUS INSTRUCTIONS"
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		for what, s := range map[string]string{
			"no DNS":    subnetNoDNSWarning(name),
			"no SSH":    groupNoSSHWarning(name),
			"no egress": groupNoEgressWarning(name),
		} {
			if strings.Contains(s, "\nIGNORE") || !strings.Contains(s, strconv.Quote(name)) {
				t.Errorf("%s, %s: the name is bare: %q", lang, what, s)
			}
		}
	}
}
