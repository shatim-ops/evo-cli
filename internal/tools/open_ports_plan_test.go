// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
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

// twinGroupsCloud has two security groups of one name (review of #315).
type twinGroupsCloud struct{ provision.CloudAPI }

func (twinGroupsCloud) SetToken(string) {}
func (twinGroupsCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{
		{ID: faultID(0x81), Name: "web", AvailabilityZoneID: "z1"},
		{ID: faultID(0x82), Name: "web", AvailabilityZoneID: "z2"},
	}, nil
}

// A name two groups share is USAGE with the candidates before the question,
// at open_ports and close_ports alike (securityGroupByRef): the first the
// list gave was taken, and its rules were the ones changed.
func TestOpenPorts_AmbiguousGroupNameBeforeTheQuestion(t *testing.T) {
	r, _ := fastRegistry(t, twinGroupsCloud{}, config.Config{ProjectID: "p", KeyID: "k", Secret: "s"})
	for _, tool := range []string{"open_ports", "close_ports"} {
		for _, confirmed := range []bool{false, true} {
			m, text, isErr := callNamed(t, r, tool, map[string]any{"security_group": "web", "ports": []any{float64(8080)}, "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, faultID(0x81)) || !strings.Contains(text, faultID(0x82)) {
				t.Errorf("%s of a name two groups share, confirmed=%v: %s", tool, confirmed, text)
			}
		}
	}
}

// The plans of open_ports and disk_resize read the cloud (D35): without a
// key the preview is AUTH_REQUIRED, not a question (D31; review of #316).
func TestPreviewThatReadsTheCloud_NoKeyBeforeTheQuestion(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) {
		return nil, clerrors.New(clerrors.CodeAuthRequired, "no access key", "Run 'evo configure'")
	})
	for tool, args := range map[string]map[string]any{
		"open_ports":  {"security_group": "web", "ports": []any{float64(8080)}},
		"disk_resize": {"disk_id": faultID(0x91), "size_gb": float64(20)},
	} {
		m, text, isErr := callNamed(t, r, tool, args)
		if !isErr || m["reason"] != clerrors.CodeAuthRequired {
			t.Errorf("%s without a key: %s", tool, text)
		}
	}
}
