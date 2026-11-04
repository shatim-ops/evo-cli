// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

// AddPorts names on the preview the ports a call opens in the VM's
// security group and the source it opens them to (cidr, 0.0.0.0/0 without
// one), and warns of a port besides the web's open to anyone (P5, G2-2).
// PortsGroup names the group once the preview knows it. One helper for the
// previews of provision, deploy and deploy_apply.
func (p *CreatePreview) AddPorts(tcp, udp []int, cidr string) {
	if len(tcp)+len(udp) == 0 {
		return
	}
	labels := make([]string, 0, len(tcp)+len(udp))
	for _, port := range tcp {
		labels = append(labels, fmt.Sprint(port))
	}
	for _, port := range udp {
		labels = append(labels, fmt.Sprintf("%d/udp", port))
	}
	p.Add("ports", strings.Join(labels, ","))
	p.Add("ports_source", sgrules.Source(cidr))
	if w := sgrules.WideOpen(tcp, udp, cidr); w != "" {
		p.Warnings = append(p.Warnings, w)
	}
}

// PortsGroup names the security group the ports of AddPorts open in: the
// zone's group, shared by every VM in it. Nothing without ports or a name.
func (p *CreatePreview) PortsGroup(name string) {
	has := func(key string) bool {
		return slices.ContainsFunc(p.Params, func(prm PreviewParam) bool { return prm.Key == key })
	}
	if name == "" || !has("ports") || has("ports_group") {
		return
	}
	p.Add("ports_group", name)
}

// portsLine is the line of a preview that opens ports: the ports, the
// source and the group, and that the group is the zone's shared one.
func portsLine(ports, source, group string) string {
	if group == "" {
		return previewLabel("ports", "порты") + i18n.Tf("%s opened to %s in the VM's security group — the zone's shared group, so they open for every VM in it; rules already there stay, destroy closes only the rules it creates",
			"%s открываются для %s в группе безопасности ВМ — общей группе зоны, поэтому и для всех ВМ в ней; уже открытые правила остаются, destroy закроет только созданные", ports, source)
	}
	return previewLabel("ports", "порты") + i18n.Tf("%s opened to %s in security group %s — the zone's shared group, so they open for every VM in it; rules already there stay, destroy closes only the rules it creates",
		"%s открываются для %s в группе безопасности %s — общей группе зоны, поэтому и для всех ВМ в ней; уже открытые правила остаются, destroy закроет только созданные", ports, source, group)
}
