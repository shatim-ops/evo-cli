// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// attachCloud has the VM web (faultID 0x41) with its interface, the
// floating IPs fips and, when vmFIP is set, the address bound to web's
// interface; attaches counts the attach calls.
type attachCloud struct {
	provision.CloudAPI
	fips     []*provision.FloatingIP
	vmFIP    *provision.IfaceFloatingIP
	attaches int
}

func (*attachCloud) SetToken(string) {}
func (c *attachCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return c.fips, nil
}
func (*attachCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: faultID(0x41), Name: "web"}}, nil
}
func (c *attachCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: faultID(0x41), Name: "web",
		Interfaces: []provision.VMInterface{{ID: faultID(0x43), Primary: true, FloatingIP: c.vmFIP}}}, nil
}
func (c *attachCloud) AttachFloatingIP(context.Context, string, string, string, string) (*provision.FloatingIP, error) {
	c.attaches++
	return &provision.FloatingIP{ID: faultID(0x42), IPAddress: "203.0.113.10", State: "in_use"}, nil
}

// FB130-1 (delta audit of 1.3.0): fip_attach took an address in use by
// another VM — consent was asked, and the attach after it failed or moved
// the address off that VM. An address that is not free, or a VM that has
// a floating IP already, is USAGE before the question, with confirmed or
// without; the address already bound to this VM is the answer of a repeat
// (D26), with no question and no attach.
func TestFipAttach_BusyAddressBeforeTheQuestion(t *testing.T) {
	other := json.RawMessage(`{"id":"` + faultID(0x51) + `","name":"db"}`)
	this := json.RawMessage(`{"id":"` + faultID(0x41) + `","name":"web"}`)
	cfg := config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}
	for name, c := range map[string]*attachCloud{
		"in use by another VM": {fips: []*provision.FloatingIP{{ID: faultID(0x42), IPAddress: "203.0.113.10", State: "in_use", VM: other}}},
		"being deleted":        {fips: []*provision.FloatingIP{{ID: faultID(0x42), IPAddress: "203.0.113.10", State: "error_deleting"}}},
		"the VM has one": {fips: []*provision.FloatingIP{{ID: faultID(0x42), IPAddress: "203.0.113.10", State: "available"}},
			vmFIP: &provision.IfaceFloatingIP{ID: faultID(0x44), IPAddress: "203.0.113.20"}},
	} {
		for _, confirmed := range []bool{false, true} {
			r, _ := fastRegistry(t, c, cfg)
			m, text, isErr := callNamed(t, r, "fip_attach", map[string]any{"fip": "203.0.113.10", "vm": "web", "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || c.attaches != 0 {
				t.Errorf("%s, confirmed=%v (attaches %d): %s", name, confirmed, c.attaches, text)
			}
		}
	}
	bound := &attachCloud{fips: []*provision.FloatingIP{{ID: faultID(0x42), IPAddress: "203.0.113.10", State: "in_use", VM: this}},
		vmFIP: &provision.IfaceFloatingIP{ID: faultID(0x42), IPAddress: "203.0.113.10"}}
	for _, confirmed := range []bool{false, true} {
		r, _ := fastRegistry(t, bound, cfg)
		m, text, isErr := callNamed(t, r, "fip_attach", map[string]any{"fip": "203.0.113.10", "vm": "web", "confirmed": confirmed})
		if isErr || m["status"] != "already_attached" || bound.attaches != 0 {
			t.Errorf("the address already web's, confirmed=%v (attaches %d): %s", confirmed, bound.attaches, text)
		}
	}
}
