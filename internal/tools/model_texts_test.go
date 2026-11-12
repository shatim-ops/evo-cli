// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Тексты, которые читает модель (P10, решение владельца 31.10): в прогоне
// Grok с LLM на 1.6.0 90 % вызовов с confirmed=true шли без превью, а
// описание аргумента «Must be true — set only after the user approved»
// читалось как «ставь true». Описание называет путь: без confirmed —
// превью, true — только после согласия человека с этим превью.
func TestConfirmedDescription_PreviewFirst(t *testing.T) {
	r := New(nil)
	n := 0
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		p, ok := props["confirmed"].(map[string]any)
		if !ok {
			continue
		}
		n++
		desc, _ := p["description"].(string)
		if strings.Contains(desc, "Must be true") {
			t.Errorf("%s: confirmed reads as an order to set it: %q", tl.Name, desc)
		}
		if !strings.Contains(desc, "Omit for the preview") || !strings.Contains(desc, "only after the person approved it") {
			t.Errorf("%s: confirmed does not say preview first, then the person's consent: %q", tl.Name, desc)
		}
	}
	if n == 0 {
		t.Fatal("no tool takes confirmed")
	}
}

// deploy names its VM by the project, not by an argument: the agent of the
// LLM run looked for its VM under the name it meant and left evo-<dir>
// behind (G2-4).
func TestDeployDescription_NamesTheVM(t *testing.T) {
	tl, ok := New(nil).Lookup("deploy")
	if !ok {
		t.Fatal("no deploy")
	}
	for _, want := range []string{"evo-<project_name>", "project_name"} {
		if !strings.Contains(tl.Description, want) {
			t.Errorf("deploy description does not say %q: %s", want, tl.Description)
		}
	}
}

// A task that only reads money (FinOps) went on to delete in the LLM run
// (G2-17): the billing reads say that such a task wants a read-only server.
func TestBillingDescriptions_PointToReadOnly(t *testing.T) {
	r := New(nil)
	for _, name := range []string{"billing_balance", "billing_consumption", "billing_finops"} {
		tl, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		if !strings.Contains(tl.Description, "--read-only") {
			t.Errorf("%s does not point a read-only task to --read-only: %s", name, tl.Description)
		}
	}
}
