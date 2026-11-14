// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// What the catalog shows of a tool is its first sentence (docs/tools.md,
// tools/list): the external check of 14.11 found two that lead an agent
// wrong (release plan 1.9, «Каталог инструментов для агента — три правки»).
//
// close_ports is no way back from open_ports: it deletes every rule of the
// port, protocol and source given, one that was there before the task too
// (#1743). Its texts promise no reversal and send to the ports open_ports
// added (opened), not those it found open.
//
// deploy_apply makes the VM only: a compose project is deploy's. Its first
// sentence and its Russian line say so (#1745).
func TestCatalog_CloseAndApplyTextsSayWhatTheyDo(t *testing.T) {
	r := New(nil)
	cp, _ := r.Lookup("close_ports")
	en, ru := cp.Description, toolsRU["close_ports"].Long
	for _, bad := range []string{"way back", "обратная операция"} {
		if strings.Contains(en+ru, bad) {
			t.Errorf("close_ports promises a reversal (%q): it deletes a rule that was there before the task too", bad)
		}
	}
	// With the protocol and the source of that call: close_ports defaults to
	// tcp from 0.0.0.0/0, and the ports of an udp or own-cidr open_ports
	// passed alone would delete another rule («Риск», conclusion 217, У1).
	for _, want := range []string{"opened", "protocol", "cidr"} {
		if !strings.Contains(en, want) || !strings.Contains(ru, want) {
			t.Errorf("close_ports does not name %q of the open_ports call whose ports it closes:\n%s\n%s", want, en, ru)
		}
	}
	da, _ := r.Lookup("deploy_apply")
	first, _, _ := strings.Cut(da.Description, ". ")
	if !strings.Contains(first, "only the VM") || !strings.Contains(first, "deploy") {
		t.Errorf("deploy_apply's first sentence does not say it makes the VM only and names deploy: %q", first)
	}
	if s := toolsRU["deploy_apply"].Short; !strings.Contains(s, "только ВМ") {
		t.Errorf("deploy_apply's Russian line does not say «только ВМ»: %q", s)
	}
}
