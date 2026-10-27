// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// G130-16: with --allow-project, a resource of another project and one that
// is gone share one answer; the message names both reasons so an agent does
// not hunt for another server after a delete.
func TestNotInProject_NamesDeletedOrNotInProjects(t *testing.T) {
	k := &ownerKind{what: "cluster", listTool: "pg_list"}
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		err := notInProject(k, "00000000-0000-4000-8000-0000000000c2")
		msg := err.Error()
		switch lang {
		case i18n.EN:
			if !strings.Contains(msg, "deleted") || !strings.Contains(msg, "not in the projects") {
				t.Errorf("%s: %q", lang, msg)
			}
		case i18n.RU:
			if !strings.Contains(msg, "удалён") || !strings.Contains(msg, "не в проектах") {
				t.Errorf("%s: %q", lang, msg)
			}
		}
	}
	i18n.SetLang(i18n.EN)
}

// G130-36: destroy preview uses future tense for the floating IP; the
// answer after confirmed=true still says the IP was released.
func TestDestroyPreview_FloatingIPFutureTense(t *testing.T) {
	r, _ := targetRegistry(t, &targetCloud{})
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001"})
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("preview: %s", text)
	}
	f, _ := m["floating_ip"].(string)
	if !strings.Contains(f, "will be released:") {
		t.Errorf("preview floating_ip = %q, want will be released:", f)
	}
	if strings.Contains(f, "released:") && !strings.Contains(f, "will be released:") {
		t.Errorf("past tense in preview: %q", f)
	}

	_, text, isErr = callNamed(t, r, "destroy", map[string]any{
		"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if isErr {
		t.Fatalf("confirmed: %s", text)
	}
	if !strings.Contains(text, "released") {
		t.Errorf("confirmed answer without released: %s", text)
	}
}
