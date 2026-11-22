// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"regexp"
	"strconv"
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

func TestDestroyTargetPreview_QuotesIDAndKeepsRawTarget(t *testing.T) {
	id := "vm-\"one\nnext"
	for _, name := range []string{"", "web"} {
		for _, gone := range []bool{true, false} {
			m := destroyTargetPreview(&provision.VM{ID: id, Name: name}, gone, "p", false, false, &state.DeploymentRecord{}, "", "", nil)
			msg := m["message"].(string)
			if strings.Contains(msg, id) || !strings.Contains(msg, strconv.Quote(id)) || m["target"].(map[string]any)["id"] != id {
				t.Errorf("name=%q gone=%v, preview %q, target %v", name, gone, msg, m["target"])
			}
		}
	}
}

func TestDestroyTargetPreview_QuotesRuleAndGroupSeparately(t *testing.T) {
	groupID, ruleID := "sg-\"new\nline", "rule-\"new\nline"
	rule := state.SGRule{ID: ruleID, SecurityGroupID: groupID}
	for _, kept := range []string{"", "another-deployment"} {
		m := destroyTargetPreview(&provision.VM{ID: "target"}, false, "p", false, false, &state.DeploymentRecord{}, "", "", []sgrules.Planned{{Rule: rule, KeptFor: kept}})
		lines := m["port_rules"].([]string)
		if len(lines) != 1 || !strings.Contains(lines[0], strconv.Quote(groupID)) || strings.Contains(lines[0], "\n") || (kept == "") != strings.Contains(lines[0], strconv.Quote(ruleID)) {
			t.Fatalf("kept=%q lines=%q", kept, lines)
		}
	}
}

func TestPersistedIDTargetMatchesOriginalReceiptBytes(t *testing.T) {
	for _, arg := range []string{"vm_id", "security_group_id"} {
		for _, id := range []string{faultID(41), "legacy-id", "legacy-\"\nline"} {
			old := arg + "=" + id
			if persistedIDTarget(arg, id) != old || persistedIDTarget(arg, id+"2") == old || persistedIDTarget("another", id) == old {
				t.Fatalf("receipt key for %q and %q", arg, id)
			}
		}
	}
}

func TestResourceReadHintOnlyCopiesUUID(t *testing.T) {
	id := faultID(41)
	if got := resourceReadHint("vpc_get", "id", id); got != "vpc_get id="+id {
		t.Fatalf("UUID read: %q", got)
	}
	id = "altered-\"\nline"
	got := resourceReadHint("vpc_get", "id", id)
	if !strings.Contains(got, "vpc_get") || !strings.Contains(got, "id") || strings.Contains(got, id) {
		t.Fatalf("unusual id in read hint: %q", got)
	}
}
