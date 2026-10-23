// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// natFIPCloud: адрес 203.0.113.9 принадлежит SNAT-шлюзу (nat_gateway в
// ответе), его состояние API может показывать как available.
type natFIPCloud struct {
	provision.CloudAPI
	deleted string
}

func natFIP() *provision.FloatingIP {
	var f provision.FloatingIP
	_ = json.Unmarshal([]byte(`{"id":"0000f1f1-0000-4000-8000-0000000000aa","name":"snat","ip_address":"203.0.113.9","state":"available","nat_gateway":{"id":"gw-1","name":"gw"}}`), &f)
	return &f
}

func (*natFIPCloud) SetToken(string) {}
func (*natFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{natFIP()}, nil
}
func (*natFIPCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return natFIP(), nil
}
func (c *natFIPCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.deleted = id
	return nil
}

// Адрес SNAT-шлюза занят шлюзом: list_floating_ips не называет его
// свободным, release_floating_ip его не освобождает (API ответил бы 422
// floating_ip_address_is_used_by_nat_gateway), fip_attach не привязывает.
func TestFloatingIP_NATGatewayAddressIsInUse(t *testing.T) {
	if natFIP().Available() || !natFIP().OnNATGateway() {
		t.Fatal("a SNAT gateway's address counts as available")
	}
	cloud := &natFIPCloud{}
	r, _ := targetRegistry(t, cloud)
	m, text, _ := callNamed(t, r, "list_floating_ips", map[string]any{})
	list, _ := m["floating_ips"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["available"] != false || list[0].(map[string]any)["nat_gateway"] != true {
		t.Errorf("list: %s", text)
	}
	for _, args := range []map[string]any{{"id": "0000f1f1-0000-4000-8000-0000000000aa", "confirmed": true}, {"ip_address": "203.0.113.9", "confirmed": true}} {
		m, text, _ = callNamed(t, r, "release_floating_ip", args)
		if m["status"] != "refused" || cloud.deleted != "" {
			t.Errorf("release %v: %s (deleted %q)", args, text, cloud.deleted)
		}
	}
	if _, text, isErr := callNamed(t, r, "fip_attach", map[string]any{"fip": "203.0.113.9", "vm": "web", "confirmed": true}); !isErr {
		t.Errorf("attach: %s", text)
	}
}

// A6 (D31): release_floating_ip refuses the address of a SNAT gateway, and
// one in use by a VM, before the consent — the call cannot go through
// whatever the person answers (CLAUDE.md §4); the question was asked, and
// the refusal came after the yes.
func TestReleaseFloatingIP_RefusedBeforeConsent(t *testing.T) {
	nat := &natFIPCloud{}
	r, _ := targetRegistry(t, nat)
	for _, args := range []map[string]any{{"id": "0000f1f1-0000-4000-8000-0000000000aa"}, {"ip_address": "203.0.113.9"}} {
		m, text, isErr := callNamed(t, r, "release_floating_ip", args)
		if !isErr || m["status"] != "refused" || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "snat_delete") {
			t.Errorf("SNAT address %v: %s", args, text)
		}
	}
	used := &inUseFIPCloud{}
	r, _ = targetRegistry(t, used)
	m, text, isErr := callNamed(t, r, "release_floating_ip", map[string]any{"id": "0000f1f1-0000-4000-8000-000000000001"})
	if !isErr || m["status"] != "refused" || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "in use") {
		t.Errorf("in use: %s", text)
	}
	if nat.deleted != "" || used.deleted {
		t.Error("a refused release deleted the address")
	}
}
