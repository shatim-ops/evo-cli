// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "testing"

// Правила группы для новой ВМ: входящий tcp 22 и исходящий системный трафик
// (сервис метаданных 169.254.169.254/32 и udp 67 до шлюза подсети —
// документация «Группы безопасности», ограничения, с 07.2026).
func TestCheckVMGroupRules(t *testing.T) {
	sshIn := SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0"}
	allOut := SGRule{Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "0.0.0.0/0"}
	metaOut := SGRule{Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "169.254.169.254/32"}
	dhcpOut := SGRule{Direction: "egress", IPProtocol: "udp", PortRange: "67", RemoteIPPrefix: "10.0.0.1/32"}
	webIn := SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "80-443", RemoteIPPrefix: "0.0.0.0/0"}
	for _, c := range []struct {
		name  string
		rules []SGRule
		want  GroupGaps
	}{
		{"SSH-access по умолчанию", []SGRule{sshIn, allOut}, GroupGaps{}},
		{"пара системных правил", []SGRule{sshIn, metaOut, dhcpOut}, GroupGaps{}},
		{"только метаданные", []SGRule{sshIn, metaOut}, GroupGaps{NoSystemEgress: true}},
		{"нет 22", []SGRule{webIn, allOut}, GroupGaps{NoSSH: true}},
		{"диапазон с 22", []SGRule{{Direction: "ingress", IPProtocol: "tcp", PortRange: "1-1024"}, allOut}, GroupGaps{}},
		{"udp 22 — не SSH", []SGRule{{Direction: "ingress", IPProtocol: "udp", PortRange: "22"}, allOut}, GroupGaps{NoSSH: true}},
		{"удаляемое правило не считается", []SGRule{sshIn, {Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "0.0.0.0/0", State: "deleting"}}, GroupGaps{NoSystemEgress: true}},
		{"исходящий только в интернет-подсеть", []SGRule{sshIn, {Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "10.0.0.0/8"}}, GroupGaps{NoSystemEgress: true}},
		{"без правил всё закрыто", nil, GroupGaps{NoSSH: true, NoSystemEgress: true}},
	} {
		if got := CheckVMGroupRules(c.rules, "10.0.0.1"); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	// Шлюз неизвестен: udp 67 в любой адрес считается.
	if got := CheckVMGroupRules([]SGRule{sshIn, metaOut, {Direction: "egress", IPProtocol: "udp", PortRange: "67", RemoteIPPrefix: "192.168.0.1/32"}}, ""); got != (GroupGaps{}) {
		t.Errorf("unknown gateway: %+v", got)
	}
}
