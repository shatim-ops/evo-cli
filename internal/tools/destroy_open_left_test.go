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

// destroy by vm_id says what stays open to the internet in the groups of
// the VM, as destroy of the folder's deployment does (engine.OpenLeft;
// «Риск», conclusion 146, У4): the groups are those of the VM's
// interfaces when no record names one.

const openLeftVM = "0000a1a1-0000-4000-8000-000000000001"

// openLeftCloud is targetCloud whose VM is in a group with rules.
type openLeftCloud struct {
	targetCloud
	rules    []*provision.SGRule
	rulesErr error
}

func (*openLeftCloud) ListInterfaces(context.Context, string) ([]*provision.Interface, error) {
	groups := []provision.InterfaceGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-1"}}
	return []*provision.Interface{
		{ID: "if-1", VM: &provision.InterfaceVM{ID: openLeftVM, Name: "web"}, SecurityGroups: groups},
		{ID: "if-2", VM: &provision.InterfaceVM{ID: "0000a1a1-0000-4000-8000-000000000002", Name: "db"}, SecurityGroups: groups},
	}, nil
}

func (*openLeftCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-1", Default: true}}, nil
}

func (c *openLeftCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return c.rules, c.rulesErr
}

func TestDestroyTargeted_SaysWhatStaysOpenToTheInternet(t *testing.T) {
	cloud := &openLeftCloud{rules: []*provision.SGRule{
		{ID: "r-ssh", Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0", State: "created"},
		{ID: "r-private", Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "10.0.0.0/8", State: "created"},
	}}
	r, _ := targetRegistry(t, cloud)
	m, text, _ := callNamed(t, r, "destroy", map[string]any{"vm_id": openLeftVM})
	raw, _ := json.Marshal(m["open_to_internet_left"])
	if m["status"] != "confirmation_required" || !strings.Contains(string(raw), `"r-ssh"`) || strings.Contains(string(raw), "r-private") {
		t.Fatalf("destroy by vm_id: open_to_internet_left = %s in %s", raw, text)
	}
	msg, _ := m["message"].(string)
	if !strings.Contains(msg, "stay open to the internet") || !strings.Contains(msg, "tcp 22:22") || !strings.Contains(msg, "shared: 2 interface") {
		t.Errorf("the message does not say what stays open, in a group shared by 2 interfaces: %s", msg)
	}
	for _, bad := range []string{"sg_rule_delete", "close_ports"} {
		if strings.Contains(text, bad) {
			t.Errorf("the preview advises %s: %s", bad, text)
		}
	}
	// The rules cannot be read: said, not silence.
	cloud = &openLeftCloud{rulesErr: clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")}
	r, _ = targetRegistry(t, cloud)
	m, text, _ = callNamed(t, r, "destroy", map[string]any{"vm_id": openLeftVM})
	left, _ := m["open_to_internet_left"].(map[string]any)
	if m["status"] != "confirmation_required" || left["unread"] != clerrors.CodeAPIUnreachable || !strings.Contains(text, "could not read what stays open") {
		t.Errorf("unread rules: %s", text)
	}
	// Nothing open to the internet stays: nothing is said.
	cloud = &openLeftCloud{rules: []*provision.SGRule{
		{ID: "r-private", Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "10.0.0.0/8", State: "created"}}}
	r, _ = targetRegistry(t, cloud)
	if m, text, _ := callNamed(t, r, "destroy", map[string]any{"vm_id": openLeftVM}); m["open_to_internet_left"] != nil {
		t.Errorf("nothing stays open: %s", text)
	}
}
