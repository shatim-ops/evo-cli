// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// G13-23 (Grok, wave 2): the preview of open_ports named the ports and the
// group as given, not the source the rules open them to (0.0.0.0/0 by
// default), and a group that is not there failed after consent. The plan
// reads the group and its rules before the question (D35): the preview
// says the protocol, the source and what is open already.
func TestOpenPorts_PreviewSaysTheSource(t *testing.T) {
	cloud := &faultCloud{rules: []provision.SGRule{
		{ID: faultID(0x71), Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", RemoteIPPrefix: "0.0.0.0/0"},
	}}
	r, _ := faultToolsRegistry(t, cloud)
	m, _, isErr := callNamed(t, r, "open_ports", map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(80), float64(8080)}})
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("open_ports preview: %v", m)
	}
	msg, _ := m["message"].(string)
	if m["cidr"] != "0.0.0.0/0" || m["protocol"] != "tcp" || !strings.Contains(msg, "0.0.0.0/0") {
		t.Errorf("the preview does not say the source and protocol: %v", m)
	}
	if open, _ := m["will_open"].([]any); len(open) != 1 || open[0] != "8080" {
		t.Errorf("will_open %v, want [8080]", m["will_open"])
	}
	if already, _ := m["already_open"].([]any); len(already) != 1 || already[0] != "80" {
		t.Errorf("already_open %v, want [80]", m["already_open"])
	}
}

// A group that is not there is refused before the question.
func TestOpenPorts_GroupNotThereBeforeTheQuestion(t *testing.T) {
	r, _ := faultToolsRegistry(t, &faultCloud{})
	for _, confirmed := range []bool{false, true} {
		m, _, isErr := callNamed(t, r, "open_ports", map[string]any{"security_group": "no-such-group", "ports": []any{float64(8080)}, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeNotFound {
			t.Errorf("open_ports of a group that is not there, confirmed=%v: %v", confirmed, m)
		}
	}
}
