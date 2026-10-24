// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// detailFailsCloud has a free floating IP and the VM web, whose details
// cannot be read.
type detailFailsCloud struct{ provision.CloudAPI }

func (detailFailsCloud) SetToken(string) {}
func (detailFailsCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: faultID(0x42), IPAddress: "203.0.113.10"}}, nil
}
func (detailFailsCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: faultID(0x41), Name: "web"}}, nil
}
func (detailFailsCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return nil, errors.New("boom")
}

// The errors fip_attach adds to the cloud's are in the CLI's language (D6;
// review of #319): «get vm detail» was English in a Russian CLI.
func TestFipAttach_ErrorsSpeakCLILanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	i18n.SetLang(i18n.RU)
	r, _ := fastRegistry(t, detailFailsCloud{}, config.Config{ProjectID: "p", KeyID: "k", Secret: "s"})
	_, text, isErr := callNamed(t, r, "fip_attach", map[string]any{"fip": "203.0.113.10", "vm": "web"})
	if !isErr || !strings.Contains(text, "данные ВМ") || strings.Contains(text, "get vm detail") {
		t.Errorf("fip_attach in a Russian CLI: %s", text)
	}
}
