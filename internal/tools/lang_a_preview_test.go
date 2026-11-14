// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The preview of destroy a person consents to speaks the language of the
// CLI: Russian by default, English over MCP (D6; Астра #1835).
func TestLangA_DestroyPreviewSpeaksTheCLILanguage(t *testing.T) {
	was := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(was) })
	latin := regexp.MustCompile(`\b[A-Za-z]{3,} [a-z]{2,} [a-z]{2,}\b`)
	rules := []sgrules.Planned{
		{Rule: state.SGRule{ID: "r1", Port: "80/tcp", SecurityGroupID: "sg"}, KeptFor: "d2"},
		{Rule: state.SGRule{ID: "r2", Port: "22/tcp", SecurityGroupID: "sg"}},
	}
	preview := func() map[string]any {
		return destroyTargetPreview(&provision.VM{ID: faultID(0x41), Name: "web"}, false, "p", false, true,
			&state.DeploymentRecord{VMIP: "203.0.113.10"}, "203.0.113.10", i18n.T("evo did not make it", "его создал не evo"), rules)
	}
	i18n.SetLang(i18n.RU)
	ru := preview()
	texts := append([]string{ru["floating_ip"].(string), ru["floating_ip_rule"].(string)}, ru["port_rules"].([]string)...)
	for _, s := range texts {
		if latin.MatchString(strings.NewReplacer("floating IP", "", "--release-ip", "").Replace(s)) {
			t.Errorf("Russian preview: %q", s)
		}
	}
	i18n.SetLang(i18n.EN)
	if en := preview(); !strings.Contains(en["floating_ip"].(string), "release_ip=true") {
		t.Errorf("English preview names the argument: %q", en["floating_ip"])
	}
}
